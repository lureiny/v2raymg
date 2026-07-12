package converter

import (
	"os"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
)

// TestMain disables the SSRF connection guard for this package's tests (they
// dial httptest servers on 127.0.0.1). The guard is exercised directly in the
// subscription package's own tests.
func TestMain(m *testing.M) {
	restore := subscription.SetSSRFGuardEnabledForTest(false)
	code := m.Run()
	restore()
	os.Exit(code)
}
