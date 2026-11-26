package ping

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	ping "github.com/prometheus-community/pro-bing"
)

const defaultMaxPingsPerCycle = 1000

// isTimedOut reports whether a ping packet sent at sendTime is overdue by now,
// given the configured timeout. Extracted so the timeout comparison (which used
// to hardcode the package-global pingTimeout) is unit-testable.
func isTimedOut(sendTime, now time.Time, timeout time.Duration) bool {
	return now.UnixMicro()-sendTime.UnixMicro() > timeout.Microseconds()
}

type getNodeFunc func() []*PingNodeInfo

var (
	pingerIndex  atomic.Int32
	pingInterval = 5 // s
	pingTimeout  = time.Second
)

// newPinger host must be ip
func newPinger(host string) (*ping.Pinger, error) {
	pinger, err := ping.NewPinger(host)
	if err != nil {
		return nil, err
	}
	pinger.SetID(int(pingerIndex.Add(1) & 0xffff))
	pinger.SetPrivileged(true)
	// Do not retain unbounded per-packet RTT/TTL slices over the process lifetime.
	pinger.RecordRtts = false
	pinger.RecordTTLs = false
	return pinger, nil
}

type pingerInfo struct {
	nodeInfo         *PingNodeInfo
	pingChecker      string
	interval         time.Duration
	timeout          time.Duration
	maxPingsPerCycle int
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	// newPingerFn creates the underlying pro-bing Pinger for a cycle; tests may
	// override it to inspect options or inject preconfigured Pingers.
	newPingerFn func(host string) (*ping.Pinger, error)
	// run is the pinger's blocking run loop; a seam so tests can inject a
	// failure without opening a raw ICMP socket. Defaults to pinger.Run.
	run func() error
	// failed records that the pinger's run loop exited with an error (e.g. no
	// CAP_NET_RAW), which previously was swallowed silently.
	failed atomic.Bool
}

func (pi *pingerInfo) effectiveInterval() time.Duration {
	if pi.interval > 0 {
		return pi.interval
	}
	return time.Second * time.Duration(pingInterval)
}

func (pi *pingerInfo) effectiveTimeout() time.Duration {
	if pi.timeout > 0 {
		return pi.timeout
	}
	return pingTimeout
}

func (pi *pingerInfo) effectiveMaxPings() int {
	if pi.maxPingsPerCycle > 0 {
		return pi.maxPingsPerCycle
	}
	return defaultMaxPingsPerCycle
}

func (pi *pingerInfo) createPinger(host string) (*ping.Pinger, error) {
	if pi.newPingerFn != nil {
		return pi.newPingerFn(host)
	}
	return newPinger(host)
}

func pingerIPString(p *ping.Pinger, fallback string) string {
	if p != nil {
		if ipAddr := p.IPAddr(); ipAddr != nil {
			return ipAddr.String()
		}
	}
	return fallback
}

func (pi *pingerInfo) runWith(fn func() error) {
	if fn == nil {
		return
	}
	if err := fn(); err != nil {
		log.Error("icmp pinger run exited",
			"geo", pi.nodeInfo.Geo, "isp", pi.nodeInfo.ISP,
			"host", pi.nodeInfo.Host, "err", err)
		pi.failed.Store(true)
	}
}

// runPinger runs the pinger and surfaces (rather than swallows) a run failure
// such as a missing CAP_NET_RAW capability, which otherwise left the node
// probing silently with zero results. It logs once and marks the pinger failed;
// it deliberately does NOT auto-retry (a permission error would then log-storm
// once per reload tick).
func (pi *pingerInfo) runPinger() {
	pi.runWith(pi.run)
}

func newPingerInfo(nodeInfo *PingNodeInfo, pingChecker string, interval, timeout time.Duration) *pingerInfo {
	return &pingerInfo{
		nodeInfo:    nodeInfo,
		pingChecker: pingChecker,
		interval:    interval,
		timeout:     timeout,
	}
}

