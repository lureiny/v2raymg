package ping

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// TestBasePingChecker_StopSetsNotRunning covers finding #4: Stop must set
// isRunning to false (it wrongly stored true, making SetPingCheck a one-way
// switch and IsRunning permanently true).
func TestBasePingChecker_StopSetsNotRunning(t *testing.T) {
	bpc := newBasePingChecker()
	bpc.isRunning.Store(true)

	bpc.Stop()

	if bpc.IsRunning() {
		t.Fatal("IsRunning must be false after Stop")
	}
	if bpc.ctx.Err() == nil {
		t.Fatal("context must be cancelled after Stop")
	}
}

// TestIsTimedOut covers finding #1: the loss/timeout comparison must honor the
// configured timeout rather than the package-global pingTimeout.
func TestIsTimedOut(t *testing.T) {
	now := time.Now()
	if !isTimedOut(now.Add(-2*time.Second), now, time.Second) {
		t.Error("a 2s-old packet must be timed out at a 1s timeout")
	}
	if isTimedOut(now.Add(-500*time.Millisecond), now, time.Second) {
		t.Error("a 500ms-old packet must NOT be timed out at a 1s timeout")
	}
	// A larger configured timeout must keep a 2s-old packet alive.
	if isTimedOut(now.Add(-2*time.Second), now, 3*time.Second) {
		t.Error("a 2s-old packet must NOT be timed out at a 3s timeout")
	}
}

// TestNewPingerInfo_StoresConfiguredDurations verifies the configured
// interval/timeout are carried into pingerInfo (they were stored but the loop
// ignored them; this guards the plumbing the loop now reads).
func TestNewPingerInfo_StoresConfiguredDurations(t *testing.T) {
	pi := newPingerInfo(&PingNodeInfo{Host: "1.2.3.4"}, "icmp_ping", 10*time.Second, 3*time.Second)
	if pi.interval != 10*time.Second || pi.timeout != 3*time.Second {
		t.Fatalf("expected interval=10s timeout=3s, got interval=%v timeout=%v", pi.interval, pi.timeout)
	}
}

// TestRunPinger_CapturesError covers finding #3: a pinger Run failure (e.g. no
// CAP_NET_RAW) must be surfaced (logged + marked failed), not swallowed. The run
// seam lets us test this without opening a raw ICMP socket.
func TestRunPinger_CapturesError(t *testing.T) {
	pi := &pingerInfo{
		nodeInfo: &PingNodeInfo{Host: "1.2.3.4", Geo: "cn", ISP: "ct"},
		run:      func() error { return errors.New("operation not permitted") },
	}
	pi.runPinger()
	if !pi.failed.Load() {
		t.Fatal("a Run() error must mark the pinger failed (not be silently swallowed)")
	}
}

func TestRunPinger_NoErrorNotFailed(t *testing.T) {
	pi := &pingerInfo{
		nodeInfo: &PingNodeInfo{Host: "1.2.3.4"},
		run:      func() error { return nil },
	}
	pi.runPinger()
	if pi.failed.Load() {
		t.Fatal("a clean Run() must not mark the pinger failed")
	}
}

func TestNewPinger_DisablesHistoryRecording(t *testing.T) {
	p, err := newPinger("127.0.0.1")
	if err != nil {
		t.Fatalf("newPinger failed: %v", err)
	}
	if p.RecordRtts {
		t.Fatal("newPinger must disable RecordRtts to prevent unbounded slice growth")
	}
	if p.RecordTTLs {
		t.Fatal("newPinger must disable RecordTTLs to prevent unbounded slice growth")
	}
}

func TestConfigurePinger_BoundsCycleAndTimeout(t *testing.T) {
	p, err := newPinger("127.0.0.1")
	if err != nil {
		t.Fatalf("newPinger failed: %v", err)
	}
	p.RecordRtts = true
	p.RecordTTLs = true

	pi := newPingerInfo(&PingNodeInfo{Geo: "us", ISP: "att", Host: "127.0.0.1"}, "icmp_ping", 2*time.Second, 500*time.Millisecond)
	pi.maxPingsPerCycle = 10
	pi.ctx = context.Background()

	sendCh := make(chan *PingPacketInfo, 1)
	recvCh := make(chan *PingPacketInfo, 1)
	pi.configurePinger(p, sendCh, recvCh)

	if p.RecordRtts || p.RecordTTLs {
		t.Fatal("configurePinger must force RecordRtts=false and RecordTTLs=false")
	}
	if p.Count != 10 {
		t.Fatalf("expected Count=10, got %d", p.Count)
	}
	if p.Interval != 2*time.Second {
		t.Fatalf("expected Interval=2s, got %v", p.Interval)
	}
	wantTimeout := 10*2*time.Second + 500*time.Millisecond + time.Second
	if p.Timeout != wantTimeout {
		t.Fatalf("expected Timeout=%v, got %v", wantTimeout, p.Timeout)
	}
}

