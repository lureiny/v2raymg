package forward

import (
	"testing"

	errs "github.com/lureiny/v2raymg/pkg/proxy/errors"
)

// These tests cover AllocateSpecificPort at the manager level — the entry point
// containers use to claim an inbound's backend port. The invariant they protect
// is the whole point of routing every component through one allocator: a port
// claimed by an inbound must never be drawn for a forward rule, and vice versa.
//
// The allocator-level counterpart lives in port_allocator_test.go; this file
// exercises the path that actually binds sockets, reusing occupyPort from
// bind_retry_test.go.

func TestAllocateSpecificPort_BlocksAddRuleFromTakingIt(t *testing.T) {
	const base = 28800
	// Two-port range so AddRule has exactly one legal choice once we claim one.
	m := newRetryTestManager(t, base, base+1)

	if err := m.AllocateSpecificPort(base); err != nil {
		t.Fatalf("AllocateSpecificPort(%d): %v", base, err)
	}

	rule, err := m.AddRule(retryTestRule("claim-a"))
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	if rule.ListenPort == base {
		t.Fatalf("AddRule drew the port claimed for an inbound (%d)", base)
	}

	// The range is now exhausted: one port claimed, one relayed.
	if _, err := m.AddRule(retryTestRule("claim-b")); err == nil {
		t.Fatal("expected exhaustion; the claimed port must not be reusable")
	}
}

func TestAllocateSpecificPort_RejectsAPortAddRuleAlreadyHolds(t *testing.T) {
	const base = 28810
	m := newRetryTestManager(t, base, base)

	rule, err := m.AddRule(retryTestRule("held"))
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	err = m.AllocateSpecificPort(rule.ListenPort)
	if err == nil {
		t.Fatalf("expected AllocateSpecificPort(%d) to fail; a forward rule holds it", rule.ListenPort)
	}
	if !errs.HasCode(err, errs.ErrPortInUse) {
		t.Fatalf("want ErrPortInUse, got %v", err)
	}
}

// A claim outside the draw range must still be honoured. This is the restart
// case: a persisted inbound port can sit anywhere, and if the allocator refuses
// to record it, nothing stops a forward rule from binding the same port later.
func TestAllocateSpecificPort_AcceptsPortOutsideDrawRange(t *testing.T) {
	m := newRetryTestManager(t, 28820, 28829)

	if err := m.AllocateSpecificPort(28900); err != nil {
		t.Fatalf("claim outside the draw range should succeed: %v", err)
	}
	if err := m.AllocateSpecificPort(28900); !errs.HasCode(err, errs.ErrPortInUse) {
		t.Fatalf("re-claim should report ErrPortInUse, got %v", err)
	}

	m.ReleasePort(28900)
	if err := m.AllocateSpecificPort(28900); err != nil {
		t.Fatalf("claim after release should succeed: %v", err)
	}
}

func TestAllocateSpecificPort_RejectsUnbindableAndClosed(t *testing.T) {
	m := newRetryTestManager(t, 28830, 28839)

	if err := m.AllocateSpecificPort(80); !errs.HasCode(err, errs.ErrPortAllocationFail) {
		t.Fatalf("privileged port: want ErrPortAllocationFail, got %v", err)
	}

	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := m.AllocateSpecificPort(28831); !errs.HasCode(err, errs.ErrPortAllocationFail) {
		t.Fatalf("closed manager: want ErrPortAllocationFail, got %v", err)
	}
}
