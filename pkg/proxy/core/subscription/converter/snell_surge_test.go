package converter

import (
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestSurgeConvertSnell_Version_DefaultsTo5(t *testing.T) {
	c := &SurgeConverter{}
	line, ok := c.convertSpec(contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolSnell,
		Host:     "h", Port: 443, Password: "psk",
		Extensions: map[string]any{},
	})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !strings.Contains(line, "version=5") {
		t.Errorf("line = %q, want to contain version=5", line)
	}
}

// Regression: convertSnell must not silently rewrite the user's version.
// Previously it hardcoded "version=5" regardless of spec.Extensions.
func TestSurgeConvertSnell_Version_RespectsSpec(t *testing.T) {
	c := &SurgeConverter{}
	line, ok := c.convertSpec(contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolSnell,
		Host:     "h", Port: 443, Password: "psk",
		Extensions: map[string]any{"version": 3},
	})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !strings.Contains(line, "version=3") {
		t.Errorf("line = %q, want to contain version=3 (not rewritten to 5)", line)
	}
	if strings.Contains(line, "version=5") {
		t.Errorf("line = %q, must not contain version=5 (stale hardcode)", line)
	}
}
