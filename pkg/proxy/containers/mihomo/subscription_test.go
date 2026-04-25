package mihomo

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// wireUserAndInbound drives the container through the SAME sequence a
// production request would use:
//
//  1. UserManager.AddUserForTest seeds the user.
//  2. c.FastAddInbound installs the inbound via params — which matches
//     the shape FromParams/adapter validate and internally calls
//     reconcileUsersForInbound to allocate a forward rule for every
//     currently-visible user on the new listener.
//
// By piggybacking on reconcileUsersForInbound we avoid any test-only
// plumbing that could mask a regression in FastAddInbound's post-push
// user wiring (stage 5 contract).
func wireUserAndInbound(t *testing.T, c *MihomoContainer, username, tag string, params map[string]any) {
	t.Helper()
	if err := c.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound(%q): %v", tag, err)
	}
}

func TestGetUserSubscriptions_MissingUsername(t *testing.T) {
	c, _, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	_, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		Host: "example.com",
	})
	if err == nil {
		t.Fatal("expected error for missing username")
	}
}

func TestGetUserSubscriptions_MissingHost(t *testing.T) {
	c, _, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	_, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
	})
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

// TestGetUserSubscriptions_NilUserMgr ensures the code path that skips
// subscription generation when no UserManager is wired returns (nil, nil)
// — matches the snell/hysteria "nothing to say" behavior rather than an
// error. The HTTP /sub endpoint expects a successful empty result from
// containers that aren't serving users.
func TestGetUserSubscriptions_NilUserMgr(t *testing.T) {
	c := newTestContainer(t) // no WithUserManager
	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if specs != nil {
		t.Errorf("expected nil specs when no user manager, got %+v", specs)
	}
}

