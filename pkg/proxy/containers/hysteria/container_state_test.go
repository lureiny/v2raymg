package hysteria

import (
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
)

// TestState_NotRunningUntilProcessUp verifies IsRunning()/State() reflect the
// real subprocess, not merely that Start() returned. During the certificate
// wait BaseContainer is already Running, but the hysteria process has not been
// started; before the fix IsRunning() lied (true) and State() reported Running,
// misleading health checks and failover. After the fix they report not-running
// / Starting until the process is actually up, Running once it is, and Stopped
// after Stop.
func TestState_NotRunningUntilProcessUp(t *testing.T) {
	dir := t.TempDir()
	bin, pidFile := writeFakeHysteriaBinary(t, dir)
	reader := newGateCertReader(dir)

	hc, err := NewHysteriaContainer(newLifecycleTestConfig(dir, bin), WithCertReader(reader))
	if err != nil {
		t.Fatalf("new container: %v", err)
	}
	if err := hc.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The cert-wait goroutine is now blocked in GetCertFiles: Start() has
	// returned and BaseContainer is Running, but the process has NOT started.
	waitEntered(t, reader, "GetCertFiles entry")
	if got := hc.BaseContainer.State(); got != container.ContainerStateRunning {
		t.Fatalf("precondition: BaseContainer should be Running after Start, got %v", got)
	}
	if hc.IsRunning() {
		t.Error("IsRunning() must be false while the process is still waiting for its certificate")
	}
	if got := hc.State(); got != container.ContainerStateStarting {
		t.Errorf("State() during cert wait = %v, want Starting", got)
	}

	// Certificate becomes ready -> startProcess runs -> the process comes up.
	sendVerdict(t, reader, true)
	waitPidAlive(t, pidFile, 5*time.Second)

	deadline := time.Now().Add(5 * time.Second)
	for !hc.IsRunning() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !hc.IsRunning() {
		t.Fatal("IsRunning() should be true once the process is up")
	}
	if got := hc.State(); got != container.ContainerStateRunning {
		t.Errorf("State() with the process up = %v, want Running", got)
	}

	if err := hc.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if hc.IsRunning() {
		t.Error("IsRunning() must be false after Stop")
	}
	if got := hc.State(); got != container.ContainerStateStopped {
		t.Errorf("State() after Stop = %v, want Stopped", got)
	}
}
