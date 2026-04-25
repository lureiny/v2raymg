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

// TestParseAllContractsProtocolsHaveBranch is the safety net for "added a
// new contracts.Protocol constant but forgot to wire it into Parse".
// Every protocol that the FastAdd/ProtocolParams layer is expected to
// handle MUST be enumerated here; calling Parse with that protocol
// (and minimal credentials) MUST NOT return ErrProtocolNotSupported.
//
// History: Phase 0 stubbed every branch; Phases 1-7 flipped them one by
// one. Earlier this test was named TestParseKnownProtocolStub and tracked
// the *un*-implemented set, which became an empty slice after Phase 7
// (and silently passed for any future additions). Inverting the contract
// means the test stays meaningful: a future ProtocolXxx that lacks a
// parser branch is caught immediately.
//
// Protocols intentionally NOT routed through this layer (snell / socks5 /
// http / direct) live elsewhere and aren't exercised here.
func TestParseAllContractsProtocolsHaveBranch(t *testing.T) {
	expected := []contracts.Protocol{
		contracts.ProtocolVLess,
		contracts.ProtocolVMess,
		contracts.ProtocolTrojan,
		contracts.ProtocolShadowsocks,
		contracts.ProtocolHysteria2,
		contracts.ProtocolTUIC,
		contracts.ProtocolAnyTLS,
	}
	// Per-protocol minimal raw maps. Some parsers still require structural
	// fields (uuid / cipher) — supply them so the call surfaces only
	// ErrProtocolNotSupported when the dispatch is missing, rather than a
	// downstream domain error.
	for _, proto := range expected {
		t.Run(string(proto), func(t *testing.T) {
			raw := map[string]any{
				KeyProtocol: string(proto),
				KeyPort:     uint32(1080),
				KeyUUID:     "00112233-4455-6677-8899-aabbccddeeff",
				KeyPassword: "p",
				KeyCipher:   "aes-256-gcm",
			}
			_, err := Parse(raw)
			if errors.Is(err, ErrProtocolNotSupported) {
				t.Errorf("%s: Parse returned ErrProtocolNotSupported — switch case missing in parser.go?", proto)
			}
			// Any other error (missing TLS material, invalid combination,
			// etc.) is fine — this test only catches dispatch gaps.
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