func TestGetUserSubscriptions_VMess(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "vmess-a", map[string]any{
		"protocol": "vmess", "port": 10001, "uuid": "u-vmess",
	})
	wantPort, ok := um.GetUserPortByDst("alice", 10001)
	if !ok {
		t.Fatal("alice has no forward port after FastAddInbound reconcile")
	}

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	spec := specs[0]

	if spec.Protocol != contracts.ProtocolVMess {
		t.Errorf("Protocol = %q, want vmess", spec.Protocol)
	}
	if spec.Port != wantPort {
		t.Errorf("Port = %d, want forward port %d", spec.Port, wantPort)
	}
	if spec.Password != "u-vmess" {
		t.Errorf("Password = %q, want shared uuid", spec.Password)
	}
	if spec.InboundTag != "vmess-a" {
		t.Errorf("InboundTag = %q, want vmess-a", spec.InboundTag)
	}
	if spec.Username != "alice" {
		t.Errorf("Username = %q, want alice", spec.Username)
	}
	if spec.NodeName != "vmess-a" {
		t.Errorf("NodeName = %q, want inbound tag fallback", spec.NodeName)
	}
	if !strings.HasPrefix(spec.URI, "vmess://") {
		t.Fatalf("URI scheme wrong: %q", spec.URI)
	}

	// Decode the URI body and confirm host / port / UUID round-tripped.
	// Using base64.StdEncoding because the codec's VMess encoder emits
	// RFC 4648 std with padding (keep this in sync with codec/vmess.go).
	body := strings.TrimPrefix(spec.URI, "vmess://")
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("vmess URI base64 decode: %v", err)
	}
	var v struct {
		Add  string `json:"add"`
		Port string `json:"port"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("vmess body decode: %v", err)
	}
	if v.Add != "vps.example.com" {
		t.Errorf("URI add = %q, want vps.example.com", v.Add)
	}
	if v.ID != "u-vmess" {
		t.Errorf("URI id = %q, want u-vmess", v.ID)
	}
}

func TestGetUserSubscriptions_Trojan(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "trojan-b", map[string]any{
		"protocol": "trojan", "port": 10002, "password": "pw-trojan",
	})
	wantPort, ok := um.GetUserPortByDst("alice", 10002)
	if !ok {
		t.Fatal("alice has no forward port after FastAddInbound reconcile")
	}

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     contracts.UserSpec{Username: "alice"},
		Host:     "vps.example.com",
		NodeName: "my-node", // explicit node name override
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	spec := specs[0]

	if spec.Protocol != contracts.ProtocolTrojan {
		t.Errorf("Protocol = %q, want trojan", spec.Protocol)
	}
	if spec.Password != "pw-trojan" {
		t.Errorf("Password = %q, want shared password", spec.Password)
	}
	if spec.Port != wantPort {
		t.Errorf("Port = %d, want %d", spec.Port, wantPort)
	}
	if spec.NodeName != "my-node" {
		t.Errorf("NodeName = %q, want my-node (request override)", spec.NodeName)
	}
	if !strings.HasPrefix(spec.URI, "trojan://") {
		t.Fatalf("URI scheme wrong: %q", spec.URI)
	}

	// Parse the URI directly; codec.TrojanNode.Encode uses net/url, so the
	// shape should round-trip through net/url.Parse.
	u, err := url.Parse(spec.URI)
	if err != nil {
		t.Fatalf("trojan URI parse: %v", err)
	}
	if u.User == nil || u.User.Username() != "pw-trojan" {
		t.Errorf("URI password = %q, want pw-trojan", u.User)
	}
	if u.Hostname() != "vps.example.com" {
		t.Errorf("URI host = %q, want vps.example.com", u.Hostname())
	}
	if u.Fragment != "my-node" {
		t.Errorf("URI fragment = %q, want my-node", u.Fragment)
	}
}

func TestGetUserSubscriptions_ShadowsocksExplicitCipher(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "ss-c", map[string]any{
		"protocol": "shadowsocks", "port": 10003,
		"password": "pw-ss", "cipher": "chacha20-ietf-poly1305",
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	spec := specs[0]

	if spec.Protocol != contracts.ProtocolShadowsocks {
		t.Errorf("Protocol = %q, want ss", spec.Protocol)
	}
	if spec.Password != "pw-ss" {
		t.Errorf("Password = %q, want pw-ss", spec.Password)
	}
	if method, _ := spec.Extensions["method"].(string); method != "chacha20-ietf-poly1305" {
		t.Errorf("Extensions[method] = %q, want chacha20-ietf-poly1305", method)
	}
	if !strings.HasPrefix(spec.URI, "ss://") {
		t.Fatalf("URI scheme wrong: %q", spec.URI)
	}
}

// TestBuildSubscriptionSpec_ShadowsocksDefaultCipher covers the "cipher-less
// SharedCred" corner case at the unit level. In production flow, profilegen
// rejects SS without cipher (FastAddInbound → BuildConfig fails), so this
// shape is unreachable through addInboundLocked. The default fallback in
// buildSubscriptionSpec is defense-in-depth for a future record-format
// change that drops Cipher — exercising it directly avoids depending on
// profilegen staying permissive.
func TestBuildSubscriptionSpec_ShadowsocksDefaultCipher(t *testing.T) {
	inb := NewMihomoInbound("ss-default", contracts.ProtocolShadowsocks, 10003, MihomoSharedCred{Password: "pw-ss"})

	spec, err := buildSubscriptionSpec(inb, contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	}, 50000)
	if err != nil {
		t.Fatalf("buildSubscriptionSpec: %v", err)
	}
	if method, _ := spec.Extensions["method"].(string); method != defaultSSMethod {
		t.Errorf("Extensions[method] = %q, want default %q", method, defaultSSMethod)
	}
	// The URI must also reflect the default cipher — this is the wire
	// contract the client sees.
	if !strings.HasPrefix(spec.URI, "ss://") {
		t.Fatalf("URI scheme wrong: %q", spec.URI)
	}
}

// TestGetUserSubscriptions_AggregatesMultipleInbounds covers the "one
// user on N inbounds" shape: the user is wired to every inbound by
// reconcileUsers in production, so subscription should return one entry
// per inbound. Result order isn't specified (map iteration), so the test
// builds a tag→spec index for assertions.
func TestGetUserSubscriptions_AggregatesMultipleInbounds(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "vmess-a", map[string]any{
		"protocol": "vmess", "port": 10001, "uuid": "u",
	})
	wireUserAndInbound(t, c, "alice", "trojan-b", map[string]any{
		"protocol": "trojan", "port": 10002, "password": "p",
	})
	wireUserAndInbound(t, c, "alice", "ss-c", map[string]any{
		"protocol": "shadowsocks", "port": 10003,
		"password": "p", "cipher": "aes-128-gcm",
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 3 {
		t.Fatalf("got %d specs, want 3", len(specs))
	}

	byTag := make(map[string]contracts.SubscriptionSpec, 3)
	for _, s := range specs {
		byTag[s.InboundTag] = s
	}
	for _, want := range []string{"vmess-a", "trojan-b", "ss-c"} {
		if _, ok := byTag[want]; !ok {
			t.Errorf("missing inbound %q in subscription output", want)
		}
	}
}

// TestGetUserSubscriptions_SkipsInboundsWithoutMapping verifies the
// behavior mirroring snell/hysteria: if a user is in UserManager but has
// no forward port for a particular inbound (e.g. reconcile hasn't run
// yet, or this inbound was just added), that inbound is silently skipped
// rather than producing a spec with a stale / zero port.
//
// Reproduces the "inbound added AFTER user but user never got wired" shape
// by manually installing the second inbound into c.inbounds (bypassing
// FastAddInbound's auto-reconcile) — mirroring the production race
// window between FastAddInbound.addInboundLocked acquiring inboundsMu and
// reconcileUsersForInbound allocating forward rules.
func TestGetUserSubscriptions_SkipsInboundsWithoutMapping(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	// vmess-a: full production path — alice gets a forward rule here.
	wireUserAndInbound(t, c, "alice", "vmess-a", map[string]any{
		"protocol": "vmess", "port": 10001, "uuid": "u",
	})

	// trojan-b: installed via the internal path WITHOUT reconcile, so
	// alice has no forward rule for it. This is the race window we want
	// GetUserSubscriptions to handle — skip rather than emit a spec
	// with a zero port.
	orphan := NewMihomoInbound("trojan-b", contracts.ProtocolTrojan, 10002, MihomoSharedCred{Password: "p"})
	orphan.SetUserManager(um)
	c.inboundsMu.Lock()
	c.inbounds["trojan-b"] = orphan
	c.inboundsMu.Unlock()

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1 (only the mapped inbound)", len(specs))
	}
	if specs[0].InboundTag != "vmess-a" {
		t.Errorf("expected only vmess-a to produce a spec, got %q", specs[0].InboundTag)
	}
}

// TestGetUserSubscriptions_EmptyContainer exercises the "no inbounds" path.
// A freshly-started container with no inbounds yet must return ([], nil),
// not an error — HTTP /sub stitches results from every container and
// expects zero-length output here, not a failure that aborts the render.
func TestGetUserSubscriptions_EmptyContainer(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 0 {
		t.Errorf("empty container should return 0 specs, got %d", len(specs))
	}
}

// TestBuildSubscriptionSpec_UnsupportedProtocolErrors asserts the helper
// returns a typed error for anything outside the MVP trio. This is what
// GetUserSubscriptions catches via errors.Is to log+skip rather than fail
// the whole subscription render.
func TestBuildSubscriptionSpec_UnsupportedProtocolErrors(t *testing.T) {
	// Construct a MihomoInbound with an unsupported protocol directly —
	// NewMihomoInbound + Validate would reject it, but buildSubscriptionSpec
	// is the defense-in-depth guard for a record that slipped through.
	// Phase 6 added tuic; anytls is the unsupported-protocol sentinel until
	// Phase 7 lands.
	base := NewMihomoInbound("exotic", contracts.ProtocolAnyTLS, 10001, MihomoSharedCred{Password: "p"})

	req := contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	}

	_, err := buildSubscriptionSpec(base, req, 50000)
	if err == nil {
		t.Fatal("expected error for unsupported protocol")
	}
	if !strings.Contains(err.Error(), "protocol not supported") {
		t.Errorf("error should mention unsupported protocol: %v", err)
	}
}

// TestBuildSubscriptionSpec_EmptyCredentialErrors ensures the emptiness
// guards in the per-protocol branches fire. These are the protection
// against a corrupt record (stored with non-empty SharedCred, then the
// operator set a field to empty via a future admin flow) reaching the URI
// generator and producing an empty-UUID vmess link etc.
func TestBuildSubscriptionSpec_EmptyCredentialErrors(t *testing.T) {
	cases := []struct {
		name string
		inb  *MihomoInbound
	}{
		{
			name: "vmess empty uuid",
			inb:  NewMihomoInbound("vm", contracts.ProtocolVMess, 10001, MihomoSharedCred{}),
		},
		{
			name: "trojan empty password",
			inb:  NewMihomoInbound("tj", contracts.ProtocolTrojan, 10001, MihomoSharedCred{}),
		},
		{
			name: "shadowsocks empty password",
			inb:  NewMihomoInbound("ss", contracts.ProtocolShadowsocks, 10001, MihomoSharedCred{Cipher: "aes-256-gcm"}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildSubscriptionSpec(tc.inb, contracts.SubscriptionRequest{
				User: contracts.UserSpec{Username: "alice"},
				Host: "vps.example.com",
			}, 50000)
			if err == nil {
				t.Fatal("expected error for empty credential")
			}
		})
	}
}
