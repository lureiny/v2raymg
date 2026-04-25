package protocolparams

import (
	"errors"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestParseNilMap(t *testing.T) {
	_, err := Parse(nil)
	if err == nil {
		t.Fatal("Parse(nil) returned no error")
	}
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("Parse(nil) err = %v, want ErrMissingRequired", err)
	}
}

func TestParseMissingProtocol(t *testing.T) {
	_, err := Parse(map[string]any{KeyPort: 443})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("err = %v, want ErrMissingRequired", err)
	}
}

func TestParseUnknownProtocol(t *testing.T) {
	_, err := Parse(map[string]any{KeyProtocol: "nonesuch", KeyPort: 443})
	if !errors.Is(err, ErrProtocolNotSupported) {
		t.Errorf("err = %v, want ErrProtocolNotSupported", err)
	}
}

// TestParseKnownProtocolStub confirms the still-stubbed protocol branches
// return "not yet implemented" rather than a zero value. Protocols whose
// branch has landed (Phase 1+ flips them over) should move to their own
// per-protocol test file and be removed from this list.
//
// Stub list shrinks one entry per phase. VLESS flipped in Phase 1, VMess
// in Phase 2, Trojan in Phase 3, Shadowsocks in Phase 4, Hysteria2 in
// Phase 5, TUIC in Phase 6.
func TestParseKnownProtocolStub(t *testing.T) {
	cases := []contracts.Protocol{
		contracts.ProtocolAnyTLS,
	}
	for _, proto := range cases {
		t.Run(string(proto), func(t *testing.T) {
			raw := map[string]any{
				KeyProtocol: string(proto),
				KeyPort:     uint32(1080),
			}
			_, err := Parse(raw)
			if !errors.Is(err, ErrProtocolNotSupported) {
				t.Errorf("%s Parse err = %v, want ErrProtocolNotSupported", proto, err)
			}
		})
	}
}

func TestParseShorthandSS(t *testing.T) {
	// "ss" folds to "shadowsocks" by normaliseProtocolString; verify the alias
	// routes correctly into parseSS (which requires password+cipher).
	_, err := Parse(map[string]any{KeyProtocol: "ss", KeyPort: uint32(8388)})
	if err == nil {
		t.Fatal("expected error for missing password+cipher, got nil")
	}
	// Should fail on missing password (not ErrProtocolNotSupported)
	if errors.Is(err, ErrProtocolNotSupported) {
		t.Errorf("ss alias should route to parseSS, not return ErrProtocolNotSupported; got: %v", err)
	}
}

func TestParsePortAsFloat64(t *testing.T) {
	// JSON numbers round-trip as float64. Confirm Parse accepts that shape.
	// vless is now implemented (Phase 1) so we get a real ProtocolParams
	// back — just confirm the port parse itself didn't choke.
	_, err := Parse(map[string]any{KeyProtocol: "vless", KeyPort: float64(443), KeyUUID: "abc"})
	if errors.Is(err, ErrMissingRequired) && containsSubstring(err.Error(), "port") {
		t.Errorf("port parse rejected float64: %v", err)
	}
}

func TestParsePortOutOfRange(t *testing.T) {
	_, err := Parse(map[string]any{KeyProtocol: "vless", KeyPort: 100000, KeyUUID: "abc"})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("out-of-range port err = %v, want ErrMissingRequired", err)
	}
}

func containsSubstring(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
