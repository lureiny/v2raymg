package usermanager

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

// newTestManagerWithForward creates a UserManager with a real ForwardManager for rotate tests.
func newTestManagerWithForward(t *testing.T) (*UserManager, *forward.DefaultForwardManager) {
	t.Helper()
	fwdMgr, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort: 30000,
		MaxPort: 40000,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	// Close on cleanup so relays are stopped and their ports released between
	// tests; otherwise leaked listeners stay bound and a later test's random
	// allocation can re-pick a still-bound port (EADDRINUSE).
	t.Cleanup(func() { _ = fwdMgr.Close() })
	mgr := NewUserManager(fwdMgr, "test-node")
	return mgr, fwdMgr
}