func (pi *pingerInfo) configurePinger(p *ping.Pinger, sendCh, recvCh chan<- *PingPacketInfo) {
	if p == nil {
		return
	}
	interval := pi.effectiveInterval()
	timeout := pi.effectiveTimeout()
	maxPings := pi.effectiveMaxPings()

	// Use the configured interval/timeout (carried in pi.interval/pi.timeout)
	// instead of the package-global constants, which made ICMPPingInterval /
	// ICMPPingTimeout config no-ops.
	p.Interval = interval
	// Cap Count and Timeout per cycle so pro-bing periodically rotates and
	// drops accumulated awaitingSequences/trackerUUIDs state for lost packets.
	p.Count = maxPings
	p.Timeout = time.Duration(maxPings)*interval + timeout + time.Second
	p.RecordRtts = false
	p.RecordTTLs = false

	ctx := pi.ctx
	p.OnSend = func(pkt *ping.Packet) {
		info := &PingPacketInfo{
			SendTime: time.Now(),
			Seq:      pkt.Seq,
		}
		select {
		case sendCh <- info:
		case <-ctx.Done():
		}
	}

	ipStr := pingerIPString(p, pi.nodeInfo.Host)
	p.OnFinish = func(s *ping.Statistics) {
		log.Debug("ping finish", "geo", pi.nodeInfo.Geo, "isp", pi.nodeInfo.ISP,
			"host", pi.nodeInfo.Host, "ip", ipStr)
	}

	p.OnRecv = func(pkt *ping.Packet) {
		info := &PingPacketInfo{
			Seq: pkt.Seq,
			RTT: pkt.Rtt.Microseconds(),
		}
		select {
		case recvCh <- info:
		case <-ctx.Done():
		}
	}
}

func (pi *pingerInfo) start(ipc *IcmpPingChecker) error {
	initialPinger, err := pi.createPinger(pi.nodeInfo.Host)
	if err != nil {
		return err
	}
	pi.ctx, pi.cancel = context.WithCancel(ipc.GetContext())
	pi.wg.Add(1)
	go pi.pingLoop(ipc.resultChan, initialPinger)
	return nil
}

func (pi *pingerInfo) stop() {
	if pi.cancel != nil {
		pi.cancel()
	}
	pi.wg.Wait()
}

func drainPingChannels(ctx context.Context, sendCh, recvCh <-chan *PingPacketInfo, pingPacket map[int]*PingPacketInfo, emit func(delayMs float64) bool) bool {
	for {
		select {
		case i := <-sendCh:
			pingPacket[i.Seq] = i
		default:
			goto drainRecv
		}
	}
drainRecv:
	for {
		select {
		case i := <-recvCh:
			delete(pingPacket, i.Seq)
			select {
			case <-ctx.Done():
				return false
			default:
			}
			if !emit(float64(i.RTT) / 1000) {
				return false
			}
		default:
			return true
		}
	}
}

func cleanupPingerRun(p *ping.Pinger, runDone <-chan struct{}) {
	if p != nil {
		p.Stop()
	}
	<-runDone
	if p != nil {
		p.OnSend = nil
		p.OnRecv = nil
		p.OnFinish = nil
	}
}

func (pi *pingerInfo) pingLoop(ch chan<- *PingResult, initialPinger *ping.Pinger) {
	defer pi.wg.Done()

	sendCh := make(chan *PingPacketInfo, 64)
	recvCh := make(chan *PingPacketInfo, 64)
	checkCycleTicker := time.NewTicker(500 * time.Millisecond)
	defer checkCycleTicker.Stop()

	pingPacket := map[int]*PingPacketInfo{}
	interval := pi.effectiveInterval()
	timeout := pi.effectiveTimeout()

	emitResult := func(ipStr string, delayMs float64) bool {
		result := NewPingResult(pi.nodeInfo.Geo, pi.nodeInfo.ISP, ipStr, pi.pingChecker)
		result.Update(delayMs)
		select {
		case ch <- result:
			return true
		case <-pi.ctx.Done():
			return false
		}
	}

	curPinger := initialPinger
	for {
		if curPinger == nil {
			var err error
			curPinger, err = pi.createPinger(pi.nodeInfo.Host)
			if err != nil {
				log.Error("create icmp pinger failed",
					"geo", pi.nodeInfo.Geo, "isp", pi.nodeInfo.ISP,
					"host", pi.nodeInfo.Host, "err", err)
				pi.failed.Store(true)
				return
			}
		}

		pi.configurePinger(curPinger, sendCh, recvCh)
		ipStr := pingerIPString(curPinger, pi.nodeInfo.Host)

		curRun := pi.run
		if curRun == nil && curPinger != nil {
			curRun = curPinger.Run
		}

		runDone := make(chan struct{})
		go func(fn func() error) {
			defer close(runDone)
			pi.runWith(fn)
		}(curRun)

	cycleLoop:
		for {
			select {
			case <-pi.ctx.Done():
				log.Debug("stop ping", "geo", pi.nodeInfo.Geo, "isp", pi.nodeInfo.ISP, "host", pi.nodeInfo.Host)
				cleanupPingerRun(curPinger, runDone)
				return
			case i := <-sendCh:
				pingPacket[i.Seq] = i
			case i := <-recvCh:
				// 收到包, 删除发送记录
				delete(pingPacket, i.Seq)
				// 转发给外部
				if !emitResult(ipStr, float64(i.RTT)/1000) {
					cleanupPingerRun(curPinger, runDone)
					return
				}
			case <-checkCycleTicker.C:
				// check cycle
				now := time.Now()
				for seq, p := range pingPacket {
					if isTimedOut(p.SendTime, now, timeout) {
						// 丢包
						delete(pingPacket, seq)
						// 转发给外部
						if !emitResult(ipStr, -2) {
							cleanupPingerRun(curPinger, runDone)
							return
						}
					}
				}
			case <-runDone:
				break cycleLoop
			}
		}

		if !drainPingChannels(pi.ctx, sendCh, recvCh, pingPacket, func(delayMs float64) bool {
			return emitResult(ipStr, delayMs)
		}) {
			return
		}
		now := time.Now()
		for seq, p := range pingPacket {
			if isTimedOut(p.SendTime, now, timeout) {
				if !emitResult(ipStr, -2) {
					return
				}
			}
			delete(pingPacket, seq)
		}

		if curPinger != nil {
			curPinger.OnSend = nil
			curPinger.OnRecv = nil
			curPinger.OnFinish = nil
			curPinger = nil
		}

		if pi.failed.Load() {
			return
		}

		waitTimer := time.NewTimer(interval)
		select {
		case <-pi.ctx.Done():
			waitTimer.Stop()
			return
		case <-waitTimer.C:
		}
	}
}

