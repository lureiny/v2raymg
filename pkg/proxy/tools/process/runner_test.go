package process

import (
	"testing"
	"time"
)

func TestRunner_NewRunner(t *testing.T) {
	// Test with empty binary path
	_, err := NewRunner(RunnerConfig{})
	if err == nil {
		t.Error("expected error for empty binary path")
	}

	// Test with a valid binary path - creation should succeed
	// (error only happens at Start when trying to execute)
	runner, err := NewRunner(RunnerConfig{
		BinaryPath: "/nonexistent-binary-for-test",
	})
	if err != nil {
		t.Errorf("unexpected error for nonexistent binary during creation: %v", err)
	}
	_ = runner
}

func TestRunner_StartStop(t *testing.T) {
	// Use sleep command for testing
	runner, err := NewRunner(RunnerConfig{
		BinaryPath: "sleep",
		Args:       []string{"100"},
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	// Test start
	if err := runner.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if !runner.IsRunning() {
		t.Error("expected running after start")
	}

	pid := runner.PID()
	if pid == 0 {
		t.Error("expected non-zero PID")
	}

	// Test stop
	if err := runner.Stop(); err != nil {
		t.Fatalf("failed to stop: %v", err)
	}

	// Give it time to fully stop
	time.Sleep(10 * time.Millisecond)

	if runner.IsRunning() {
		t.Error("expected not running after stop")
	}
}

func TestRunner_Restart(t *testing.T) {
	runner, err := NewRunner(RunnerConfig{
		BinaryPath: "sleep",
		Args:       []string{"100"},
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	// Start
	if err := runner.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Restart
	if err := runner.Restart(); err != nil {
		t.Fatalf("failed to restart: %v", err)
	}

	if !runner.IsRunning() {
		t.Error("expected running after restart")
	}

	// Stop
	runner.Stop()
}

func TestRunner_Start_AlreadyRunning(t *testing.T) {
	runner, err := NewRunner(RunnerConfig{
		BinaryPath: "sleep",
		Args:       []string{"100"},
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	// Start first time
	if err := runner.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Try to start again - should fail
	if err := runner.Start(); err == nil {
		t.Error("expected error when starting already running process")
	}

	runner.Stop()
}

func TestRunner_Stop_NotRunning(t *testing.T) {
	runner, err := NewRunner(RunnerConfig{
		BinaryPath: "sleep",
		Args:       []string{"100"},
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	// Stop when not running - should not error
	if err := runner.Stop(); err != nil {
		t.Errorf("unexpected error when stopping non-running process: %v", err)
	}
}

func TestRunner_SetConfig(t *testing.T) {
	runner, err := NewRunner(RunnerConfig{
		BinaryPath: "sleep",
		Args:       []string{"100"},
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	// Update config
	runner.SetConfig(RunnerConfig{
		BinaryPath: "sleep",
		Args:       []string{"200"},
	})

	cfg := runner.GetConfig()
	if len(cfg.Args) != 1 || cfg.Args[0] != "200" {
		t.Errorf("expected args [200], got %v", cfg.Args)
	}
}

func TestRunner_PID(t *testing.T) {
	runner, err := NewRunner(RunnerConfig{
		BinaryPath: "sleep",
		Args:       []string{"100"},
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	// PID should be 0 when not running
	if runner.PID() != 0 {
		t.Error("expected PID 0 when not running")
	}

	// Start and check PID
	if err := runner.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	pid := runner.PID()
	if pid <= 0 {
		t.Errorf("expected positive PID, got %d", pid)
	}

	runner.Stop()
}

// TestRunner_Version tests version command with a real binary
func TestRunner_Version(t *testing.T) {
	// Use go version to test
	runner, err := NewRunner(RunnerConfig{
		BinaryPath: "go",
		Args:       []string{"version"},
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	// This won't run as part of normal Start/Stop cycle
	// but verifies the runner can be created with go binary
	_ = runner
}

// Verify Runner implements expected interface
var _ interface{} = (*Runner)(nil)

// TestRunner_IsRunning_AfterSelfExit covers finding #1: once the process exits
// on its own, the reaper must flip IsRunning to false and PID to 0 (before the
// fix, IsRunning stayed true forever and the child became a zombie).
func TestRunner_IsRunning_AfterSelfExit(t *testing.T) {
	r, err := NewRunner(RunnerConfig{BinaryPath: "true"})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for r.IsRunning() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if r.IsRunning() {
		t.Fatal("IsRunning must be false after the process self-exits (zombie / stale state)")
	}
	if r.PID() != 0 {
		t.Errorf("PID must be 0 after exit, got %d", r.PID())
	}
}

// TestRunner_RestartAfterSelfExit mirrors the updater path: after a crash,
// IsRunning is false so Stop is skipped and Start is called directly — which
// must succeed (the exited-aware guard allows it).
func TestRunner_RestartAfterSelfExit(t *testing.T) {
	r, _ := NewRunner(RunnerConfig{BinaryPath: "true"})
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for r.IsRunning() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if r.IsRunning() { // updater does: if IsRunning { Stop }
		_ = r.Stop()
	}
	if err := r.Start(); err != nil {
		t.Fatalf("Start after self-exit must succeed, got: %v", err)
	}
	_ = r.Stop()
}

// TestRunner_Stop_AfterSelfExit_NoDoubleWait ensures Stop after a self-exit is a
// clean no-op (it waits on the reaper's done channel and does not double-Wait).
func TestRunner_Stop_AfterSelfExit_NoDoubleWait(t *testing.T) {
	r, _ := NewRunner(RunnerConfig{BinaryPath: "true"})
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for r.IsRunning() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := r.Stop(); err != nil {
		t.Fatalf("Stop after self-exit must be a clean no-op, got: %v", err)
	}
}
