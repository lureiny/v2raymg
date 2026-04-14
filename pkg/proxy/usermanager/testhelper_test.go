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
	mgr := NewUserManager(fwdMgr, "test-node")
	return mgr, fwdMgr
}
