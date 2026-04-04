package server

import (
	"bufio"
	"context"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lureiny/v2raymg/pkg/buildinfo"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

var (
	cpuSamplerOnce sync.Once
	cachedCPUPct   atomic.Value // stores float64

	netSpeedSamplerOnce sync.Once
	cachedNetRxSpeed    atomic.Value // stores float64 (bytes/sec)
	cachedNetTxSpeed    atomic.Value // stores float64 (bytes/sec)
	cachedNetRxBytes    atomic.Uint64
	cachedNetTxBytes    atomic.Uint64

	memSamplerOnce sync.Once
	cachedMemTotal atomic.Uint64
	cachedMemAvail atomic.Uint64

	tcpConnSamplerOnce sync.Once
	cachedTCPConns     atomic.Int32

	// netMonitorIfaceSet is pre-built by InitNetSpeedMonitor.
	// nil means collect all non-loopback interfaces (fallback).
	netMonitorIfaceSet map[string]struct{}
)

func initCPUSampler() {
	cachedCPUPct.Store(float64(-1))
	go func() {
		// Do initial sample immediately.
		pct := sampleCPUPercent(200 * time.Millisecond)
		cachedCPUPct.Store(pct)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			pct := sampleCPUPercent(200 * time.Millisecond)
			cachedCPUPct.Store(pct)
		}
	}()
}

func getCachedCPUPercent() float64 {
	cpuSamplerOnce.Do(initCPUSampler)
	if v, ok := cachedCPUPct.Load().(float64); ok {
		return v
	}
	return -1
}

// InitNetSpeedMonitor resolves the interfaces to monitor and must be called
// once during server initialization before the first GetStatus call.
// configured: list from config file; empty triggers auto-detection.
func InitNetSpeedMonitor(configured []string) {
	ifaces := resolveMonitorInterfaces(configured)
	if len(ifaces) > 0 {
		netMonitorIfaceSet = make(map[string]struct{}, len(ifaces))
		for _, name := range ifaces {
			netMonitorIfaceSet[name] = struct{}{}
		}
		source := "config"
		if len(configured) == 0 {
			source = "auto-detect"
		}
		log.Info("net monitor: selected interfaces", "source", source, "ifaces", strings.Join(ifaces, ","))
	} else {
		log.Warn("net monitor: no default route found, falling back to all non-loopback interfaces (may cause inaccurate speed readings on hosts with virtual interfaces)")
	}
}

// resolveMonitorInterfaces implements priority selection:
// configured list → auto-detect default-route interface → nil (all non-lo fallback).
func resolveMonitorInterfaces(configured []string) []string {
	if len(configured) > 0 {
		return configured
	}
	if iface := detectExternalInterface(); iface != "" {
		return []string{iface}
	}
	return nil
}

// detectExternalInterface reads /proc/net/route and returns the interface
// associated with the default route (lowest metric among Destination=0/0 entries).
// It first looks for gateway routes (RTF_GATEWAY), then falls back to any
// default route (e.g. point-to-point or direct-connected).
// Returns "" on error or when no default route exists.
func detectExternalInterface() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()

	const rtfGateway = 0x2

	// Two-pass: prefer gateway routes, fall back to any default route.
	type candidate struct {
		iface  string
		metric uint64
		hasGW  bool
	}
	var candidates []candidate

	scanner := bufio.NewScanner(f)
	scanner.Scan() // skip header line
	for scanner.Scan() {
		// /proc/net/route columns: Iface Destination Gateway Flags RefCnt Use Metric Mask ...
		fields := strings.Fields(scanner.Text())
		if len(fields) < 8 {
			continue
		}
		if fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil {
			continue
		}
		metric, err := strconv.ParseUint(fields[6], 10, 64)
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{
			iface:  fields[0],
			metric: metric,
			hasGW:  flags&rtfGateway != 0,
		})
	}

	// Pick gateway routes first, then any default route; within each group, lowest metric wins.
	bestIface := ""
	bestMetric := uint64(math.MaxUint64)
	bestHasGW := false
	for _, c := range candidates {
		better := false
		switch {
		case c.hasGW && !bestHasGW:
			better = true // gateway always beats non-gateway
		case c.hasGW == bestHasGW && c.metric < bestMetric:
			better = true // same category, lower metric wins
		}
		if better {
			bestIface = c.iface
			bestMetric = c.metric
			bestHasGW = c.hasGW
		}
	}
	return bestIface
}

func initNetSpeedSampler() {
	cachedNetRxSpeed.Store(float64(-1))
	cachedNetTxSpeed.Store(float64(-1))
	ifaceSet := netMonitorIfaceSet
	go func() {
		prevRx, prevTx := readNetDev(ifaceSet)
		cachedNetRxBytes.Store(prevRx)
		cachedNetTxBytes.Store(prevTx)
		prevTime := time.Now()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			curRx, curTx := readNetDev(ifaceSet)
			cachedNetRxBytes.Store(curRx)
			cachedNetTxBytes.Store(curTx)
			now := time.Now()
			dt := now.Sub(prevTime).Seconds()
			if dt > 0 {
				cachedNetRxSpeed.Store(float64(curRx-prevRx) / dt)
				cachedNetTxSpeed.Store(float64(curTx-prevTx) / dt)
			}
			prevRx, prevTx = curRx, curTx
			prevTime = now
		}
	}()
}