func TestPingerInfo_StopDoesNotDeadlockWhenCallbacksFire(t *testing.T) {
	ipc := NewIcmpPingChecker()
	defer ipc.Stop()

	// Fill resultChan completely so pingLoop hits send backpressure.
	for len(ipc.resultChan) < cap(ipc.resultChan) {
		ipc.resultChan <- NewPingResult("us", "att", "127.0.0.1", ipc.Name())
	}

	var curPinger *probing.Pinger
	started := make(chan struct{})

	pi := newPingerInfo(&PingNodeInfo{Geo: "us", ISP: "att", Host: "127.0.0.1"}, ipc.Name(), time.Millisecond, 50*time.Millisecond)
	pi.newPingerFn = func(host string) (*probing.Pinger, error) {
		p, err := newPinger(host)
		if err == nil {
			curPinger = p
		}
		return p, err
	}
	pi.run = func() error {
		close(started)
		for i := 0; ; i++ {
			select {
			case <-pi.ctx.Done():
				return nil
			default:
			}
			if curPinger.OnSend != nil {
				curPinger.OnSend(&probing.Packet{Seq: i})
			}
			if curPinger.OnRecv != nil {
				curPinger.OnRecv(&probing.Packet{Seq: i, Rtt: time.Millisecond})
			}
		}
	}

	if err := pi.start(ipc); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for mock pinger run to start")
	}

	// Give callbacks a brief moment to saturate sendCh/recvCh and resultChan.
	time.Sleep(20 * time.Millisecond)

	stopped := make(chan struct{})
	go func() {
		pi.stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("pi.stop deadlocked while callbacks and resultChan were backpressured")
	}
}

func TestPingerInfo_RotatesPingerAcrossCycles(t *testing.T) {
	ipc := NewIcmpPingChecker()
	defer ipc.Stop()

	var createCount atomic.Int32
	var runCount atomic.Int32
	rotated := make(chan struct{})

	pi := newPingerInfo(&PingNodeInfo{Geo: "jp", ISP: "ntt", Host: "127.0.0.1"}, ipc.Name(), time.Millisecond, 50*time.Millisecond)
	pi.maxPingsPerCycle = 2
	pi.newPingerFn = func(host string) (*probing.Pinger, error) {
		createCount.Add(1)
		return newPinger(host)
	}
	pi.run = func() error {
		if runCount.Add(1) >= 2 {
			select {
			case <-rotated:
			default:
				close(rotated)
			}
			<-pi.ctx.Done()
		}
		return nil
	}

	if err := pi.start(ipc); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	select {
	case <-rotated:
	case <-time.After(2 * time.Second):
		pi.stop()
		t.Fatalf("expected pinger rotation across cycles, got create=%d run=%d", createCount.Load(), runCount.Load())
	}

	pi.stop()
	if got := createCount.Load(); got < 2 {
		t.Fatalf("expected at least 2 pinger creations across cycles, got %d", got)
	}
}

func TestPingerInfo_FailedRunDoesNotRetryOrLeak(t *testing.T) {
	ipc := NewIcmpPingChecker()
	defer ipc.Stop()

	var createCount atomic.Int32
	var runCount atomic.Int32
	done := make(chan struct{})

	pi := newPingerInfo(&PingNodeInfo{Geo: "us", ISP: "cogent", Host: "127.0.0.1"}, ipc.Name(), time.Millisecond, 50*time.Millisecond)
	pi.newPingerFn = func(host string) (*probing.Pinger, error) {
		createCount.Add(1)
		return newPinger(host)
	}
	pi.run = func() error {
		runCount.Add(1)
		close(done)
		return errors.New("operation not permitted")
	}

	if err := pi.start(ipc); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for failed run")
	}

	pi.wg.Wait()
	if !pi.failed.Load() {
		t.Fatal("expected pingerInfo.failed to be true")
	}
	if runCount.Load() != 1 || createCount.Load() != 1 {
		t.Fatalf("expected single failed attempt without retry, got run=%d create=%d", runCount.Load(), createCount.Load())
	}
	pi.stop()
}

func TestTcpPingerInfo_StopDoesNotDeadlockOnFullResultChan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pi := &tcpPingerInfo{
		nodeInfo: &PingNodeInfo{Geo: "us", ISP: "att", Host: "127.0.0.1", Port: 1},
		interval: time.Millisecond,
		ctx:      ctx,
	}

	// Unbuffered channel with no receiver forces immediate send backpressure.
	ch := make(chan *PingResult)
	pi.wg.Add(1)
	go pi.pingLoop(ch)

	time.Sleep(20 * time.Millisecond)
	cancel()

	stopped := make(chan struct{})
	go func() {
		pi.wg.Wait()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("tcpPingerInfo.pingLoop deadlocked on full result channel during cancel")
	}
}
