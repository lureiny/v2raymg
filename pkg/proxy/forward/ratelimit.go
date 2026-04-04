package forward

import (
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// TokenBucketLimiter implements rate limiting using a token bucket algorithm.
// It implements the Limiter interface.
//
// Tokens represent bytes. The bucket refills at `rate` bytes per second
// with a maximum burst capacity of `burst` bytes.
// A zero or negative rate means unlimited (passthrough).
type TokenBucketLimiter struct {
	unlimited atomic.Bool // fast lock-free check for passthrough

	rate     float64 // tokens (bytes) per second
	burst    float64 // max bucket size
	maxChunk float64 // max bytes per single waitForTokens call (for smoothing)

	mu     sync.Mutex
	tokens float64
	last   time.Time
}

// NewTokenBucketLimiter creates a limiter with the given rate (bytes/sec) and burst size.
// If rate <= 0, the limiter is a passthrough (unlimited).
// Burst is calculated as max(rate/4, 64*1024) for tighter limiting.
// maxChunk is calculated as max(rate/10, 16*1024) to smooth throughput.
func NewTokenBucketLimiter(bytesPerSec int64, burst int64) *TokenBucketLimiter {
	r := float64(bytesPerSec)

	// Tighter burst: max(rate/4, 64KB)
	b := r / 4
	if b < 64*1024 {
		b = 64 * 1024
	}

	// maxChunk for smoothing: max(rate/10, 16KB)
	mc := r / 10
	if mc < 16*1024 {
		mc = 16 * 1024
	}

	l := &TokenBucketLimiter{
		rate:     r,
		burst:    b,
		maxChunk: mc,
		tokens:   b, // start full but limited burst
		last:     time.Now(),
	}
	l.unlimited.Store(bytesPerSec <= 0)
	return l
}

// IsUnlimited returns true if the limiter is a passthrough.
// Uses atomic load for lock-free concurrent access.
func (l *TokenBucketLimiter) IsUnlimited() bool {
	return l.unlimited.Load()
}

// SetRate atomically updates the rate (bytes/sec) of this limiter.
// If rate <= 0, the limiter becomes unlimited.
// Burst is recalculated as max(rate/4, 64KB).
// maxChunk is recalculated as max(rate/10, 16KB).
// When switching from unlimited to limited, tokens are limited to avoid burst rush.
func (l *TokenBucketLimiter) SetRate(rate int64) {
	if rate <= 0 {
		l.unlimited.Store(true) // set unlimited BEFORE zeroing rate to avoid divide-by-zero window
		l.mu.Lock()
		l.rate = 0
		l.burst = 0
		l.maxChunk = 0
		l.mu.Unlock()
		return
	}

	r := float64(rate)

	l.mu.Lock()
	wasUnlimited := l.rate <= 0
	l.rate = r

	// Tighter burst: max(rate/4, 64KB)
	l.burst = r / 4
	if l.burst < 64*1024 {
		l.burst = 64 * 1024
	}

	// maxChunk for smoothing: max(rate/10, 16KB)
	l.maxChunk = r / 10
	if l.maxChunk < 16*1024 {
		l.maxChunk = 16 * 1024
	}

	// When switching from unlimited to limited, limit tokens to avoid burst rush
	// Tokens are capped at burst/2 instead of full burst
	if wasUnlimited {
		if l.tokens > l.burst/2 {
			l.tokens = l.burst / 2
		}
	}
	l.mu.Unlock()
	l.unlimited.Store(false) // set limited AFTER rate is ready to avoid stale-false with rate=0
}

// GetRate returns the current rate setting (bytes/sec).
// Returns 0 if unlimited.
func (l *TokenBucketLimiter) GetRate() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.rate <= 0 {
		return 0
	}
	return int64(l.rate)
}

// LimitReader returns a rate-limited reader. If unlimited, returns r as-is.
func (l *TokenBucketLimiter) LimitReader(r io.Reader) io.Reader {
	if l.IsUnlimited() {
		return r
	}
	return &limitedReader{r: r, limiter: l}
}

// LimitWriter returns a rate-limited writer. If unlimited, returns w as-is.
func (l *TokenBucketLimiter) LimitWriter(w io.Writer) io.Writer {
	if l.IsUnlimited() {
		return w
	}
	return &limitedWriter{w: w, limiter: l}
}

// waitForTokens blocks until n tokens are available, consuming them.
// May return fewer tokens if the request is larger than burst.
// Also limits single release to maxChunk for throughput smoothing.
func (l *TokenBucketLimiter) waitForTokens(n int) int {
	if l.IsUnlimited() {
		return n
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Re-check under lock: rate may have been set to 0 between the atomic
	// check above and acquiring the mutex (stale-false window).
	if l.rate <= 0 {
		return n
	}

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(l.last).Seconds()
	l.last = now
	l.tokens += elapsed * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}

	// Determine how many bytes we can allow
	allowed := int(l.tokens)
	if allowed <= 0 {
		// Wait for at least 1 byte worth of time
		wait := time.Duration(float64(time.Second) / l.rate)
		l.mu.Unlock()
		time.Sleep(wait)
		l.mu.Lock()

		now = time.Now()
		elapsed = now.Sub(l.last).Seconds()
		l.last = now
		l.tokens += elapsed * l.rate
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		allowed = int(l.tokens)
		if allowed <= 0 {
			allowed = 1
		}
	}

	// Apply maxChunk limit for smoothing (single release granularity)
	if l.maxChunk > 0 && allowed > int(l.maxChunk) {
		allowed = int(l.maxChunk)
	}

	if allowed > n {
		allowed = n
	}
	l.tokens -= float64(allowed)
	return allowed
}

