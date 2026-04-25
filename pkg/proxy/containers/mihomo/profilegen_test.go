package mihomo

import (
	"errors"
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestBuildListener_Vmess(t *testing.T) {
	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "uuid-a"})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	want := map[string]any{
		"name":   "vmess-a",
		"type":   "vmess",
		"listen": "127.0.0.1",
		"port":   "10001",
		"users": []map[string]any{
			{"uuid": "uuid-a", "alterId": 0},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("vmess listener map mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBuildListener_Trojan(t *testing.T) {
	inb := NewMihomoInbound("trojan-a", contracts.ProtocolTrojan, 10002, MihomoSharedCred{Password: "pw-a"})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	want := map[string]any{
		"name":   "trojan-a",
		"type":   "trojan",
		"listen": "127.0.0.1",
		"port":   "10002",
		"users": []map[string]any{
			{"password": "pw-a"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("trojan listener map mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBuildListener_Shadowsocks(t *testing.T) {
	// shadowsocks carries password/cipher on the listener root, NOT in a
	// users[] array — critical distinction from vmess/trojan.
	inb := NewMihomoInbound("ss-a", contracts.ProtocolShadowsocks, 10003, MihomoSharedCred{Password: "pw-a", Cipher: "aes-128-gcm"})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	want := map[string]any{
		"name":     "ss-a",
		"type":     "shadowsocks",
		"listen":   "127.0.0.1",
		"port":     "10003",
		"password": "pw-a",
		"cipher":   "aes-128-gcm",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("shadowsocks listener map mismatch:\n got: %#v\nwant: %#v", got, want)
	}
	if _, hasUsers := got["users"]; hasUsers {
		t.Errorf("shadowsocks must not have users[] field, got: %#v", got["users"])
	}
}

func TestBuildListener_StringifiedPort(t *testing.T) {
	// BaseOption.Port is a string tag (supports range syntax "1000-2000").
	// Passing an int would break mihomo's decoder. Lock in the stringify.
	inb := NewMihomoInbound("t", contracts.ProtocolVMess, 65535, MihomoSharedCred{UUID: "u"})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	if port, ok := got["port"].(string); !ok || port != "65535" {
		t.Errorf("port should be stringified, got %T: %v", got["port"], got["port"])
	}
}

func TestBuildListener_TagLiteralKeysForMihomoDecoder(t *testing.T) {
	// Regression guard: mihomo decodes each listener map with
	// structure.NewDecoder(TagName:"inbound"), so every yaml-level key
	// must match the `inbound:"..."` literal values on mihomo's Option
	// structs (see listener/inbound/{base,vmess,trojan,shadowsocks}.go).
	// If BuildListener is ever refactored to use Go struct marshalling
	// with `yaml:"..."` tags, mihomo's decoder silently drops the
	// unrecognised keys and the listener runs with zero-value defaults.
	//
	// Each protocol has its own set of keys below; the assertions flag
	// both "missing expected key" and "present but wrong casing/name"
	// (the two realistic drift modes).

	t.Run("base fields shared across protocols", func(t *testing.T) {
		m, _ := BuildListener(NewMihomoInbound("t", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"}))
		for _, k := range []string{"name", "type", "listen", "port"} {
			if _, ok := m[k]; !ok {
				t.Errorf("base field %q missing from listener map: %#v", k, m)
			}
		}
		// Common renaming mistakes — all forbidden.
		for _, bad := range []string{"tag", "Type", "Listen", "Port", "listen-addr", "listenAddr"} {
			if _, ok := m[bad]; ok {
				t.Errorf("unexpected base field %q (renames of the mihomo literal)", bad)
			}
		}
	})

	t.Run("vmess user fields", func(t *testing.T) {
		m, _ := BuildListener(NewMihomoInbound("t", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"}))
		users := m["users"].([]map[string]any)
		if len(users) != 1 {
			t.Fatalf("users length = %d", len(users))
		}
		// Required: uuid, alterId — the exact `inbound:` tag literals.
		for _, k := range []string{"uuid", "alterId"} {
			if _, ok := users[0][k]; !ok {
				t.Errorf("vmess user field %q missing: %#v", k, users[0])
			}
		}
		// Forbidden: upper-case / snake-case / v2ray-style renames.
		for _, bad := range []string{"UUID", "id", "Id", "alter_id", "alter-id", "alterID", "alterid"} {
			if _, ok := users[0][bad]; ok {
				t.Errorf("unexpected vmess user field %q (not a mihomo tag literal)", bad)
			}
		}
	})

	t.Run("trojan user fields", func(t *testing.T) {
		m, _ := BuildListener(NewMihomoInbound("t", contracts.ProtocolTrojan, 10001, MihomoSharedCred{Password: "p"}))
		users := m["users"].([]map[string]any)
		if len(users) != 1 {
			t.Fatalf("users length = %d", len(users))
		}
		if _, ok := users[0]["password"]; !ok {
			t.Errorf("trojan user must have password key: %#v", users[0])
		}
		for _, bad := range []string{"Password", "pass", "psk"} {
			if _, ok := users[0][bad]; ok {
				t.Errorf("unexpected trojan user field %q", bad)
			}
		}
	})

	t.Run("shadowsocks root fields (no users array)", func(t *testing.T) {
		m, _ := BuildListener(NewMihomoInbound("t", contracts.ProtocolShadowsocks, 10001, MihomoSharedCred{Password: "p", Cipher: "aes-128-gcm"}))
		for _, k := range []string{"password", "cipher"} {
			if _, ok := m[k]; !ok {
				t.Errorf("ss field %q missing at listener root: %#v", k, m)
			}
		}
		// Forbidden: treating SS like vmess/trojan (users[]), or the
		// sing-box-style `psk` used by some SS2022 variants which is the
		// most plausible confusion.
		if _, ok := m["users"]; ok {
			t.Errorf("ss must not carry a users[] array: %#v", m["users"])
		}
		for _, bad := range []string{"psk", "Password", "Cipher", "method"} {
			if _, ok := m[bad]; ok {
				t.Errorf("unexpected ss field %q", bad)
			}
		}
	})
}

func TestBuildListener_RejectsInvalid(t *testing.T) {
	// BuildListener calls Validate first, so the errors from the inbound
	// layer bubble through.
	_, err := BuildListener(NewMihomoInbound("t", contracts.ProtocolVMess, 10001, MihomoSharedCred{}))
	if !errors.Is(err, ErrMissingCredential) {
		t.Errorf("expected ErrMissingCredential, got %v", err)
	}

	// Phase 7 wired anytls. Synthesise an unknown contracts.Protocol
	// value so the BuildListener default branch fires.
	_, err = BuildListener(NewMihomoInbound("t", contracts.Protocol("nonesuch"), 10001, MihomoSharedCred{Password: "p"}))
	if !errors.Is(err, ErrProtocolNotSupported) {
		t.Errorf("expected ErrProtocolNotSupported, got %v", err)
	}
}
