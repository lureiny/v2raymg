package mihomo

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// newTestUserManager builds a UserManager backed by a real (in-process)
// DefaultForwardManager so AddUser/RemoveUser/ReleaseAllUserPorts exercise
// the real port allocation path. The 40000-50000 range avoids any overlap
// with ports other tests in this package fabricate for inbound listen
// ports (10001-10003).
func newTestUserManager(t *testing.T) (*usermanager.UserManager, *forward.DefaultForwardManager) {
	t.Helper()
	fwdMgr, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort: 40000,
		MaxPort: 50000,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	t.Cleanup(func() { _ = fwdMgr.Close() })
	um := usermanager.NewUserManager(fwdMgr, "test-node")
	return um, fwdMgr
}

func TestMihomoInbound_NewSetsLoopback(t *testing.T) {
	// Hardcoded 127.0.0.1 is load-bearing: forward layer owns external
	// ingress, and mihomo must not be publicly reachable.
	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "uuid-a"})
	if inb.ListenAddr() != "127.0.0.1" {
		t.Errorf("listen_addr = %q, want 127.0.0.1", inb.ListenAddr())
	}
	if inb.Tag() != "vmess-a" {
		t.Errorf("tag = %q", inb.Tag())
	}
	if inb.Port() != 10001 {
		t.Errorf("port = %d", inb.Port())
	}
	if inb.Protocol() != contracts.ProtocolVMess {
		t.Errorf("protocol = %q", inb.Protocol())
	}
}

func TestMihomoInbound_Validate(t *testing.T) {
	tests := []struct {
		name    string
		inbound *MihomoInbound
		wantErr error
	}{
		{
			name:    "vmess ok",
			inbound: NewMihomoInbound("t", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "uuid"}),
		},
		{
			name:    "vmess missing uuid",
			inbound: NewMihomoInbound("t", contracts.ProtocolVMess, 10001, MihomoSharedCred{}),
			wantErr: ErrMissingCredential,
		},
		{
			name:    "trojan ok",
			inbound: NewMihomoInbound("t", contracts.ProtocolTrojan, 10001, MihomoSharedCred{Password: "pw"}),
		},
		{
			name:    "trojan missing password",
			inbound: NewMihomoInbound("t", contracts.ProtocolTrojan, 10001, MihomoSharedCred{}),
			wantErr: ErrMissingCredential,
		},
		{
			name:    "shadowsocks ok",
			inbound: NewMihomoInbound("t", contracts.ProtocolShadowsocks, 10001, MihomoSharedCred{Password: "pw", Cipher: "aes-128-gcm"}),
		},
		{
			name:    "shadowsocks missing password",
			inbound: NewMihomoInbound("t", contracts.ProtocolShadowsocks, 10001, MihomoSharedCred{Cipher: "aes-128-gcm"}),
			wantErr: ErrMissingCredential,
		},
		{
			name:    "shadowsocks missing cipher",
			inbound: NewMihomoInbound("t", contracts.ProtocolShadowsocks, 10001, MihomoSharedCred{Password: "pw"}),
			wantErr: ErrMissingCredential,
		},
		{
			// Phase 6 added tuic support; anytls is the next unimplemented
			// protocol — it's the "unsupported" sentinel until Phase 7.
			name:    "unsupported protocol",
			inbound: NewMihomoInbound("t", contracts.ProtocolAnyTLS, 10001, MihomoSharedCred{Password: "pw"}),
			wantErr: ErrProtocolNotSupported,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.inbound.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want wrapping %v", err, tc.wantErr)
			}
		})
	}
}

