package container

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// mockHooks implements Hooks for testing
type mockHooks struct {
	runFunc  func() error
	stopFunc func() error
}

func (h *mockHooks) GetRunFunc() (func() error, func() error) {
	return h.runFunc, h.stopFunc
}

func TestBaseContainer_Type(t *testing.T) {
	hooks := &mockHooks{runFunc: func() error { return nil }}
	bc := NewBaseContainer(contracts.ContainerXray, hooks)
	if bc.Type() != contracts.ContainerXray {
		t.Errorf("expected type %v, got %v", contracts.ContainerXray, bc.Type())
	}
}

func TestBaseContainer_NewBaseContainer_NilHooks_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when hooks is nil")
		}
	}()
	NewBaseContainer(contracts.ContainerXray, nil)
}

func TestBaseContainer_IsRunning_InitialState(t *testing.T) {
	hooks := &mockHooks{runFunc: func() error { return nil }}
	bc := NewBaseContainer(contracts.ContainerXray, hooks)
	if bc.IsRunning() {
		t.Error("expected initial state to be not running")
	}
}

func TestBaseContainer_Start_WithHooks_Success(t *testing.T) {
	executed := false
	stopped := false
	hooks := &mockHooks{
		runFunc: func() error {
			executed = true
			return nil
		},
		stopFunc: func() error {
			stopped = true
			return nil
		},
	}

	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	if err := bc.Start(); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	if !executed {
		t.Error("expected runFunc to be executed")
	}

	if !bc.IsRunning() {
		t.Error("expected IsRunning() to be true after successful Start()")
	}

	// Test stop
	if err := bc.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}

	if !stopped {
		t.Error("expected stopFunc to be executed")
	}

	if bc.IsRunning() {
		t.Error("expected IsRunning() to be false after Stop()")
	}
}

func TestBaseContainer_Start_WithHooks_Failure(t *testing.T) {
	hooks := &mockHooks{
		runFunc: func() error {
			return errors.New("start failed")
		},
	}

	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	err := bc.Start()
	if err == nil {
		t.Fatal("expected Start() to return error when runFunc fails")
	}

	if bc.IsRunning() {
		t.Error("expected IsRunning() to be false after failed Start()")
	}

	if bc.State() != ContainerStateStopped {
		t.Errorf("expected state %v after failed Start(), got %v", ContainerStateStopped, bc.State())
	}
}

func TestBaseContainer_Stop_WithHooks_Failure(t *testing.T) {
	hooks := &mockHooks{
		runFunc: func() error {
			return nil
		},
		stopFunc: func() error {
			return errors.New("stop failed")
		},
	}

	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	// Start
	if err := bc.Start(); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	// Stop should return error but still mark as stopped
	err := bc.Stop()
	if err == nil {
		t.Fatal("expected Stop() to return error when stopFunc fails")
	}

	// Should be stopped despite error
	if bc.IsRunning() {
		t.Error("expected IsRunning() to be false after Stop() even with error")
	}
}

func TestBaseContainer_GetRunFunc(t *testing.T) {
	runFunc := func() error { return nil }
	stopFunc := func() error { return nil }
	hooks := &mockHooks{runFunc: runFunc, stopFunc: stopFunc}

	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	run, stop := bc.GetRunFunc()
	if run == nil {
		t.Error("expected non-nil run function")
	}
	if stop == nil {
		t.Error("expected non-nil stop function")
	}
}

func TestBaseContainer_Stop_AfterRunning(t *testing.T) {
	hooks := &mockHooks{runFunc: func() error { return nil }}
	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	// Start (marks as running since no hooks)
	bc.Start()

	// Stop should transition to stopping state
	if err := bc.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}

	// State should be stopped
	if bc.State() != ContainerStateStopped {
		t.Errorf("expected state %v, got %v", ContainerStateStopped, bc.State())
	}

	if bc.IsRunning() {
		t.Error("expected IsRunning() to be false after Stop()")
	}
}

func TestBaseContainer_Start_Idempotent(t *testing.T) {
	hooks := &mockHooks{runFunc: func() error { return nil }}
	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	// Start twice
	bc.Start()

	err := bc.Start()
	if err != nil {
		t.Errorf("idempotent Start() should not return error: %v", err)
	}

	if !bc.IsRunning() {
		t.Error("expected still running after idempotent Start()")
	}
}

func TestBaseContainer_Stop_Idempotent(t *testing.T) {
	hooks := &mockHooks{runFunc: func() error { return nil }}
	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	// Start and stop
	bc.Start()
	bc.Stop()

	// Stop again - should be idempotent
	err := bc.Stop()
	if err != nil {
		t.Errorf("idempotent Stop() should not return error: %v", err)
	}

	if bc.IsRunning() {
		t.Error("expected not running after idempotent Stop()")
	}
}

func TestBaseContainer_Stop_AlreadyStopped(t *testing.T) {
	hooks := &mockHooks{runFunc: func() error { return nil }}
	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	// Stop when already stopped - should be idempotent
	err := bc.Stop()
	if err != nil {
		t.Errorf("idempotent Stop() on stopped container should not return error: %v", err)
	}
}

func TestBaseContainer_Concurrent_StartStop(t *testing.T) {
	hooks := &mockHooks{runFunc: func() error { return nil }}
	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	// Concurrent start/stop operations should be safe
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bc.Start()
			time.Sleep(1 * time.Millisecond)
			bc.Stop()
		}()
	}
	wg.Wait()

	// Should end up in stopped state
	if bc.State() != ContainerStateStopped {
		t.Errorf("expected final state %v, got %v", ContainerStateStopped, bc.State())
	}
}

func TestBaseContainer_Restart(t *testing.T) {
	hooks := &mockHooks{runFunc: func() error { return nil }}
	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	// Start, then restart
	bc.Start()

	err := bc.Restart()
	if err != nil {
		t.Errorf("Restart() returned error: %v", err)
	}

	// Should be running after restart
	if !bc.IsRunning() {
		t.Error("expected IsRunning() to be true after Restart()")
	}
}

func TestBaseContainer_StopChan(t *testing.T) {
	hooks := &mockHooks{runFunc: func() error { return nil }}
	bc := NewBaseContainer(contracts.ContainerXray, hooks)

	// Start first, then get the channel
	bc.Start()

	stopCh := bc.StopChan()
	if stopCh == nil {
		t.Error("StopChan() should not return nil")
	}

	// After stop, channel should be closed
	bc.Stop()

	select {
	case <-stopCh:
		// Expected - channel closed
	default:
		t.Error("expected StopChan to be closed after Stop()")
	}
}

func TestBaseContainer_Start_WithNilRunFunc_Panic(t *testing.T) {
	// This tests that GetRunFunc returning nil run causes panic
	hooks := &mockHooks{
		runFunc:  nil, // nil runFunc should cause panic
		stopFunc: func() error { return nil },
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when runFunc is nil")
		}
	}()

	bc := NewBaseContainer(contracts.ContainerXray, hooks)
	bc.Start() // Should panic
}
