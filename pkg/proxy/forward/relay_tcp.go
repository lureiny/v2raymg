package forward

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
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

// Start begins listening and accepting connections.
func (r *TCPRelay) Start() error {
	ln, err := net.Listen("tcp", r.listenAddr)
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

	for {
		conn, err := r.listener.Accept()
		if err != nil {
			select {
			case <-r.ctx.Done():
				return
			default:
				// transient error, continue
				continue
			}
		}

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

	// Track single-direction drain state
	var drainDeadline time.Time
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
			drainDeadline = r.clientLimiter.OnSingleDirectionEnd(remoteIP)
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
			drainDeadline = r.clientLimiter.OnSingleDirectionEnd(remoteIP)
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
			if drainActive && !time.Now().IsZero() && time.Now().After(drainDeadline) {
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
			if drainActive && !time.Now().IsZero() && time.Now().After(drainDeadline) {
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
			if drainActive && !time.Now().IsZero() && time.Now().After(drainDeadline) {
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