// TestValidateHysteria2_ErrorPaths covers the validateHysteria2 sentinel
// rejections. Phase 5 has no SharedCred legacy path, so all cases construct
// the inbound directly via NewMihomoInboundFromProtocolParams.
func TestValidateHysteria2_ErrorPaths(t *testing.T) {
	hy2BasePP := func() *protocolparams.ProtocolParams {
		return &protocolparams.ProtocolParams{
			Tag:      "hy2-test",
			Protocol: contracts.ProtocolHysteria2,
			Port:     10101,
			Hysteria2: &protocolparams.Hysteria2Params{
				Password: "pw",
			},
			Security: &protocolparams.SecuritySpec{
				Kind: contracts.SecurityTLS,
				TLS: &protocolparams.TLSSpec{
					CertFile: "/tmp/cert.pem",
					KeyFile:  "/tmp/key.pem",
				},
			},
		}
	}

	cases := []struct {
		name    string
		mutate  func(*protocolparams.ProtocolParams)
		wantErr error
	}{
		{
			name:    "missing Hysteria2Params",
			mutate:  func(pp *protocolparams.ProtocolParams) { pp.Hysteria2 = nil },
			wantErr: ErrMissingCredential,
		},
		{
			name:    "empty password",
			mutate:  func(pp *protocolparams.ProtocolParams) { pp.Hysteria2.Password = "" },
			wantErr: ErrMissingCredential,
		},
		{
			name:    "no security",
			mutate:  func(pp *protocolparams.ProtocolParams) { pp.Security = nil },
			wantErr: ErrMissingCredential,
		},
		{
			name:    "security=none",
			mutate:  func(pp *protocolparams.ProtocolParams) { pp.Security.Kind = contracts.SecurityNone },
			wantErr: ErrProtocolNotSupported,
		},
		{
			name:    "security=tls but TLS spec nil",
			mutate:  func(pp *protocolparams.ProtocolParams) { pp.Security.TLS = nil },
			wantErr: ErrMissingCredential,
		},
		{
			name:    "missing cert_file",
			mutate:  func(pp *protocolparams.ProtocolParams) { pp.Security.TLS.CertFile = "" },
			wantErr: ErrMissingCredential,
		},
		{
			name:    "missing key_file",
			mutate:  func(pp *protocolparams.ProtocolParams) { pp.Security.TLS.KeyFile = "" },
			wantErr: ErrMissingCredential,
		},
		{
			name:    "unknown obfs",
			mutate:  func(pp *protocolparams.ProtocolParams) { pp.Hysteria2.Obfs = "snowflake" },
			wantErr: ErrProtocolNotSupported,
		},
		{
			name: "obfs=salamander missing password",
			mutate: func(pp *protocolparams.ProtocolParams) {
				pp.Hysteria2.Obfs = "salamander"
				pp.Hysteria2.ObfsPassword = ""
			},
			wantErr: ErrMissingCredential,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pp := hy2BasePP()
			tc.mutate(pp)
			inb := NewMihomoInboundFromProtocolParams(pp)
			err := inb.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want wrapping %v", err, tc.wantErr)
			}
		})
	}

	// Sanity: the unmutated baseline must pass — otherwise the mutate
	// helpers above could pass for the wrong reason.
	t.Run("baseline ok", func(t *testing.T) {
		inb := NewMihomoInboundFromProtocolParams(hy2BasePP())
		if err := inb.Validate(); err != nil {
			t.Fatalf("baseline should validate, got %v", err)
		}
	})
}

func TestMihomoInbound_ToNative_OmitsIrrelevantCredFields(t *testing.T) {
	// vmess record should not include `password` / `cipher` keys even as
	// empty strings — keeps JSON compact and hides credential schema drift
	// if we ever add a new cred field.
	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "uuid"})
	native, err := inb.ToNative()
	if err != nil {
		t.Fatalf("ToNative: %v", err)
	}
	if strings.Contains(string(native), `"password"`) {
		t.Errorf("native JSON leaks password field on vmess inbound: %s", native)
	}
	if strings.Contains(string(native), `"cipher"`) {
		t.Errorf("native JSON leaks cipher field on vmess inbound: %s", native)
	}
	// And it MUST contain uuid.
	if !strings.Contains(string(native), `"uuid":"uuid"`) {
		t.Errorf("native JSON missing uuid: %s", native)
	}
}

