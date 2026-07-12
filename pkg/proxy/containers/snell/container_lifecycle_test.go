package snell

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// ---- test infrastructure (mirrors hysteria/container_lifecycle_test.go) ----

func writeFakeSnellBinary(t *testing.T, dir string) (binPath, pidFile string) {
	t.Helper()
	pidFile = filepath.Join(dir, "started.pid")
	binPath = filepath.Join(dir, "fake-snell")
	script := "#!/bin/sh\necho $$ > " + pidFile + "\nexec sleep 60\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	t.Cleanup(func() {
		if pid, ok := readPidFile(pidFile); ok {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return binPath, pidFile
}

func readPidFile(pidFile string) (int, bool) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func waitPidAlive(t *testing.T, pidFile string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pid, ok := readPidFile(pidFile); ok && processAlive(pid) {
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("snell process did not start in time")
	return 0
}

func waitProcessDead(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d still alive", pid)
}

func newLifecycleTestConfig(dir, bin string) SnellConfig {
	return SnellConfig{
		BinaryPath:     bin,
		ConfigFilePath: filepath.Join(dir, "snell.conf"),
		Port:           16160,
		PSK:            "testpsk",
		Version:        "v0.0.0-test",
	}
}

// ---- regression tests ----

// TestConcurrentStopWithEventForward_NoPanic hammers Start/Stop cycles while a
// producer goroutine offers events exactly the way forwardUserEvents does (nil
// check + non-blocking send). Before the fix, the producer's reads of
// sc.userEventCh race with the close/nil writes in the old closeUserEventCh
// (-race reports; a send-on-closed panic is possible). After the fix the field
// is immutable and never closed.
func TestConcurrentStopWithEventForward_NoPanic(t *testing.T) {
	dir := t.TempDir()
	bin, _ := writeFakeSnellBinary(t, dir)
	sc, err := NewSnellContainer(newLifecycleTestConfig(dir, bin))
	if err != nil {
		t.Fatalf("new container: %v", err)
	}
	// Constructor only creates userEventCh when a UserManager is injected; wire
	// it directly (before any Start, as the constructor would). handleUserEvent
	// is a no-op with a nil userMgr, so consumption is observable via draining.
	sc.userEventCh = make(chan usermanager.UserEvent, 100)

	stopProducer := make(chan struct{})
	var producerWg sync.WaitGroup
	producerWg.Add(1)
	go func() {
		defer producerWg.Done()
		for i := 0; ; i++ {
			select {
			case <-stopProducer:
				return
			default:
			}
			if sc.userEventCh != nil { // mirror forwardUserEvents' send pattern
				select {
				case sc.userEventCh <- usermanager.UserEvent{
					Type:     usermanager.UserEventAdd,
					Username: fmt.Sprintf("u%d", i),
				}:
				default:
				}
			}
			time.Sleep(100 * time.Microsecond)
		}
	}()

	for i := 0; i < 30; i++ {
		if err := sc.Start(); err != nil {
			t.Fatalf("start #%d: %v", i, err)
		}
		if err := sc.Stop(); err != nil {
			t.Fatalf("stop #%d: %v", i, err)
		}
	}

	close(stopProducer)
	producerWg.Wait()
}

// TestUpdatePath_UserEventsSurviveRestart covers the Stop→Start skeleton of the
// Update() path. Before the fix, Stop closed userEventCh and set it nil, so
// after a restart the container had no event handler and no channel — user
// event handling was permanently dead.
func TestUpdatePath_UserEventsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	bin, pidFile := writeFakeSnellBinary(t, dir)
	sc, err := NewSnellContainer(newLifecycleTestConfig(dir, bin))
	if err != nil {
		t.Fatalf("new container: %v", err)
	}
	sc.userEventCh = make(chan usermanager.UserEvent, 100)

	if err := sc.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid1 := waitPidAlive(t, pidFile, 5*time.Second)
	if err := sc.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitProcessDead(t, pid1, 5*time.Second)

	// Restart — the skeleton of the Update() path.
	if err := sc.Start(); err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer func() { _ = sc.Stop() }()

	if sc.UserEventChannel() == nil {
		t.Fatal("user event channel is nil after restart: user event handling is permanently dead")
	}
	sc.userEventCh <- usermanager.UserEvent{Type: usermanager.UserEventAdd, Username: "u1"}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(sc.userEventCh) == 0 {
			return // handler consumed the event — it survived the restart
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("event not consumed after restart: handler goroutine not running")
}

// TestStopStartCycle_NoOrphanProcess verifies the process is cleanly stopped and
// a fresh one comes up across a Stop→Start cycle, with no orphan left behind.
func TestStopStartCycle_NoOrphanProcess(t *testing.T) {
	dir := t.TempDir()
	bin, pidFile := writeFakeSnellBinary(t, dir)
	sc, err := NewSnellContainer(newLifecycleTestConfig(dir, bin))
	if err != nil {
		t.Fatalf("new container: %v", err)
	}

	if err := sc.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid1 := waitPidAlive(t, pidFile, 5*time.Second)
	if err := sc.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitProcessDead(t, pid1, 5*time.Second)

	if err := sc.Start(); err != nil {
		t.Fatalf("restart: %v", err)
	}
	pid2 := waitPidAlive(t, pidFile, 5*time.Second)
	if err := sc.Stop(); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	waitProcessDead(t, pid2, 5*time.Second)
}
