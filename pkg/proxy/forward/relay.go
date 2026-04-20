package forward

import (
	"context"
	"io"
)

// Relay is a network-agnostic forwarding runtime.
//
// Both TCP and UDP implementations must honor:
//   - Per-rule TrafficCounter accumulation (upload/download/activeConns).
//   - User-level shared Limiter (bandwidth) — the Limiter handed to a Relay
//     must be the same instance used by sibling rules of the same user.
//   - User-level shared ClientLimiter — same instance semantics as Limiter.
//   - Stop() must drain in-flight work and release OS resources before returning.
type Relay interface {
	// Start begins listening and accepting/receiving packets.
	Start() error

	// Stop stops accepting new work, releases OS resources, and waits for
	// in-flight work to drain. Safe to call once; subsequent calls are no-ops.
	Stop()

	// ListenAddr returns the actual bound address (useful when port=0).
	ListenAddr() string
}

// Limiter is the interface for rate limiting a stream.
//
// Contract:
//   - LimitReader/LimitWriter wrap byte streams (used by TCP copy loops).
//   - WaitN blocks until n tokens are available or ctx is cancelled (used by
//     UDP per-packet pacing). Implementations MUST share the same underlying
//     token bucket across LimitReader/LimitWriter/WaitN so that a single
//     bandwidth budget covers both TCP streams and UDP packets.
//   - If unlimited (passthrough), WaitN returns nil immediately.
type Limiter interface {
	// LimitReader returns an io.Reader that reads from r at the limited rate.
	LimitReader(r io.Reader) io.Reader
	// LimitWriter returns an io.Writer that writes to w at the limited rate.
	LimitWriter(w io.Writer) io.Writer
	// WaitN blocks until n tokens are available, consuming them atomically
	// from the same bucket used by LimitReader/LimitWriter. Returns ctx.Err()
	// if ctx is cancelled before tokens are available.
	WaitN(ctx context.Context, n int) error
}
