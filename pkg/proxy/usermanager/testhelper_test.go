package usermanager

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

// newTestManagerWithForward creates a UserManager with a real ForwardManager for rotate tests.
func newTestManagerWithForward(t *testing.T) (*UserManager, *forward.DefaultForwardManager) {
	t.Helper()
	fwdMgr, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
	// Port window deliberately BELOW the Linux ephemeral range (32768-60999).
	// PortAllocator does not ask the OS whether a port is free, so a pool that
	// overlaps the ephemeral range collides with outbound connections made by
	// other tests running in parallel — that was a real CI flake. AddRule now
	// retries past a collision, but tests should not depend on that.
		MinPort: 22000,
		MaxPort: 22999,
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
