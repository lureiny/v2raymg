package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
)

const (
	defaultDialTimeout = 5 * time.Second
	defaultBufferSize  = 32 * 1024 // 32 KB copy buffer
)

// TCPRelay listens on a local TCP port and forwards connections to a target address.
// It supports traffic counting, rate limiting, and connection limits.
//
// Data path:
// - client -> relay(upload) -> target
// - target -> relay(download) -> client
//
// Shutdown model:
// - Stop() cancels context, closes listener, and waits for all active connection goroutines.
// - Each connection half-close signals peer direction to drain gracefully where possible.
type TCPRelay struct {
	listenAddr string
	targetAddr string
	v6only     bool // set IPV6_V6ONLY on the listen socket

	counter     *TrafficCounter
	limiterUp   Limiter // upload (client → target), nil = unlimited
	limiterDown Limiter // download (target → client), nil = unlimited

	maxConns    int   // 0 = unlimited
	activeConns int64 // atomic

	clientLimiter ClientLimiter // remote IP based client limiter

	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup // tracks active connections
}

// TCPRelayConfig configures a TCPRelay.
type TCPRelayConfig struct {
	ListenAddr    string
	V6Only        bool // set IPV6_V6ONLY on the listen socket (IPv6 wildcard only)
	TargetAddr    string
	Counter       *TrafficCounter
	LimiterUp     Limiter // nil = no limit
	LimiterDown   Limiter // nil = no limit
	MaxConns      int     // 0 = unlimited
	ClientLimiter ClientLimiter // nil = no client-level limit
}

// NewTCPRelay creates a new TCPRelay but does not start listening.
func NewTCPRelay(cfg TCPRelayConfig) *TCPRelay {
	ctx, cancel := context.WithCancel(context.Background())
	return &TCPRelay{
		listenAddr:    cfg.ListenAddr,
		v6only:        cfg.V6Only,
		targetAddr:    cfg.TargetAddr,
		counter:       cfg.Counter,
		limiterUp:     cfg.LimiterUp,
		limiterDown:   cfg.LimiterDown,
		maxConns:      cfg.MaxConns,
		clientLimiter: cfg.ClientLimiter,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// isTemporaryAcceptErr reports whether an Accept error is transient and should
// be retried after a backoff (fd exhaustion, aborted connections) rather than
// treated as fatal.
func isTemporaryAcceptErr(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) && ne.Temporary() { //nolint:staticcheck // Temporary() still flags EMFILE/ECONNABORTED
		return true
	}
	return errors.Is(err, syscall.EMFILE) ||
		errors.Is(err, syscall.ENFILE) ||
		errors.Is(err, syscall.ECONNABORTED)
}

// listenDualStack listens best-effort on both address families. For a wildcard
// host (":port") Go creates a single IPV6_V6ONLY=0 socket that accepts both v4
// and v6. If that bind fails (e.g. IPv6 disabled), it falls back to the IPv4
// wildcard so the relay still starts — the best-effort dual-stack semantics
// PROJECT_GUIDE requires. A non-wildcard host is bound as-is.
func listenDualStack(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}
	if host, port, e := net.SplitHostPort(addr); e == nil && (host == "" || host == "::") {
		if ln2, e2 := net.Listen("tcp", net.JoinHostPort("0.0.0.0", port)); e2 == nil {
			log.Warnf("[TCPRelay] dual-stack bind %q failed (%v); fell back to IPv4-only", addr, err)
			return ln2, nil
		}
	}
	return nil, err // both families failed → real error
}

// Start begins listening and accepting connections.
func (r *TCPRelay) Start() error {
	ln, err := listenTCP(r.listenAddr, r.v6only)
	if err != nil {
		return fmt.Errorf("relay listen %s: %w", r.listenAddr, err)
	}
	r.listener = ln

	r.wg.Add(1)
	go r.acceptLoop()

	return nil
}

// Stop stops accepting new connections and waits for existing ones to drain.
func (r *TCPRelay) Stop() {
	r.cancel()
	if r.listener != nil {
		r.listener.Close()
	}
	r.wg.Wait()
}

// ListenAddr returns the actual listen address (useful when port is 0).
func (r *TCPRelay) ListenAddr() string {
	if r.listener != nil {
		return r.listener.Addr().String()
	}
	return r.listenAddr
}

// ActiveConnections returns the current number of active connections.
func (r *TCPRelay) ActiveConnections() int64 {
	return atomic.LoadInt64(&r.activeConns)
}

