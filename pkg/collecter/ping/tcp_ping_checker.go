package ping

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
)

// tcpPingerInfo manages TCP probing for a single node.
type tcpPingerInfo struct {
	nodeInfo    *PingNodeInfo
	pingChecker string
	interval    time.Duration
	timeout     time.Duration
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

func newTcpPingerInfo(nodeInfo *PingNodeInfo, pingChecker string, interval, timeout time.Duration) *tcpPingerInfo {
	return &tcpPingerInfo{
		nodeInfo:    nodeInfo,
		pingChecker: pingChecker,
		interval:    interval,
		timeout:     timeout,
	}
}

func (pi *tcpPingerInfo) start(resultChan chan<- *PingResult) error {
	pi.ctx, pi.cancel = context.WithCancel(context.Background())
	pi.wg.Add(1)
	go pi.pingLoop(resultChan)
	return nil
}

func (pi *tcpPingerInfo) stop() {
	if pi.cancel != nil {
		pi.cancel()
	}
	pi.wg.Wait()
}

func (pi *tcpPingerInfo) pingLoop(ch chan<- *PingResult) {
	defer pi.wg.Done()

	ticker := time.NewTicker(pi.interval)
	defer ticker.Stop()

	for {
		select {
		case <-pi.ctx.Done():
			log.Debug("stop tcp ping", "geo", pi.nodeInfo.Geo, "isp", pi.nodeInfo.ISP,
				"host", pi.nodeInfo.Host, "port", pi.nodeInfo.Port)
			return
		case <-ticker.C:
			result := pi.doPing()
			ch <- result
		}
	}
}

func (pi *tcpPingerInfo) doPing() *PingResult {
	addr := fmt.Sprintf("%s:%d", pi.nodeInfo.Host, pi.nodeInfo.Port)
	result := NewPingResult(pi.nodeInfo.Geo, pi.nodeInfo.ISP, addr, pi.pingChecker)

	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, pi.timeout)
	if err != nil {
		result.Update(-2) // loss
		log.Debug("tcp ping failed", "addr", addr, "err", err)
	} else {
		rtt := time.Since(start).Microseconds()
		result.Update(float64(rtt) / 1000) // convert to ms
		conn.Close()
	}
	return result
}

// TcpPingChecker performs TCP-based latency probing.
type TcpPingChecker struct {
	*basePingChecker
	pingerMap   map[string]*tcpPingerInfo // key: host:port
	mu          sync.Mutex
	nodeManager NodeManager
	interval    int // seconds, default 5
	timeout     int // seconds, default 1
}

// NewTcpPingChecker creates a new TcpPingChecker.
func NewTcpPingChecker() *TcpPingChecker {
	return &TcpPingChecker{
		basePingChecker: newBasePingChecker(),
		interval:        5,
		timeout:         1,
	}
}

func (tpc *TcpPingChecker) Name() string {
	return "tcp_ping"
}

// Init initializes the checker with options.
func (tpc *TcpPingChecker) Init(opts ...OptionFunc) {
	for _, opt := range opts {
		opt(tpc)
	}
}

// Start begins the TCP ping probing loop.
func (tpc *TcpPingChecker) Start() {
	if tpc.isRunning.Load() {
		return
	}

	tpc.pingerMap = make(map[string]*tcpPingerInfo)

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-tpc.ctx.Done():
				log.Debug("stop tcp ping collector")
				tpc.stopAllPingers()
				return
			case <-ticker.C:
				tpc.updatePingers()
			}
		}
	}()

	tpc.isRunning.Store(true)
}

func (tpc *TcpPingChecker) updatePingers() {
	tpc.mu.Lock()
	defer tpc.mu.Unlock()

	// Get nodes for TCP ping
	var nodes []*PingNodeInfo
	if tpc.nodeManager != nil {
		nodes = tpc.nodeManager.ListByUsage("tcp")
	}

	// Build address set for current nodes
	currentAddrs := make(map[string]*PingNodeInfo)
	for _, node := range nodes {
		addr := fmt.Sprintf("%s:%d", node.Host, node.Port)
		currentAddrs[addr] = node
	}

	// Calculate interval and timeout
	interval := time.Second * time.Duration(tpc.interval)
	if interval <= 0 {
		interval = 5 * time.Second
	}
	timeout := time.Second * time.Duration(tpc.timeout)
	if timeout <= 0 {
		timeout = time.Second
	}

	// Stop pingers for removed nodes
	for addr, pinger := range tpc.pingerMap {
		if _, exists := currentAddrs[addr]; !exists {
			pinger.stop()
			delete(tpc.pingerMap, addr)
			log.Debug("stopped tcp pinger", "addr", addr)
		}
	}

	// Start pingers for new nodes
	for addr, node := range currentAddrs {
		if _, exists := tpc.pingerMap[addr]; !exists {
			pinger := newTcpPingerInfo(node, tpc.Name(), interval, timeout)
			if err := pinger.start(tpc.resultChan); err != nil {
				log.Error("start tcp pinger failed", "addr", addr, "err", err)
				continue
			}
			tpc.pingerMap[addr] = pinger
			log.Debug("started tcp pinger", "addr", addr)
		}
	}
}

func (tpc *TcpPingChecker) stopAllPingers() {
	tpc.mu.Lock()
	defer tpc.mu.Unlock()

	for _, pinger := range tpc.pingerMap {
		pinger.stop()
	}
	tpc.pingerMap = nil
}

// Stop stops the TCP ping checker.
func (tpc *TcpPingChecker) Stop() {
	if tpc.cancel != nil && tpc.isRunning.Load() {
		tpc.cancel()
		tpc.isRunning.Store(false)
	}
}
