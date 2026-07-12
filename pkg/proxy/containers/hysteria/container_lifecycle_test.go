package hysteria

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

// ---- test infrastructure -------------------------------------------------

// writeFakeHysteriaBinary writes a shell script that records its PID and
// sleeps, standing in for the real hysteria binary so lifecycle tests never
// touch the network. The recorded PID lets tests assert whether a process
// outlived Stop.
func writeFakeHysteriaBinary(t *testing.T, dir string) (binPath, pidFile string) {
	t.Helper()
	pidFile = filepath.Join(dir, "started.pid")
	binPath = filepath.Join(dir, "fake-hysteria")
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

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func waitPidAlive(t *testing.T, pidFile string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pid, ok := readPidFile(pidFile); ok && processAlive(pid) {
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("hysteria process did not start in time")
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

func newLifecycleTestConfig(dir, bin string) HysteriaConfig {
	return HysteriaConfig{
		BinaryPath:     bin,
		ConfigFilePath: filepath.Join(dir, "hysteria.yaml"),
		Port:           4443,
		Domain:         "test.local",
		Version:        "app/v0.0.0-test",
	}
}

// gateCertReader blocks GetCertFiles until the test releases it with a
// verdict, letting tests pin the "certificate becomes ready" moment
// precisely against a concurrent Stop/Start. After the first true verdict
// the reader stays open and answers immediately (matching a cert that,
// once issued, stays on disk).
type gateCertReader struct {
	entered chan struct{} // one signal per GetCertFiles entry
	gate    chan bool     // one verdict per blocked call; true = cert ready
	mu      sync.Mutex
	open    bool
	cert    string
	key     string
}

func newGateCertReader(dir string) *gateCertReader {
	return &gateCertReader{
		entered: make(chan struct{}, 16),
		gate:    make(chan bool),
		cert:    filepath.Join(dir, "cert.pem"),
		key:     filepath.Join(dir, "key.pem"),
	}
}

func (r *gateCertReader) GetCertFiles(string) (string, string, bool) {
	r.entered <- struct{}{}
	r.mu.Lock()
	open := r.open
	r.mu.Unlock()
	if open {
		return r.cert, r.key, true
	}
	ok := <-r.gate
	if ok {
		r.mu.Lock()
		r.open = true
		r.mu.Unlock()
	}
	return r.cert, r.key, ok
}

func waitEntered(t *testing.T, r *gateCertReader, what string) {
	t.Helper()
	select {
	case <-r.entered:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %s", what)
	}
}

func sendVerdict(t *testing.T, r *gateCertReader, ok bool) {
	t.Helper()
	select {
	case r.gate <- ok:
	case <-time.After(5 * time.Second):
		t.Fatal("no GetCertFiles call consumed the verdict")
	}
}

// ---- regression tests ----------------------------------------------------

// TestStopDuringCertWait_NoOrphanProcess pins the P0: the certificate
// becomes ready while (or right after) Stop runs. Before the fix, Stop did
// not wait for the cert-wait goroutine, which then happily called
// startProcess — leaving a live hysteria process behind a "stopped"
// container. The invariant is: after Stop has returned, no hysteria process
// may be left alive, no matter how the cert-ready wakeup interleaved.
func TestStopDuringCertWait_NoOrphanProcess(t *testing.T) {
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
	// The cert-wait goroutine is now blocked inside GetCertFiles.
	waitEntered(t, reader, "GetCertFiles entry")

	stopDone := make(chan error, 1)
	go func() { stopDone <- hc.Stop() }()

	// Let Stop progress as far as it can (after the fix it blocks waiting
	// for the cert-wait goroutine), then declare the certificate ready.
	time.Sleep(200 * time.Millisecond)
	sendVerdict(t, reader, true)

	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return within 5s")
	}

	// Watch for a late orphan: before the fix the process is started only
	// after Stop has already returned.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pid, ok := readPidFile(pidFile); ok && processAlive(pid) {
			t.Fatalf("orphan hysteria process alive after Stop returned (pid=%d)", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestStopStartCycle_CertWaitNoRace drives a full Stop→Start generation swap
// while the first generation's cert-wait goroutine is still alive. Before
// the fix, -race reports the unsynchronized certWaitStopCh field accesses
// across generations; after the fix each goroutine only holds its own
// session context by value, and the second generation must still bring the
// process up normally.
func TestStopStartCycle_CertWaitNoRace(t *testing.T) {
	dir := t.TempDir()
	bin, pidFile := writeFakeHysteriaBinary(t, dir)
	reader := newGateCertReader(dir)

	hc, err := NewHysteriaContainer(newLifecycleTestConfig(dir, bin), WithCertReader(reader))
	if err != nil {
		t.Fatalf("new container: %v", err)
	}

	// Generation 1: blocked waiting for the certificate.
	if err := hc.Start(); err != nil {
		t.Fatalf("start gen1: %v", err)
	}
	waitEntered(t, reader, "gen1 GetCertFiles entry")

	stopDone := make(chan error, 1)
	go func() { stopDone <- hc.Stop() }()
	time.Sleep(100 * time.Millisecond)
	// Release gen1 with "cert not ready": it must terminate via its stop
	// signal instead of starting anything.
	sendVerdict(t, reader, false)
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("stop gen1: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop gen1 did not return within 5s")
	}

	// Generation 2: strictly after Stop returned, start again and let the
	// certificate become ready this time.
	if err := hc.Start(); err != nil {
		t.Fatalf("start gen2: %v", err)
	}
	waitEntered(t, reader, "gen2 GetCertFiles entry")
	sendVerdict(t, reader, true)

	pid := waitPidAlive(t, pidFile, 5*time.Second)

	if err := hc.Stop(); err != nil {
		t.Fatalf("stop gen2: %v", err)
	}
	waitProcessDead(t, pid, 5*time.Second)
}

// TestUpdatePath_UserEventsSurviveRestart covers the Stop→Start skeleton of
// the Update() path. Before the fix, Stop closed userEventCh and set it to
// nil, so after a restart the container had no event handler and no channel
// — user event handling was permanently dead.
func TestUpdatePath_UserEventsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	bin, pidFile := writeFakeHysteriaBinary(t, dir)

	cfg := newLifecycleTestConfig(dir, bin)
	// Direct cert path: waitForCertAndStart starts the process immediately.
	cfg.CertFile = filepath.Join(dir, "cert.pem")
	cfg.KeyFile = filepath.Join(dir, "key.pem")

	hc, err := NewHysteriaContainer(cfg)
	if err != nil {
		t.Fatalf("new container: %v", err)
	}
	// The constructor only creates userEventCh when a UserManager is
	// injected; wire the channel directly (before any Start, mirroring the
	// constructor) to keep the test free of a full UserManager.
	// handleUserEvent is a no-op with a nil userMgr, so consumption is
	// observable purely via the channel draining.
	hc.userEventCh = make(chan usermanager.UserEvent, 100)

	if err := hc.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid1 := waitPidAlive(t, pidFile, 5*time.Second)
	if err := hc.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitProcessDead(t, pid1, 5*time.Second)

	// Restart — the skeleton of the Update() path.
	if err := hc.Start(); err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer func() { _ = hc.Stop() }()

	if hc.UserEventChannel() == nil {
		t.Fatal("user event channel is nil after restart: user event handling is permanently dead")
	}
	hc.userEventCh <- usermanager.UserEvent{Type: usermanager.UserEventAdd, Username: "u1"}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(hc.userEventCh) == 0 {
			return // handler consumed the event — it survived the restart
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("event not consumed after restart: handler goroutine not running")
}

// TestConcurrentStopWithEventForward_NoPanic hammers Start/Stop cycles while
// a producer goroutine keeps offering events exactly the way
// forwardUserEvents does (nil check + non-blocking send). Before the fix,
// the producer's reads of hc.userEventCh race with the close/nil writes in
// closeUserEventCh (-race reports; a send-on-closed panic is possible).
// After the fix the field is immutable and never closed.
func TestConcurrentStopWithEventForward_NoPanic(t *testing.T) {
	dir := t.TempDir()
	bin, _ := writeFakeHysteriaBinary(t, dir)

	cfg := newLifecycleTestConfig(dir, bin)
	cfg.CertFile = filepath.Join(dir, "cert.pem")
	cfg.KeyFile = filepath.Join(dir, "key.pem")

	hc, err := NewHysteriaContainer(cfg)
	if err != nil {
		t.Fatalf("new container: %v", err)
	}
	hc.userEventCh = make(chan usermanager.UserEvent, 100)

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
			// Mirror forwardUserEvents' send pattern.
			if hc.userEventCh != nil {
				select {
				case hc.userEventCh <- usermanager.UserEvent{
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
		if err := hc.Start(); err != nil {
			t.Fatalf("start #%d: %v", i, err)
		}
		if err := hc.Stop(); err != nil {
			t.Fatalf("stop #%d: %v", i, err)
		}
	}

	close(stopProducer)
	producerWg.Wait()
}