func (r *TCPRelay) acceptLoop() {
	defer r.wg.Done()

	var tempDelay time.Duration
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			select {
			case <-r.ctx.Done():
				return
			default:
			}
			// Transient (EMFILE/ENFILE/ECONNABORTED): exponential backoff 5ms→1s
			// so fd exhaustion can't spin the loop at 100% CPU with no log.
			if isTemporaryAcceptErr(err) {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if tempDelay > time.Second {
					tempDelay = time.Second
				}
				log.Warnf("[TCPRelay] transient accept error on %s, backing off %v: %v", r.listenAddr, tempDelay, err)
				select {
				case <-time.After(tempDelay):
				case <-r.ctx.Done():
					return
				}
				continue
			}
			// Permanent: the listener is unusable; stop the accept loop.
			log.Errorf("[TCPRelay] accept failed on %s, stopping relay: %v", r.listenAddr, err)
			return
		}
		tempDelay = 0 // reset backoff after a successful accept

		// Get remote IP for client limiter
		remoteIP := ""
		if remoteAddr := conn.RemoteAddr(); remoteAddr != nil {
			if ip, ok := remoteAddr.(*net.TCPAddr); ok {
				remoteIP = ip.IP.String()
			} else {
				remoteIP = remoteAddr.String()
			}
		}

		// Client limiter check (acquire before connection is counted)
		if r.clientLimiter != nil && remoteIP != "" {
			if !r.clientLimiter.Acquire(remoteIP) {
				conn.Close()
				continue
			}
		}

		// Connection limit check
		if r.maxConns > 0 && atomic.LoadInt64(&r.activeConns) >= int64(r.maxConns) {
			if r.clientLimiter != nil && remoteIP != "" {
				r.clientLimiter.CancelAcquire(remoteIP)
			}
			conn.Close()
			continue
		}

		r.wg.Add(1)
		go r.handleConn(conn, remoteIP)
	}
}