// limitedReader wraps an io.Reader with rate limiting.
type limitedReader struct {
	r       io.Reader
	limiter *TokenBucketLimiter
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	// Limit the read size to what the token bucket allows
	allowed := lr.limiter.waitForTokens(len(p))
	return lr.r.Read(p[:allowed])
}

// limitedWriter wraps an io.Writer with rate limiting.
type limitedWriter struct {
	w       io.Writer
	limiter *TokenBucketLimiter
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	totalWritten := 0
	for totalWritten < len(p) {
		remaining := p[totalWritten:]
		allowed := lw.limiter.waitForTokens(len(remaining))
		n, err := lw.w.Write(remaining[:allowed])
		totalWritten += n
		if err != nil {
			return totalWritten, err
		}
	}
	return totalWritten, nil
}

// NoopLimiter is a limiter that does nothing (passthrough).
// It is used as a sentinel value to indicate no rate limiting.
type NoopLimiter struct{}

func (NoopLimiter) LimitReader(r io.Reader) io.Reader { return r }
func (NoopLimiter) LimitWriter(w io.Writer) io.Writer { return w }

// Verify interface compliance at compile time.
var _ Limiter = (*TokenBucketLimiter)(nil)
var _ Limiter = NoopLimiter{}

// --- User-level bandwidth limiting ---

// userBandwidthLimiter manages per-user token buckets for upload and download.
// All rules belonging to the same user share these buckets, achieving
// aggregate bandwidth limits across all user's inbounds.
// IMPORTANT: Limiter references are always stable - we never return nil or create
// new limiter objects. This ensures relays created before bandwidth is set
// will still see the updated rates.
type userBandwidthLimiter struct {
	uploadLimiter   *TokenBucketLimiter // always non-nil for stable reference
	downloadLimiter *TokenBucketLimiter // always non-nil for stable reference
	uploadRate      atomic.Int64        // bytes/sec, 0 = unlimited
	downloadRate    atomic.Int64        // bytes/sec, 0 = unlimited
	uploadSet       atomic.Bool         // true if upload limit was explicitly set
	downloadSet     atomic.Bool         // true if download limit was explicitly set
}

// newUserBandwidthLimiter creates a new user bandwidth limiter.
// Values <= 0 mean unlimited. setXxx flags indicate if the limit was explicitly set.
// Always creates TokenBucketLimiter objects (even for rate=0) to ensure stable references.
func newUserBandwidthLimiter(uploadBPS, downloadBPS int64, uploadSet, downloadSet bool) *userBandwidthLimiter {
	l := &userBandwidthLimiter{}
	l.uploadRate.Store(uploadBPS)
	l.downloadRate.Store(downloadBPS)
	l.uploadSet.Store(uploadSet)
	l.downloadSet.Store(downloadSet)
	l.initLimiters()
	return l
}

// initLimiters creates token bucket limiters based on current rate settings.
// Always creates non-nil TokenBucketLimiter objects to ensure stable references.
// When rate=0, the limiter is unlimited (passthrough).
func (l *userBandwidthLimiter) initLimiters() {
	uploadRate := l.uploadRate.Load()
	downloadRate := l.downloadRate.Load()

	// Always create TokenBucketLimiter, even for rate=0 (unlimited)
	// This ensures stable references for relays
	l.uploadLimiter = NewTokenBucketLimiter(uploadRate, uploadRate)
	l.downloadLimiter = NewTokenBucketLimiter(downloadRate, downloadRate)
}

// UploadLimiter returns the upload limiter.
// Always returns a stable TokenBucketLimiter reference (never nil).
func (l *userBandwidthLimiter) UploadLimiter() Limiter {
	return l.uploadLimiter
}

// DownloadLimiter returns the download limiter.
// Always returns a stable TokenBucketLimiter reference (never nil).
func (l *userBandwidthLimiter) DownloadLimiter() Limiter {
	return l.downloadLimiter
}

// UpdateRates atomically updates the bandwidth rates and set flags.
// Values <= 0 mean unlimited. setXxx indicates if the limit was explicitly set.
// This method updates existing limiter objects in-place to ensure relays
// holding references to these limiters see the updated rates immediately.
// IMPORTANT: Never creates new limiter objects - always updates in-place.
func (l *userBandwidthLimiter) UpdateRates(uploadBPS, downloadBPS int64, uploadSet, downloadSet bool) {
	l.uploadRate.Store(uploadBPS)
	l.downloadRate.Store(downloadBPS)
	l.uploadSet.Store(uploadSet)
	l.downloadSet.Store(downloadSet)

	// Update limiter in-place (never replace reference)
	// TokenBucketLimiter.SetRate(0) makes it unlimited
	l.uploadLimiter.SetRate(uploadBPS)
	l.downloadLimiter.SetRate(downloadBPS)
}

// UploadRate returns the current upload rate setting.
func (l *userBandwidthLimiter) UploadRate() int64 {
	return l.uploadRate.Load()
}

// DownloadRate returns the current download rate setting.
func (l *userBandwidthLimiter) DownloadRate() int64 {
	return l.downloadRate.Load()
}

// IsUploadSet returns true if upload limit was explicitly set.
func (l *userBandwidthLimiter) IsUploadSet() bool {
	return l.uploadSet.Load()
}

// IsDownloadSet returns true if download limit was explicitly set.
func (l *userBandwidthLimiter) IsDownloadSet() bool {
	return l.downloadSet.Load()
}
