package forward

import (
	"bytes"
	"io"
	"testing"
	"time"
)

// TestLimitReader_ExistingConnHonorsLaterSetRate covers finding #3: a reader
// wrapped while the bucket was unlimited must start honoring a rate set later
// (in-place SetRate on the stable-reference bucket). Before the fix, LimitReader
// returned the bare reader when unlimited, so the connection bypassed the limit
// forever.
func TestLimitReader_ExistingConnHonorsLaterSetRate(t *testing.T) {
	l := NewTokenBucketLimiter(0, 0) // start unlimited
	src := bytes.NewReader(make([]byte, 160*1024))
	lr := l.LimitReader(src)

	// Flip unlimited -> limited AFTER wrapping (mirrors SetUserBandwidthLimit
	// applied to an already-established connection).
	l.SetRate(64 * 1024) // 64 KB/s

	start := time.Now()
	n, _ := io.Copy(io.Discard, lr)
	elapsed := time.Since(start)

	if n != 160*1024 {
		t.Fatalf("read %d bytes, want 160KB", n)
	}
	// ~32KB burst then 64KB/s => ~2s. Any real throttle is far above the
	// near-instant bypass the bug produced.
	if elapsed < 700*time.Millisecond {
		t.Fatalf("existing connection was not throttled after SetRate (took %v) — rate-limit bypass", elapsed)
	}
}

type shortReader struct{ n int }

func (s *shortReader) Read(p []byte) (int, error) {
	m := s.n
	if m > len(p) {
		m = len(p)
	}
	for i := 0; i < m; i++ {
		p[i] = 'x'
	}
	return m, nil
}

// TestLimitedReader_ShortReadRefund covers finding #4: a short read must refund
// the tokens deducted for the unread remainder, so small packets don't burn the
// shared bucket at buffer granularity. Before the fix, a 32KB-buffer read that
// returned 100 bytes consumed the whole maxChunk allotment.
func TestLimitedReader_ShortReadRefund(t *testing.T) {
	l := NewTokenBucketLimiter(1<<20, 1<<20) // 1 MB/s (burst 256KB, maxChunk 100KB)
	// Seed a mid-level balance: enough that waitForTokens grants the full 32KB
	// buffer, with headroom below burst so a refund is observable (not clamped),
	// and reset last so elapsed-time refill doesn't skew the before/after delta.
	l.mu.Lock()
	l.tokens = 200 * 1024
	l.last = time.Now()
	before := l.tokens
	l.mu.Unlock()

	lr := l.LimitReader(&shortReader{n: 100})
	buf := make([]byte, 32*1024)

	n, err := lr.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != 100 {
		t.Fatalf("expected short read of 100 bytes, got %d", n)
	}

	l.mu.Lock()
	after := l.tokens
	l.mu.Unlock()

	consumed := before - after
	// Post-fix: ~100 tokens consumed (the bytes actually read). Pre-fix: the
	// whole allowed chunk (thousands) was consumed and never refunded.
	if consumed > 1000 {
		t.Fatalf("short read consumed %.0f tokens, expected ~100 (unrefunded tokens burn the shared bucket)", consumed)
	}
}
