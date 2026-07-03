package forward

import (
	"errors"
	"strings"
	"testing"
)

// fakeRelay is a controllable Relay for exercising multiRelay start/stop logic
// without binding real sockets (so the IPv4-only / IPv6-only degradation paths
// are testable on any host).
type fakeRelay struct {
	addr     string
	startErr error
	started  bool
	stopped  bool
}

func (f *fakeRelay) Start() error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = true
	return nil
}
func (f *fakeRelay) Stop()              { f.stopped = true }
func (f *fakeRelay) ListenAddr() string { return f.addr }

// IPv6-only host: the optional IPv4 half fails to bind, the IPv6 half succeeds,
// and the rule still comes up on IPv6.
func TestMultiRelay_IPv6OnlyHost(t *testing.T) {
	v4 := &fakeRelay{addr: "0.0.0.0:9000", startErr: errors.New("cannot assign requested address")}
	v6 := &fakeRelay{addr: "[::]:9000"}
	mr := newMultiRelay([]relayChild{
		{relay: v4, optional: true},
		{relay: v6, optional: true},
	})

	if err := mr.Start(); err != nil {
		t.Fatalf("Start should succeed when the IPv6 half binds: %v", err)
	}
	if v4.started {
		t.Error("IPv4 half should not be marked started")
	}
	if !v6.started {
		t.Error("IPv6 half should be started")
	}
	if got := mr.ListenAddr(); got != "[::]:9000" {
		t.Errorf("ListenAddr = %q, want only the IPv6 address", got)
	}
	mr.Stop()
	if !v6.stopped {
		t.Error("Stop should stop the started IPv6 half")
	}
}

// IPv4-only host: the optional IPv6 half fails, the IPv4 half succeeds.
func TestMultiRelay_IPv4OnlyHost(t *testing.T) {
	v4 := &fakeRelay{addr: "0.0.0.0:9000"}
	v6 := &fakeRelay{addr: "[::]:9000", startErr: errors.New("address family not supported")}
	mr := newMultiRelay([]relayChild{
		{relay: v4, optional: true},
		{relay: v6, optional: true},
	})

	if err := mr.Start(); err != nil {
		t.Fatalf("Start should succeed when the IPv4 half binds: %v", err)
	}
	if got := mr.ListenAddr(); got != "0.0.0.0:9000" {
		t.Errorf("ListenAddr = %q, want only the IPv4 address", got)
	}
}

// Neither family can bind: the rule must fail.
func TestMultiRelay_NoFamilyBinds(t *testing.T) {
	v4 := &fakeRelay{addr: "0.0.0.0:9000", startErr: errors.New("boom4")}
	v6 := &fakeRelay{addr: "[::]:9000", startErr: errors.New("boom6")}
	mr := newMultiRelay([]relayChild{
		{relay: v4, optional: true},
		{relay: v6, optional: true},
	})
	if err := mr.Start(); err == nil {
		t.Fatal("Start must fail when no endpoint binds")
	}
}

// A required endpoint failing rolls back any already-started children.
func TestMultiRelay_RequiredFailureRollsBack(t *testing.T) {
	ok := &fakeRelay{addr: "0.0.0.0:9000"}
	required := &fakeRelay{addr: "[::]:9000", startErr: errors.New("hard fail")}
	mr := newMultiRelay([]relayChild{
		{relay: ok, optional: true},
		{relay: required, optional: false},
	})
	if err := mr.Start(); err == nil {
		t.Fatal("Start must fail when a required endpoint fails")
	}
	if !ok.stopped {
		t.Error("already-started child must be stopped on rollback")
	}
	if strings.TrimSpace(mr.ListenAddr()) != "" {
		t.Errorf("ListenAddr should be empty after a failed Start, got %q", mr.ListenAddr())
	}
}
