package forward

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultUDPSessionIdle = 60 * time.Second
	udpGCInterval         = 5 * time.Second
	udpReadBufferSize     = 64 * 1024 // UDP max packet size ceiling
	udpLimiterWait        = 10 * time.Millisecond
)

// UDPRelay forwards UDP packets between clients and an upstream target.
//
// Demultiplexing model:
//   - A single PacketConn receives packets from all clients on the listen port.
//   - Per-client sessions (keyed by src addr "ip:port") own a dedicated
//     net.UDPConn to the upstream target; upstream responses arrive on that
//     socket and are forwarded back to the originating client.
//   - Each session has a downstream read loop that runs until the upstream
//     socket errors out, the session is reaped by the idle GC, or Stop is
//     called on the relay.
//
// Rate limiting:
//   - limiterUp/limiterDown are shared with the user's TCP relays. UDP uses
//     WaitN with a short deadline so a saturated bucket causes packet drops
//     instead of blocking the read loop.
//
// Client limiting:
//   - ClientLimiter is also user-level shared. The acquire / confirm / release
//     sequence mirrors TCP handleConn, so a single IP occupies one slot
//     regardless of whether the traffic is TCP or UDP.
//   - OnSingleDirectionEnd is NOT called (UDP has no half-close).
type UDPRelay struct {
	listenAddr string
	targetAddr string

	counter       *TrafficCounter
	limiterUp     Limiter
	limiterDown   Limiter
	maxSessions   int
	clientLimiter ClientLimiter

	sessionIdleTimeout time.Duration

	conn           net.PacketConn
	sessions       sync.Map // key = clientAddr.String(), value = *udpSession
	activeSessions int64    // atomic

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	stopOnce sync.Once
}

// UDPRelayConfig configures a UDPRelay.
type UDPRelayConfig struct {
	ListenAddr         string
	TargetAddr         string
	Counter            *TrafficCounter
	LimiterUp          Limiter
	LimiterDown        Limiter
	MaxSessions        int // 0 = unlimited
	ClientLimiter      ClientLimiter
	SessionIdleTimeout time.Duration // 0 = default (60s)
}

// NewUDPRelay creates a UDPRelay but does not start listening.
func NewUDPRelay(cfg UDPRelayConfig) *UDPRelay {
	idle := cfg.SessionIdleTimeout
	if idle <= 0 {
		idle = defaultUDPSessionIdle
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &UDPRelay{
		listenAddr:         cfg.ListenAddr,
		targetAddr:         cfg.TargetAddr,
		counter:            cfg.Counter,
		limiterUp:          cfg.LimiterUp,
		limiterDown:        cfg.LimiterDown,
		maxSessions:        cfg.MaxSessions,
		clientLimiter:      cfg.ClientLimiter,
		sessionIdleTimeout: idle,
		ctx:                ctx,
		cancel:             cancel,
	}
}

// udpSession holds per-client state. Each session has:
//   - a dedicated upstream UDP socket (client demux requires per-session locals)
//   - a downstream read goroutine
//   - a monotonically updated lastActive for idle GC
//   - closeOnce for idempotent teardown
type udpSession struct {
	clientAddr net.Addr
	key        string
	remoteIP   string
	upstream   *net.UDPConn

	lastActive atomic.Int64 // unix nanos

	ctx    context.Context
	cancel context.CancelFunc

	closeOnce sync.Once
}

// Start binds the listen socket and launches the read/GC goroutines.
// Calling Start twice will fail the second call at ListenPacket (address
// already in use), matching TCPRelay's failure semantics.
func (r *UDPRelay) Start() error {
	pc, err := net.ListenPacket("udp", r.listenAddr)
	if err != nil {
		return fmt.Errorf("udp relay listen %s: %w", r.listenAddr, err)
	}
	r.conn = pc
	r.wg.Add(2)
	go r.readLoop()
	go r.gcLoop()
	return nil
}

// Stop closes the listener, reaps every active session, and blocks until all
// spawned goroutines have returned. It is idempotent.
func (r *UDPRelay) Stop() {
	r.stopOnce.Do(func() {
		r.cancel()
		if r.conn != nil {
			_ = r.conn.Close()
		}
		r.sessions.Range(func(key, value any) bool {
			if s, ok := value.(*udpSession); ok {
				r.reapSession(s)
			}
			return true
		})
		r.wg.Wait()
	})
}

// ListenAddr returns the bound local address.
func (r *UDPRelay) ListenAddr() string {
	if r.conn != nil {
		if la := r.conn.LocalAddr(); la != nil {
			return la.String()
		}
	}
	return r.listenAddr
}

// ActiveSessions returns the number of live UDP sessions.
func (r *UDPRelay) ActiveSessions() int64 {
	return atomic.LoadInt64(&r.activeSessions)
}

// readLoop receives every incoming packet and dispatches it to the session
// that owns the source address (creating one on first sight).
func (r *UDPRelay) readLoop() {
	defer r.wg.Done()
	buf := make([]byte, udpReadBufferSize)
	for {
		n, clientAddr, err := r.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-r.ctx.Done():
				return
			default:
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return
			}
		}
		if n == 0 {
			continue
		}

		key := clientAddr.String()
		sessionAny, exists := r.sessions.Load(key)
		var session *udpSession
		if exists {
			session = sessionAny.(*udpSession)
		} else {
			session = r.establishSession(clientAddr, key)
			if session == nil {
				continue
			}
		}

		// readLoop is the only goroutine reading from r.conn, and
		// forwardUpstream consumes the slice synchronously (upstream.Write
		// copies into the kernel before returning). So buf[:n] is safe until
		// the next ReadFrom call — no need to allocate+copy per datagram.
		r.forwardUpstream(session, buf[:n])
	}
}

