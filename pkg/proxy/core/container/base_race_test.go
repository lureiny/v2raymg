package container

import (
	"sync"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// gatedProc models a container process so tests can pin Start/Stop
// interleavings and observe whether the "process" is left running.
type gatedProc struct {
	mu          sync.Mutex
	running     bool
	doubleStart bool
}

func (p *gatedProc) start() {
	p.mu.Lock()
	if p.running {
		p.doubleStart = true
	}
	p.running = true
	p.mu.Unlock()
}

func (p *gatedProc) stop() {
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
}

func (p *gatedProc) isRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

func (p *gatedProc) hadDoubleStart() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.doubleStart
}

// TestBaseContainer_StopDuringStartingRun_NoOrphan pins finding #2: a Stop that
// arrives while a Start's runFunc is still executing must not leave the process
// running once both calls return. Before opMu, Stop observed state=Starting,
// took the Starting branch (which never calls stopFunc), and returned "stopped"
// while the run hook went on to start the process — an orphan. With opMu, Stop
// blocks until Start finishes and then stops normally.
//
// This is a logical race (the state lies), not a memory race, so it is pinned
// deterministically with gates rather than relying on -race.
func TestBaseContainer_StopDuringStartingRun_NoOrphan(t *testing.T) {
	proc := &gatedProc{}
	gate := make(chan struct{})
	entered := make(chan struct{})
	hooks := &mockHooks{
		runFunc: func() error {
			close(entered)
			<-gate
			proc.start()
			return nil
		},
		stopFunc: func() error {
			proc.stop()
			return nil
		},
	}
	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	startDone := make(chan struct{})
	go func() { defer close(startDone); _ = bc.Start() }()
	<-entered // run hook is executing, blocked on the gate

	stopDone := make(chan struct{})
	go func() { defer close(stopDone); _ = bc.Stop() }()

	// Give Stop a chance to progress: pre-fix it races ahead through the
	// Starting branch, post-fix it blocks on opMu.
	time.Sleep(50 * time.Millisecond)
	close(gate) // let the run hook start the process and Start finish

	<-startDone
	<-stopDone

	if proc.isRunning() {
		t.Fatal("process still running after Start and Stop both returned (orphan): Stop did not wait for the in-flight Start")
	}
	if bc.State() != ContainerStateStopped {
		t.Errorf("expected final state stopped, got %v", bc.State())
	}
}

// TestBaseContainer_StartDuringStopping_NoDoubleProcess pins findings #1/#3: a
// Start arriving while a Stop's stopFunc is still tearing down must not spin up
// a second process on top of the one being stopped. Before opMu, Start observed
// state=Stopping, took the Stopping->Starting branch, and ran its run hook while
// the old process was still live (double start); the late unconditional
// MarkStopped then overwrote the freshly-set Running. Both are logical races,
// invisible to -race, so the gate makes them deterministic.
func TestBaseContainer_StartDuringStopping_NoDoubleProcess(t *testing.T) {
	proc := &gatedProc{}
	gate := make(chan struct{})
	stopEntered := make(chan struct{})
	hooks := &mockHooks{
		runFunc: func() error {
			proc.start()
			return nil
		},
		stopFunc: func() error {
			close(stopEntered)
			<-gate
			proc.stop()
			return nil
		},
	}
	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	if err := bc.Start(); err != nil { // -> Running, proc running
		t.Fatalf("initial Start: %v", err)
	}

	stopDone := make(chan struct{})
	go func() { defer close(stopDone); _ = bc.Stop() }()
	<-stopEntered // stopFunc executing, blocked on gate, proc still running

	startDone := make(chan struct{})
	go func() { defer close(startDone); _ = bc.Start() }()

	// Let the second Start attempt progress: pre-fix it enters the
	// Stopping->Starting branch and runs its hook while proc is still running.
	time.Sleep(50 * time.Millisecond)
	close(gate) // stopFunc finishes tearing down

	<-stopDone
	<-startDone

	if proc.hadDoubleStart() {
		t.Fatal("run hook started a second process while the first was still being stopped (double start)")
	}
}