func getCachedNetStats() (rxBytes, txBytes uint64, rxSpeed, txSpeed float64) {
	netSpeedSamplerOnce.Do(initNetSpeedSampler)
	rxBytes = cachedNetRxBytes.Load()
	txBytes = cachedNetTxBytes.Load()
	rxSpeed = -1
	txSpeed = -1
	if v, ok := cachedNetRxSpeed.Load().(float64); ok {
		rxSpeed = v
	}
	if v, ok := cachedNetTxSpeed.Load().(float64); ok {
		txSpeed = v
	}
	return
}

func initMemSampler() {
	total, avail := readMemInfo()
	cachedMemTotal.Store(total)
	cachedMemAvail.Store(avail)
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			total, avail := readMemInfo()
			cachedMemTotal.Store(total)
			cachedMemAvail.Store(avail)
		}
	}()
}

func getCachedMemInfo() (total, avail uint64) {
	memSamplerOnce.Do(initMemSampler)
	return cachedMemTotal.Load(), cachedMemAvail.Load()
}

func initTCPConnSampler() {
	cachedTCPConns.Store(int32(countTCPConnections()))
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			cachedTCPConns.Store(int32(countTCPConnections()))
		}
	}()
}

func getCachedTCPConns() int32 {
	tcpConnSamplerOnce.Do(initTCPConnSampler)
	return cachedTCPConns.Load()
}

func (s *EndNodeServer) GetStatus(ctx context.Context, in *proto.GetStatusReq) (*proto.GetStatusRsp, error) {
	rsp := &proto.GetStatusRsp{Code: 0}

	memTotal, memAvail := getCachedMemInfo()
	var memUsed uint64
	if memTotal > memAvail {
		memUsed = memTotal - memAvail
	}

	rxBytes, txBytes, rxSpeed, txSpeed := getCachedNetStats()

	rsp.Status = &proto.NodeStatus{
		NodeName:           s.cfg.Name,
		OnlyGateway:        s.cfg.OnlyGateway,
		ClusterUserEnabled: s.userMgr.IsClusterEnabled(),
		MemTotal:           memTotal,
		MemUsed:            memUsed,
		MemAvailable:       memAvail,
		NumGoroutine:       int32(runtime.NumGoroutine()),
		CpuPercent:         getCachedCPUPercent(),
		NetRxBytes:         rxBytes,
		NetTxBytes:         txBytes,
		TcpConnections:     getCachedTCPConns(),
		NetRxSpeed:         rxSpeed,
		NetTxSpeed:         txSpeed,
		Version:            buildinfo.Version,
	}
	return rsp, nil
}

// sampleCPUPercent reads /proc/stat twice with the given interval and returns
// the overall CPU usage percentage (0-100). Returns -1 on error (non-Linux).
func sampleCPUPercent(interval time.Duration) float64 {
	idle0, total0, err := readProcStat()
	if err != nil {
		return -1
	}
	time.Sleep(interval)
	idle1, total1, err := readProcStat()
	if err != nil {
		return -1
	}

	// Guard against counter wrap (e.g. CPU hotplug reset).
	if total1 < total0 || idle1 < idle0 {
		return -1
	}
	totalDelta := float64(total1 - total0)
	if totalDelta == 0 {
		return 0
	}
	idleDelta := float64(idle1 - idle0)
	pct := (1 - idleDelta/totalDelta) * 100
	// Clamp to [0, 100] to handle kernel counter quirks on virtualized environments.
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	return math.Round(pct*100) / 100
}

// readProcStat parses the first "cpu" line of /proc/stat and returns (idle, total).
func readProcStat() (idle, total uint64, err error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		// fields: cpu user nice system idle iowait irq softirq steal ...
		if len(fields) < 5 {
			break
		}
		for i := 1; i < len(fields); i++ {
			val, _ := strconv.ParseUint(fields[i], 10, 64) // best-effort: malformed field treated as 0
			total += val
			if i == 4 { // idle field
				idle = val
			}
		}
		return idle, total, nil
	}
	return 0, 0, os.ErrNotExist
}

// readNetDev sums rx_bytes and tx_bytes from /proc/net/dev.
// ifaceSet: if non-nil, only listed interfaces are included; nil collects all non-loopback.
func readNetDev(ifaceSet map[string]struct{}) (rxBytes, txBytes uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:colonIdx])
		if ifaceSet != nil {
			if _, ok := ifaceSet[iface]; !ok {
				continue
			}
		} else if iface == "lo" {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(line[colonIdx+1:]))
		if len(fields) < 10 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		rxBytes += rx
		txBytes += tx
	}
	return rxBytes, txBytes
}

// countTCPConnections counts data lines in /proc/net/tcp and /proc/net/tcp6
// (skipping the header line in each).
func countTCPConnections() int {
	count := 0
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		count += countLinesInFile(path)
	}
	return count
}

// readMemInfo parses /proc/meminfo and returns MemTotal and MemAvailable in bytes.
func readMemInfo() (total, available uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	got := 0
	for scanner.Scan() && got < 2 {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(fields[1], 10, 64)
		val *= 1024 // /proc/meminfo reports in kB
		switch fields[0] {
		case "MemTotal:":
			total = val
			got++
		case "MemAvailable:":
			available = val
			got++
		}
	}
	return total, available
}

// countLinesInFile counts non-empty lines in the given file, skipping the first
// (header) line. Returns 0 if the file cannot be opened.
func countLinesInFile(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024) // /proc/net/tcp can exceed default 64KB
	count := 0
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue // skip header
		}
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count
}