// establishSession attempts the 7-step new-session protocol. Any failure
// rolls back prior steps and returns nil; the caller should drop the packet.
//
// NOTE: readLoop synchronously dials upstream on first sight of a new
// clientAddr. This is safe today because TargetAddr, set by UserManager,
// is always an IP literal like "127.0.0.1:<port>" — ResolveUDPAddr +
// DialUDP is μs-level on loopback. If DNS-backed or slow-resolving
// addresses are ever allowed, DialUDP will block readLoop on every new
// client and starve other clients' packets queued in the kernel UDP
// buffer. In that case, move the dial to an async path: buffer pending
// packets per clientAddr, spawn a goroutine to resolve + dial, and
// flush the buffer on success.
func (r *UDPRelay) establishSession(clientAddr net.Addr, key string) *udpSession {
	// NOTE: the activeSessions check + atomic.AddInt64 below is race-free only
	// because readLoop is single-goroutine. If we ever introduce multi-reader
	// (e.g. sharded by hash(clientAddr)), this must switch to a CAS loop.
	// Step 1: session cap. maxSessions<=0 means unlimited.
	if r.maxSessions > 0 && atomic.LoadInt64(&r.activeSessions) >= int64(r.maxSessions) {
		return nil
	}

	remoteIP := extractRemoteIP(clientAddr)

	// Step 2: client limiter acquire (matches TCP acceptLoop semantics).
	if r.clientLimiter != nil && remoteIP != "" {
		if !r.clientLimiter.Acquire(remoteIP) {
			return nil
		}
	}

	// Step 3: dial upstream. Each session owns its own socket so upstream
	// responses can be demuxed back to the right client by source address.
	raddr, err := net.ResolveUDPAddr("udp", r.targetAddr)
	if err != nil {
		if r.clientLimiter != nil && remoteIP != "" {
			r.clientLimiter.CancelAcquire(remoteIP)
		}
		return nil
	}
	upstream, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		if r.clientLimiter != nil && remoteIP != "" {
			r.clientLimiter.CancelAcquire(remoteIP)
		}
		return nil
	}

	sctx, scancel := context.WithCancel(r.ctx)
	session := &udpSession{
		clientAddr: clientAddr,
		key:        key,
		remoteIP:   remoteIP,
		upstream:   upstream,
		ctx:        sctx,
		cancel:     scancel,
	}
	session.lastActive.Store(time.Now().UnixNano())

	// Step 4: register. LoadOrStore guards against a concurrent reader that
	// observed the same client source between our Load and establishSession.
	if existing, loaded := r.sessions.LoadOrStore(key, session); loaded {
		scancel()
		_ = upstream.Close()
		if r.clientLimiter != nil && remoteIP != "" {
			r.clientLimiter.CancelAcquire(remoteIP)
		}
		return existing.(*udpSession)
	}
	atomic.AddInt64(&r.activeSessions, 1)
	if r.counter != nil {
		r.counter.IncrConns()
	}

	// Step 5: confirm the client-slot reservation now that the session is live.
	if r.clientLimiter != nil && remoteIP != "" {
		r.clientLimiter.ConfirmAcquire(remoteIP)
	}

	// Step 6: spawn downstream read loop.
	r.wg.Add(1)
	go r.downstreamLoop(session)

	return session
}