func (r *TCPRelay) handleConn(clientConn net.Conn, remoteIP string) {
	defer r.wg.Done()
	defer clientConn.Close()

	// Dial target FIRST - before incrementing any counters
	// This ensures +1 only happens after connection is fully established
	targetConn, err := net.DialTimeout("tcp", r.targetAddr, defaultDialTimeout)
	if err != nil {
		// Dial failed - cancel the pending acquire to clean up the slot
		if r.clientLimiter != nil && remoteIP != "" {
			r.clientLimiter.CancelAcquire(remoteIP)
		}
		clientConn.Close()
		return
	}
	defer targetConn.Close()

	// Connection established - now increment counters
	atomic.AddInt64(&r.activeConns, 1)
	if r.counter != nil {
		r.counter.IncrConns()
	}

	// Confirm acquire in client limiter (this is the actual +1 for slot counting)
	if r.clientLimiter != nil && remoteIP != "" {
		r.clientLimiter.ConfirmAcquire(remoteIP)
	}

	// Cleanup function - using sync.Once to ensure exactly-once execution
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			atomic.AddInt64(&r.activeConns, -1)
			if r.counter != nil {
				r.counter.DecrConns()
			}
			// Release client limiter (only after successful acquire)
			if r.clientLimiter != nil && remoteIP != "" {
				r.clientLimiter.Release(remoteIP)
			}
		})
	}

	// Track single-direction drain state. The deadline itself lives in the
	// client limiter's drainEnd map, which RecordActivity renews on every read,
	// so we query IsDrainExpired live each tick instead of freezing a snapshot.
	var drainActive bool
	drainMu := sync.Mutex{}

	// Activity tracker for drain reset - called on each real read/write
	recordActivity := func() {
		if r.clientLimiter != nil && remoteIP != "" {
			r.clientLimiter.RecordActivity(remoteIP)
		}
	}

	// Bidirectional copy with counting
	var copyWg sync.WaitGroup
	copyWg.Add(2)

	// Channel to signal copy completion
	uploadDone := make(chan struct{}, 1)
	downloadDone := make(chan struct{}, 1)

	// Client → Target (upload)
	go func() {
		defer copyWg.Done()
		// Pass recordActivity callback - called on each Read to reset drain deadline
		r.copyWithCount(targetConn, clientConn, true, recordActivity)

		// Signal direction end and start drain
		drainMu.Lock()
		if r.clientLimiter != nil && remoteIP != "" {
			r.clientLimiter.OnSingleDirectionEnd(remoteIP) // seeds drainEnd; renewed by RecordActivity
			drainActive = true
		}
		drainMu.Unlock()

		uploadDone <- struct{}{}

		// Signal the other direction to stop
		if tc, ok := targetConn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	// Target → Client (download)
	go func() {
		defer copyWg.Done()
		// Pass recordActivity callback - called on each Read to reset drain deadline
		r.copyWithCount(clientConn, targetConn, false, recordActivity)

		// Signal direction end and start drain
		drainMu.Lock()
		if r.clientLimiter != nil && remoteIP != "" {
			r.clientLimiter.OnSingleDirectionEnd(remoteIP) // seeds drainEnd; renewed by RecordActivity
			drainActive = true
		}
		drainMu.Unlock()

		downloadDone <- struct{}{}

		if tc, ok := clientConn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	// Wait for both directions or context cancel or drain timeout
	idleTick := time.NewTicker(100 * time.Millisecond)
	defer idleTick.Stop()

	// Track completion state
	uploadComplete := false
	downloadComplete := false

	for {
		select {
		case <-uploadDone:
			uploadComplete = true
			// Check drain timeout
			drainMu.Lock()
			if drainActive && r.clientLimiter != nil && r.clientLimiter.IsDrainExpired(remoteIP) {
				drainMu.Unlock()
				// Drain expired, force close
				clientConn.Close()
				targetConn.Close()
				if !downloadComplete {
					<-downloadDone
				}
				cleanup()
				return
			}
			drainMu.Unlock()
			// If both directions complete, exit normally
			if downloadComplete {
				clientConn.Close()
				targetConn.Close()
				cleanup()
				return
			}
		case <-downloadDone:
			downloadComplete = true
			// Check drain timeout
			drainMu.Lock()
			if drainActive && r.clientLimiter != nil && r.clientLimiter.IsDrainExpired(remoteIP) {
				drainMu.Unlock()
				// Drain expired, force close
				clientConn.Close()
				targetConn.Close()
				if !uploadComplete {
					<-uploadDone
				}
				cleanup()
				return
			}
			drainMu.Unlock()
			// If both directions complete, exit normally
			if uploadComplete {
				clientConn.Close()
				targetConn.Close()
				cleanup()
				return
			}
		case <-idleTick.C:
			// Check drain timeout
			drainMu.Lock()
			if drainActive && r.clientLimiter != nil && r.clientLimiter.IsDrainExpired(remoteIP) {
				drainMu.Unlock()
				clientConn.Close()
				targetConn.Close()
				copyWg.Wait()
				cleanup()
				return
			}
			drainMu.Unlock()
		case <-r.ctx.Done():
			clientConn.Close()
			targetConn.Close()
			copyWg.Wait()
			cleanup()
			return
		}
	}
}

// copyWithCount copies from src to dst, counting bytes and optionally rate-limiting.
// isUpload: true = client→target (upload), false = target→client (download).
// onActivity is called on each successful read operation to reset drain deadline.
func (r *TCPRelay) copyWithCount(dst io.Writer, src io.Reader, isUpload bool, onActivity func()) {
	var reader io.Reader = src
	var writer io.Writer = dst

	// Wrap reader to track activity (each Read resets drain deadline)
	if onActivity != nil {
		reader = &activityReader{
			Reader:     reader,
			onActivity: onActivity,
		}
	}

	if isUpload && r.limiterUp != nil {
		reader = r.limiterUp.LimitReader(reader)
	}
	if !isUpload && r.limiterDown != nil {
		reader = r.limiterDown.LimitReader(reader)
	}

	// Use a counting writer
	cw := &countingWriter{
		w:        writer,
		counter:  r.counter,
		isUpload: isUpload,
	}

	buf := make([]byte, defaultBufferSize)
	io.CopyBuffer(cw, reader, buf)
}

// activityReader wraps a Reader and calls onActivity on each successful Read.
type activityReader struct {
	Reader     io.Reader
	onActivity func()
}

func (ar *activityReader) Read(p []byte) (int, error) {
	n, err := ar.Reader.Read(p)
	if n > 0 && err == nil {
		ar.onActivity()
	}
	return n, err
}

// countingWriter wraps a Writer and adds byte counts to a TrafficCounter.
type countingWriter struct {
	w        io.Writer
	counter  *TrafficCounter
	isUpload bool
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	if n > 0 && cw.counter != nil {
		if cw.isUpload {
			cw.counter.AddUpload(int64(n))
		} else {
			cw.counter.AddDownload(int64(n))
		}
	}
	return n, err
}

// Compile-time interface conformance check.
var _ Relay = (*TCPRelay)(nil)
