package hysteria

import (
	"testing"
)

// TestFastAddInbound_LegacyPath_Regression ensures Phase 5's mihomo hy2 work
// did not disturb the hysteria container's pre-existing FastAddInbound contract:
//
//   - Only the canonical defaultInboundTag ("hysteria-default") is accepted.
//   - The params map is intentionally ignored — hy2 protocol-level config
//     (password/obfs/up/down/...) lives in the container's HysteriaConfig
//     rather than per-inbound, and stage 5 is explicit about not changing
//     this. Phase 5 routed all new hy2 work through the mihomo container
//     instead, so the hysteria container stays a single-inbound legacy shape.
//   - inboundEnabled flips to true on success.
func TestFastAddInbound_LegacyPath_Regression(t *testing.T) {
	hc, err := NewHysteriaContainer(HysteriaConfig{
		Listen: "127.0.0.1",
		Port:   9443,
	})
	if err != nil {
		t.Fatalf("NewHysteriaContainer: %v", err)
	}

	// Default tag with no params should succeed (Phase 5 baseline).
	if err := hc.FastAddInbound(defaultInboundTag, nil); err != nil {
		t.Fatalf("FastAddInbound(default, nil): %v", err)
	}
	if !hc.inboundEnabled {
		t.Fatal("inboundEnabled should be true after FastAddInbound")
	}

	// Default tag with arbitrary params should still succeed; params are ignored.
	if err := hc.FastAddInbound(defaultInboundTag, map[string]any{
		"protocol":                "hysteria2",
		"port":                    uint32(443),
		"password":                "ignored-by-design",
		"obfs":                    "salamander",
		"this_key_does_not_exist": "and that's fine",
	}); err != nil {
		t.Fatalf("FastAddInbound(default, params): %v", err)
	}

	// Non-default tag rejected.
	if err := hc.FastAddInbound("some-other-tag", nil); err == nil {
		t.Fatal("FastAddInbound with non-default tag should be rejected")
	}
}
