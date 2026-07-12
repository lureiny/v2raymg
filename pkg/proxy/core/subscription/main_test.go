package subscription

import (
	"os"
	"testing"
)

// TestMain disables the SSRF connection guard for this package's tests, which
// dial httptest servers on 127.0.0.1. Tests that exercise the guard itself call
// classifyBlockedAddr directly (a pure, un-gated function) or re-enable the
// guard locally.
func TestMain(m *testing.M) {
	restore := SetSSRFGuardEnabledForTest(false)
	code := m.Run()
	restore()
	os.Exit(code)
}