// forwardUpstream applies the upload limiter and writes one datagram to the
// session's upstream socket. Failures drop the packet silently.
func (r *UDPRelay) forwardUpstream(session *udpSession, payload []byte) {
	if len(payload) == 0 {
		return
	}

	if r.limiterUp != nil {
		wctx, cancel := context.WithTimeout(session.ctx, udpLimiterWait)
		err := r.limiterUp.WaitN(wctx, len(payload))
		cancel()
		if err != nil {
			return
		}
	}

	n, err := session.upstream.Write(payload)
	if err != nil {
		// Upstream socket is wedged; reap promptly so the slot/session/
		// counter state clears without waiting for the downstream read
		// timeout. reapSession is idempotent via closeOnce. Run async so
		// the caller (readLoop) is not held by teardown; tracked via r.wg
		// so Stop's wg.Wait() includes this goroutine (no orphan leak).
		// Add(1) is race-free: we are in readLoop (itself in wg), so wg>=1
		// and Stop's Wait has not yet observed zero.
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.reapSession(session)
		}()
		return
	}
	if r.counter != nil {
		r.counter.AddUpload(int64(n))
	}
	session.lastActive.Store(time.Now().UnixNano())
	if r.clientLimiter != nil && session.remoteIP != "" {
		r.clientLimiter.RecordActivity(session.remoteIP)
	}
}

// downstreamLoop reads from the upstream socket and writes back to the
// client. Exits on upstream error or session cancellation, then cleans up.
func (r *UDPRelay) downstreamLoop(session *udpSession) {
	defer r.wg.Done()
	buf := make([]byte, udpReadBufferSize)
	for {
		select {
		case <-session.ctx.Done():
			r.reapSession(session)
			return
		default:
		}

		// SetReadDeadline lets us notice ctx cancellation promptly without
		// blocking a Read forever.
		_ = session.upstream.SetReadDeadline(time.Now().Add(udpLimiterWait * 25))
		n, err := session.upstream.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			r.reapSession(session)
			return
		}
		if n == 0 {
			continue
		}

		payload := buf[:n]

		if r.limiterDown != nil {
			wctx, cancel := context.WithTimeout(session.ctx, udpLimiterWait)
			err := r.limiterDown.WaitN(wctx, n)
			cancel()
			if err != nil {
				continue
			}
		}

		if _, werr := r.conn.WriteTo(payload, session.clientAddr); werr != nil {
			r.reapSession(session)
			return
		}
		if r.counter != nil {
			r.counter.AddDownload(int64(n))
		}
		session.lastActive.Store(time.Now().UnixNano())
		if r.clientLimiter != nil && session.remoteIP != "" {
			r.clientLimiter.RecordActivity(session.remoteIP)
		}
	}
}

// gcLoop reaps sessions that have been idle past sessionIdleTimeout.
func (r *UDPRelay) gcLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(udpGCInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-r.sessionIdleTimeout).UnixNano()
			r.sessions.Range(func(_, value any) bool {
				s := value.(*udpSession)
				if s.lastActive.Load() < cutoff {
					r.reapSession(s)
				}
				return true
			})
		}
	}
}

// reapSession is the unified idempotent session teardown. Safe to call from
// the downstream loop, gc loop, forwardUpstream (on write error), or Stop
// concurrently — subsequent calls are no-ops thanks to closeOnce.
//
// Cleanup ordering (eventually consistent, not strict):
//  1. Under closeOnce: cancel the session ctx (unblocks any in-flight WaitN)
//     and Close the upstream socket (breaks downstreamLoop's Read).
//  2. Immediately remove the session from r.sessions so the readLoop stops
//     routing new client packets to it.
//  3. Immediately decrement activeSessions / counter.DecrConns and release
//     the ClientLimiter slot — these reflect the *logical* session end.
//  4. downstreamLoop eventually returns (Read errors out on the closed
//     upstream or SetReadDeadline fires, then it sees session.ctx.Done or
//     the error path) and decrements r.wg via the deferred wg.Done.
//
// Note that step 4 can happen *after* steps 2–3. The global barrier for
// "all session goroutines have truly exited" is r.wg.Wait() in Stop — not
// reapSession. Callers observing ActiveSessions() right after reapSession
// may see 0 while the downstreamLoop goroutine is still in the final
// couple of read-loop iterations; this is intentional and safe, because
// that goroutine no longer has any path to user-visible side effects
// (the session is off the map, the upstream is closed, the counter is
// already decremented).
//
// If strict-ordering semantics are ever required (e.g. counter must read 0
// only after the goroutine is provably gone), add an explicit
// per-session done channel + <-done here and pay the latency.
func (r *UDPRelay) reapSession(session *udpSession) {
	first := false
	session.closeOnce.Do(func() {
		first = true
		session.cancel()
		_ = session.upstream.Close()
	})
	if !first {
		return
	}

	// Remove from the lookup map so no more packets are dispatched here.
	r.sessions.Delete(session.key)

	atomic.AddInt64(&r.activeSessions, -1)
	if r.counter != nil {
		r.counter.DecrConns()
	}
	if r.clientLimiter != nil && session.remoteIP != "" {
		r.clientLimiter.Release(session.remoteIP)
	}
}

func extractRemoteIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	if ua, ok := addr.(*net.UDPAddr); ok {
		return ua.IP.String()
	}
	return addr.String()
}

// Compile-time interface conformance check.
var _ Relay = (*UDPRelay)(nil)