func TestMihomoInbound_ToNative_FromNative_RoundTrip(t *testing.T) {
	tests := []*MihomoInbound{
		NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "uuid-a"}),
		NewMihomoInbound("trojan-b", contracts.ProtocolTrojan, 10002, MihomoSharedCred{Password: "pw-b"}),
		NewMihomoInbound("ss-c", contracts.ProtocolShadowsocks, 10003, MihomoSharedCred{Password: "pw-c", Cipher: "aes-128-gcm"}),
	}
	for _, orig := range tests {
		t.Run(string(orig.Protocol()), func(t *testing.T) {
			native, err := orig.ToNative()
			if err != nil {
				t.Fatalf("ToNative: %v", err)
			}
			reloaded, err := FromNative(native)
			if err != nil {
				t.Fatalf("FromNative: %v", err)
			}
			if reloaded.Tag() != orig.Tag() ||
				reloaded.Protocol() != orig.Protocol() ||
				reloaded.Port() != orig.Port() ||
				reloaded.ListenAddr() != orig.ListenAddr() ||
				reloaded.SharedCred != orig.SharedCred {
				t.Errorf("round-trip diverged:\n  orig=%+v\n  back=%+v", orig, reloaded)
			}
		})
	}
}

func TestFromNative_RejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"bad json", `{not json`},
		{"unsupported protocol", `{"tag":"t","protocol":"anytls","port":10001,"shared_cred":{"password":"pw"}}`},
		{"missing credential", `{"tag":"t","protocol":"vmess","port":10001,"shared_cred":{}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FromNative([]byte(tc.data))
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestFromNative_AcceptsPortBoundaryFromStoredRecord(t *testing.T) {
	// Records written by older code paths may have port at the edge of the
	// allowed range. Round-trip should preserve it.
	rec := mihomoInboundJSON{
		Tag:        "t",
		Protocol:   "vmess",
		Port:       65535,
		ListenAddr: "127.0.0.1",
		SharedCred: MihomoSharedCred{UUID: "uuid"},
	}
	data, _ := json.Marshal(&rec)
	inb, err := FromNative(data)
	if err != nil {
		t.Fatalf("FromNative: %v", err)
	}
	if inb.Port() != 65535 {
		t.Errorf("port lost: %d", inb.Port())
	}
}

// ----- user lifecycle (stage 5) --------------------------------------------

func TestMihomoInbound_AddUser_AllocatesAndTracks(t *testing.T) {
	um, _ := newTestUserManager(t)
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	inb.SetUserManager(um)

	port, err := inb.AddUser("alice", nil)
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if port == 0 {
		t.Error("AddUser returned port 0")
	}
	if !inb.hasAddedUser("alice") {
		t.Error("hasAddedUser=false after AddUser")
	}
	// UserManager should have a port mapping linking inbound.Port() → port.
	if got, ok := um.GetUserPortByDst("alice", 10001); !ok || got != port {
		t.Errorf("UserManager mapping mismatch: got=%d ok=%v want=%d", got, ok, port)
	}
}

func TestMihomoInbound_AddUser_Idempotent(t *testing.T) {
	// Second AddUser must NOT allocate a new port — the fast-path in
	// AddUser should return the cached mapping. Regression guard: if
	// markAddedUser happens before GetBindPort errors out, calling
	// AddUser again would skip the fast-path check and try to allocate
	// a second time.
	um, _ := newTestUserManager(t)
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	inb.SetUserManager(um)

	first, err := inb.AddUser("alice", nil)
	if err != nil {
		t.Fatalf("first AddUser: %v", err)
	}
	second, err := inb.AddUser("alice", nil)
	if err != nil {
		t.Fatalf("second AddUser: %v", err)
	}
	if first != second {
		t.Errorf("AddUser not idempotent: first=%d second=%d", first, second)
	}
}

func TestMihomoInbound_AddUser_StaleTrackingRecovers(t *testing.T) {
	// addedUsers records a user that has no matching forward rule (e.g.
	// rule was torn down out-of-band). AddUser must detect the stale
	// flag via GetUserPortByDst returning (_, false) and reallocate.
	um, _ := newTestUserManager(t)
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	inb.SetUserManager(um)

	// Poison: mark as added without allocating a rule.
	inb.markAddedUser("alice")

	port, err := inb.AddUser("alice", nil)
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if port == 0 {
		t.Error("AddUser should have reallocated after stale tracking detected")
	}
	if got, ok := um.GetUserPortByDst("alice", 10001); !ok || got != port {
		t.Errorf("expected fresh mapping for alice: got=%d ok=%v", got, ok)
	}
}

func TestMihomoInbound_RemoveUser_TearsDownRule(t *testing.T) {
	um, _ := newTestUserManager(t)
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	inb.SetUserManager(um)
	if _, err := inb.AddUser("alice", nil); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	if err := inb.RemoveUser("alice"); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}
	if inb.hasAddedUser("alice") {
		t.Error("hasAddedUser=true after RemoveUser")
	}
	if _, ok := um.GetUserPortByDst("alice", 10001); ok {
		t.Error("UserManager mapping survived RemoveUser")
	}
}

func TestMihomoInbound_RemoveUser_NeverAdded_NoOp(t *testing.T) {
	// Ensure "remove user that was never added" does not error — this
	// guarantees reconcile's blanket RemoveUser sweep is safe.
	um, _ := newTestUserManager(t)
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	inb.SetUserManager(um)

	if err := inb.RemoveUser("alice"); err != nil {
		t.Errorf("RemoveUser on never-added user should be no-op, got: %v", err)
	}
}

func TestMihomoInbound_ReleaseAllUserPorts_ReleasesByTag(t *testing.T) {
	// Users on inb-a and inb-b: ReleaseAllUserPorts("vmess-a") must
	// release ONLY the inb-a forward rules, leaving inb-b rules intact.
	// This is the atomicity contract that RemoveInboundConfig relies on.
	//
	// NOTE: ReleaseInboundPorts intentionally leaves user.PortMappings
	// untouched (see usermanager.ReleaseInboundPorts doc) so tests assert
	// against ForwardManager's rule state, not GetUserPortByDst.
	um, fwdMgr := newTestUserManager(t)
	for _, user := range []string{"alice", "bob"} {
		if err := um.AddUserForTest(user, nil); err != nil {
			t.Fatalf("AddUserForTest %q: %v", user, err)
		}
	}
	inbA := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	inbA.SetUserManager(um)
	inbB := NewMihomoInbound("trojan-b", contracts.ProtocolTrojan, 10002, MihomoSharedCred{Password: "p"})
	inbB.SetUserManager(um)

	for _, user := range []string{"alice", "bob"} {
		if _, err := inbA.AddUser(user, nil); err != nil {
			t.Fatalf("inbA AddUser %q: %v", user, err)
		}
		if _, err := inbB.AddUser(user, nil); err != nil {
			t.Fatalf("inbB AddUser %q: %v", user, err)
		}
	}

	if err := inbA.ReleaseAllUserPorts(); err != nil {
		t.Fatalf("ReleaseAllUserPorts: %v", err)
	}

	for _, user := range []string{"alice", "bob"} {
		if rule := fwdMgr.GetRule("mihomo:vmess-a:" + user); rule != nil {
			t.Errorf("user %q inbA rule should be gone, got %+v", user, rule)
		}
		if rule := fwdMgr.GetRule("mihomo:trojan-b:" + user); rule == nil {
			t.Errorf("user %q inbB rule should remain", user)
		}
	}
}

func TestMihomoInbound_NoUserManager(t *testing.T) {
	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	// SetUserManager intentionally not called.
	if _, err := inb.AddUser("alice", nil); err == nil {
		t.Error("AddUser should error when userMgr is nil")
	}
	if err := inb.RemoveUser("alice"); err != nil {
		t.Errorf("RemoveUser should no-op when userMgr is nil, got: %v", err)
	}
	if err := inb.ReleaseAllUserPorts(); err != nil {
		t.Errorf("ReleaseAllUserPorts should no-op when userMgr is nil, got: %v", err)
	}
}