// IcmpPingChecker 基于配置的ip主动探测
type IcmpPingChecker struct {
	*basePingChecker
	pingerMap   map[string]*pingerInfo // ip -> pinger info
	getNodeFunc getNodeFunc
	nodeManager NodeManager // alternative to getNodeFunc
	interval    int         // seconds, default 5
	timeout     int         // seconds, default 1
}

// NewIcmpPingChecker ...
func NewIcmpPingChecker() *IcmpPingChecker {
	return &IcmpPingChecker{
		basePingChecker: newBasePingChecker(),
		interval:        pingInterval,
		timeout:         int(pingTimeout.Seconds()),
	}
}

func (ipc *IcmpPingChecker) Name() string {
	return "icmp_ping"
}

// Init ...
func (ipc *IcmpPingChecker) Init(opts ...OptionFunc) {
	for _, opt := range opts {
		opt(ipc)
	}
}

func (ipc *IcmpPingChecker) Start() {
	if !ipc.isRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ipc.ctx.Done():
				log.Debug("stop icmp ping collector")
				for _, p := range ipc.pingerMap {
					p.stop()
				}
				ipc.pingerMap = nil
				return
			case <-ticker.C:
				oldPingerMap := ipc.pingerMap
				ipc.pingerMap = make(map[string]*pingerInfo)
				// update pinger info
				var nodes []*PingNodeInfo
				if ipc.nodeManager != nil {
					nodes = ipc.nodeManager.ListByUsage("icmp")
				} else if ipc.getNodeFunc != nil {
					nodes = ipc.getNodeFunc()
				}
				// calculate interval and timeout
				interval := time.Second * time.Duration(ipc.interval)
				if interval <= 0 {
					interval = time.Second * time.Duration(pingInterval)
				}
				timeout := time.Second * time.Duration(ipc.timeout)
				if timeout <= 0 {
					timeout = pingTimeout
				}
				// add new node
				for _, node := range nodes {
					if pinger, ok := oldPingerMap[node.Host]; ok {
						// transfor pinger info
						ipc.pingerMap[node.Host] = pinger
						delete(oldPingerMap, node.Host)
					} else {
						// create new pinger info
						pi := newPingerInfo(node, ipc.Name(), interval, timeout)
						if err := pi.start(ipc); err != nil {
							log.Error("start ping failed", "geo", node.Geo, "isp", node.ISP, "host", node.Host)
							continue
						}
						ipc.pingerMap[node.Host] = pi
					}
				}

				// stop all deleted pinger
				for _, deletedPinger := range oldPingerMap {
					deletedPinger.stop()
				}
			}
		}
	}()
}

type PingPacketInfo struct {
	SendTime time.Time
	Seq      int
	RTT      int64
}
