package forward

// bind_retry_test.go — regressions for the port-collision handling in AddRule.
//
// PortAllocator only tracks the ports it has handed out; it never asks the OS
// whether one is bindable. The default range (10000-60000) straddles Linux's
// ephemeral range (32768-60999), so a port the allocator believes is free can be
// held by any outbound connection on the host. Before AddRule retried, a single
// collision failed the rule outright — a user silently got no forward, and it was
// the root cause of a CI flake that only reproduced on busy machines.
//
// NOTE on determinism: NewDefaultForwardManager forces UseRandom=true, so these
// tests cannot rely on which port is drawn first. They are instead built so the
// OUTCOME is deterministic — every port but one is occupied, so a success is only
// reachable by retrying — and the scenario is repeated enough times that the old
// no-retry behaviour cannot pass by luck.

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
)

// occupyPort makes a port genuinely unusable for a dual-stack forward rule.
//
// Binding ":port" gives a dual-stack socket (bindv6only=0), which blocks both the
// IPv4 and IPv6 wildcard. That matters: a multiRelay treats BOTH halves as
// optional, so occupying only one stack would let the other half bind and Start()
// would succeed on the "occupied" port — the retry path would never run and the
// test would pass for the wrong reason. The helper verifies the block instead of
// assuming it.
func occupyPort(t *testing.T, port uint32) {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Skipf("cannot occupy port %d: %v", port, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	probe, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err == nil {
		_ = probe.Close()
		t.Skipf("port %d is not blocked on IPv4 by the dual-stack listener "+
			"(bindv6only=1?); this test cannot create a real collision here", port)
	}
}

func newRetryTestManager(t *testing.T, minPort, maxPort uint32) *DefaultForwardManager {
	t.Helper()
	m, err := NewDefaultForwardManager(PortAllocatorConfig{MinPort: minPort, MaxPort: maxPort})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func retryTestRule(tag string) ForwardRule {
	return ForwardRule{
		ContainerType: "xray",
		InboundTag:    tag,
		Username:      "retry-user",
		TargetAddr:    "127.0.0.1:9",
	}
}

// TestAddRule_RetriesOntoTheOnlyFreePort is the core regression. Three of the four
// ports in the range are held by foreign listeners, so the single free port is
// reachable only by retrying past the collisions.
//
// Repeated with a fresh manager each round because allocation is random: without
// the retry, a round succeeds only when the first draw happens to be the free port
// (1 in 4), so passing all rounds by luck is under 0.1%.
func TestAddRule_RetriesOntoTheOnlyFreePort(t *testing.T) {
	const base = 28300
	const rounds = 5

	for round := 0; round < rounds; round++ {
		minPort := uint32(base + round*10)
		maxPort := minPort + 3 // 4 ports
		freePort := maxPort    // occupy every port except the last

		t.Run(fmt.Sprintf("round%d", round), func(t *testing.T) {
			for p := minPort; p < freePort; p++ {
				occupyPort(t, p)
			}
			m := newRetryTestManager(t, minPort, maxPort)

			got, err := m.AddRule(retryTestRule("retry-test"))
			if err != nil {
				t.Fatalf("AddRule should have retried past the %d occupied ports onto %d, got: %v",
					freePort-minPort, freePort, err)
			}
			if got.ListenPort != freePort {
				t.Errorf("bound port %d, want the only free one %d", got.ListenPort, freePort)
			}
		})
	}
}

// TestAddRule_RetryReleasesSkippedPorts asserts ports skipped during the loop go
// back to the pool. Holding them for the duration is deliberate (so a retry cannot
// draw the same port twice), but keeping them afterwards would slowly drain the
// range on a host with steady port churn.
func TestAddRule_RetryReleasesSkippedPorts(t *testing.T) {
	const minPort, maxPort = 28400, 28403 // 4 ports
	occupyPort(t, minPort)                // 3 remain usable

	m := newRetryTestManager(t, minPort, maxPort)

	seen := map[uint32]bool{}
	for i := 0; i < 3; i++ {
		got, err := m.AddRule(retryTestRule(fmt.Sprintf("leak-test-%d", i)))
		if err != nil {
			t.Fatalf("AddRule #%d failed with %d usable ports left — a skipped port was "+
				"not released back to the pool: %v", i+1, 3-i, err)
		}
		if seen[got.ListenPort] {
			t.Errorf("port %d handed out twice", got.ListenPort)
		}
		seen[got.ListenPort] = true
		if got.ListenPort == minPort {
			t.Errorf("bound the occupied port %d", minPort)
		}
	}
}

// TestAddRule_FailsWhenEveryPortIsTaken: the retry must give up rather than spin,
// and the returned error must still carry the underlying conflict.
func TestAddRule_FailsWhenEveryPortIsTaken(t *testing.T) {
	const minPort, maxPort = 28500, 28502
	for p := uint32(minPort); p <= maxPort; p++ {
		occupyPort(t, p)
	}

	m := newRetryTestManager(t, minPort, maxPort)

	_, err := m.AddRule(retryTestRule("exhausted"))
	if err == nil {
		t.Fatal("AddRule succeeded although every port in the range was occupied")
	}
	if !isAddrInUse(err) {
		t.Errorf("error lost the underlying conflict, operator cannot tell why: %v", err)
	}
}

// TestAddRule_PinnedPortIsNeverSubstituted: when the caller names a port, that port
// is part of the request — a client has been told to connect there. Silently
// binding a different one would be worse than failing.
func TestAddRule_PinnedPortIsNeverSubstituted(t *testing.T) {
	const minPort, maxPort = 28600, 28605
	occupyPort(t, minPort)

	m := newRetryTestManager(t, minPort, maxPort)

	rule := retryTestRule("pinned")
	rule.ListenPort = minPort // the occupied one, explicitly requested

	if got, err := m.AddRule(rule); err == nil {
		t.Fatalf("AddRule moved a caller-pinned port %d to %d instead of failing",
			minPort, got.ListenPort)
	}
}

// TestAddRule_DoesNotRetryOnNonConflictError: an unbindable ADDRESS (as opposed to
// a taken port) will not become bindable on another port, so the retry must not
// mask it — the operator needs the real error.
func TestAddRule_DoesNotRetryOnNonConflictError(t *testing.T) {
	m := newRetryTestManager(t, 28700, 28705)

	rule := retryTestRule("bad-addr")
	// TEST-NET-1; belongs to no interface, so bind fails with EADDRNOTAVAIL.
	rule.ListenAddr = "192.0.2.1"

	_, err := m.AddRule(rule)
	if err == nil {
		t.Skip("host unexpectedly allows binding 192.0.2.1; nothing to assert")
	}
	if isAddrInUse(err) {
		t.Fatalf("expected a non-conflict bind error, got an address-in-use one: %v", err)
	}
}

// TestIsAddrInUse_SeesThroughWrapping guards the predicate the whole retry hinges
// on. The bind error arrives wrapped through net.OpError -> os.SyscallError ->
// syscall.Errno, and for dual-stack rules additionally through multiRelay's
// errors.Join; a direct comparison would silently never match and disable the
// retry entirely.
func TestIsAddrInUse_SeesThroughWrapping(t *testing.T) {
	inUse := &net.OpError{Op: "listen", Net: "tcp", Err: syscall.EADDRINUSE}

	cases := map[string]struct {
		err  error
		want bool
	}{
		"bare errno":            {syscall.EADDRINUSE, true},
		"net.OpError":           {inUse, true},
		"fmt wrapped":           {fmt.Errorf("relay start: %w", inUse), true},
		"joined with a sibling": {errors.Join(errors.New("no ipv6"), inUse), true},
		"multiRelay shape": {fmt.Errorf("no listener could be started: %w",
			errors.Join(fmt.Errorf("[::]:1: %w", inUse), fmt.Errorf("0.0.0.0:1: %w", inUse))), true},
		"different errno": {&net.OpError{Op: "listen", Err: syscall.EADDRNOTAVAIL}, false},
		"unrelated":       {errors.New("boom"), false},
		"nil":             {nil, false},
	}
	for name, tc := range cases {
		if got := isAddrInUse(tc.err); got != tc.want {
			t.Errorf("%s: isAddrInUse = %v, want %v (err: %v)", name, got, tc.want, tc.err)
		}
	}
}

// TestMultiRelay_TotalFailureWrapsChildErrors guards the error plumbing the retry
// depends on. When every child is optional and every one fails, multiRelay used to
// return a bare "no listener could be started", discarding the errno — which both
// disabled the retry for dual-stack rules and made a real CI failure impossible to
// diagnose.
func TestMultiRelay_TotalFailureWrapsChildErrors(t *testing.T) {
	// Occupy a port on both stacks so both halves of a dual-stack rule fail.
	const port = 28800
	occupyPort(t, port)

	m := newRetryTestManager(t, port, port) // single-port range: no retry possible

	_, err := m.AddRule(retryTestRule("multirelay"))
	if err == nil {
		t.Fatal("AddRule succeeded on a fully occupied single-port range")
	}
	if !isAddrInUse(err) {
		t.Errorf("multiRelay swallowed the child errno; retry logic and operators "+
			"both depend on it surviving: %v", err)
	}
}
