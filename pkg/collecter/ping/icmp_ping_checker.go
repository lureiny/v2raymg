package ping

import (
	"context"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	ping "github.com/prometheus-community/pro-bing"
)

type getNodeFunc func() []*PingNodeInfo

var (
	pingerIndex  = 0
	pingInterval = 5 // s
	pingTimeout  = time.Second
)

// newPinger host must be ip
func newPinger(host string) (*ping.Pinger, error) {
	pinger, err := ping.NewPinger(host)
	if err != nil {
		return nil, err
	}
	pinger.SetID(pingerIndex)
	pinger.SetPrivileged(true)
	pingerIndex++
	return pinger, nil
}

type pingerInfo struct {
	pinger      *ping.Pinger
	nodeInfo    *PingNodeInfo
	pingChecker string
	interval    time.Duration
	timeout     time.Duration
	ctx         context.Context
	cancel      context.CancelFunc
}

func newPingerInfo(nodeInfo *PingNodeInfo, pingChecker string, interval, timeout time.Duration) *pingerInfo {
	return &pingerInfo{
		nodeInfo:    nodeInfo,
		pingChecker: pingChecker,
		interval:    interval,
		timeout:     timeout,
	}
}

func (pi *pingerInfo) start(ipc *IcmpPingChecker) error {
	var err error = nil
	pi.pinger, err = newPinger(pi.nodeInfo.Host)
	if err != nil {
		return err
	}
	pi.ctx, pi.cancel = context.WithCancel(ipc.GetContext())
	go pi.pingLoop(ipc.resultChan)
	return nil
}

func (pi *pingerInfo) stop() {
	if pi.cancel != nil {
		pi.cancel()
	}
}

func (pi *pingerInfo) pingLoop(ch chan<- *PingResult) {
	sendCh := make(chan *PingPacketInfo)
	recvCh := make(chan *PingPacketInfo)
	pi.pinger.OnSend = func(p *ping.Packet) {
		sendCh <- &PingPacketInfo{
			SendTime: time.Now(),
			Seq:      p.Seq,
		}
	}

	pi.pinger.OnFinish = func(s *ping.Statistics) {
		log.Debug("ping finish", "geo", pi.nodeInfo.Geo, "isp", pi.nodeInfo.ISP,
			"host", pi.nodeInfo.Host, "ip", pi.pinger.IPAddr().IP)
	}

	pi.pinger.OnRecv = func(p *ping.Packet) {
		recvCh <- &PingPacketInfo{
			Seq: p.Seq,
			RTT: p.Rtt.Microseconds(),
		}
	}
	pi.pinger.Interval = time.Second * time.Duration(pingInterval)
	go pi.pinger.Run()

	checkCycleTicker := time.NewTicker(500 * time.Millisecond)
	pingPacket := map[int]*PingPacketInfo{}
	for {
		select {
		case <-pi.ctx.Done():
			log.Debug("stop ping", "geo", pi.nodeInfo.Geo, "isp", pi.nodeInfo.ISP, "host", pi.nodeInfo.Host)
			pi.pinger.Stop()
			return
		case i := <-sendCh:
			pingPacket[i.Seq] = i
		case i := <-recvCh:
			// 收到包, 删除发送记录
			delete(pingPacket, i.Seq)
			// 转发给外部
			result := NewPingResult(pi.nodeInfo.Geo, pi.nodeInfo.ISP, pi.pinger.IPAddr().String(), pi.pingChecker)
			result.Update(float64(i.RTT) / 1000)
			ch <- result
		case <-checkCycleTicker.C:
			// check cycle
			for seq, p := range pingPacket {
				if time.Now().UnixMicro()-p.SendTime.UnixMicro() > pingTimeout.Microseconds() {
					// 丢包
					delete(pingPacket, seq)
					// 转发给外部
					result := NewPingResult(pi.nodeInfo.Geo, pi.nodeInfo.ISP, pi.pinger.IPAddr().String(), pi.pingChecker)
					result.Update(float64(-2))
					ch <- result
				}
			}
		}
	}
}

// IcmpPingChecker 基于配置的ip主动探测
type IcmpPingChecker struct {
	*basePingChecker
	pingerMap   map[string]*pingerInfo // ip -> pinger info
	getNodeFunc getNodeFunc
	nodeManager NodeManager // alternative to getNodeFunc
	interval    int // seconds, default 5
	timeout     int // seconds, default 1
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
	if ipc.isRunning.Load() {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		for range ticker.C {
			select {
			case <-ipc.ctx.Done():
				log.Debug("stop icmp ping collector")
				return
			default:
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
	ipc.isRunning.Store(true)
}

type PingPacketInfo struct {
	SendTime time.Time
	Seq      int
	RTT      int64
}
