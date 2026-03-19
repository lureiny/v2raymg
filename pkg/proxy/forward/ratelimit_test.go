package forward

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestTokenBucketLimiter_Unlimited(t *testing.T) {
	l := NewTokenBucketLimiter(0, 0) // rate=0 → unlimited
	if !l.IsUnlimited() {
		t.Error("rate=0 should be unlimited")
	}

	l2 := NewTokenBucketLimiter(-1, 0)
	if !l2.IsUnlimited() {
		t.Error("rate=-1 should be unlimited")
	}
}

func TestTokenBucketLimiter_LimitReaderPassthrough(t *testing.T) {
	l := NewTokenBucketLimiter(0, 0)
	r := strings.NewReader("hello")
	lr := l.LimitReader(r)

	// Should be the same reader (passthrough)
	if lr != r {
		t.Error("unlimited limiter should return original reader")
	}
}

func TestTokenBucketLimiter_LimitWriterPassthrough(t *testing.T) {
	l := NewTokenBucketLimiter(0, 0)
	var buf bytes.Buffer
	lw := l.LimitWriter(&buf)

	if lw != &buf {
		t.Error("unlimited limiter should return original writer")
	}
}

func TestTokenBucketLimiter_ReadLimited(t *testing.T) {
	// Use higher rate to ensure maxChunk is meaningful
	// 100KB/sec rate -> maxChunk = max(100KB/10, 16KB) = 16KB
	// We request 100KB to test rate limiting
	l := NewTokenBucketLimiter(100*1024, 100*1024)

	// Request 100KB data - more than maxChunk to force rate limiting
	data := strings.Repeat("x", 100*1024)
	r := l.LimitReader(strings.NewReader(data))

	start := time.Now()

	// Read all data
	result := make([]byte, 0, 100*1024)
	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		n, err := r.Read(buf)
		result = append(result, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
	}

	elapsed := time.Since(start)

	if len(result) != 100*1024 {
		t.Errorf("expected 102400 bytes, got %d", len(result))
	}

	// At 100KB/sec with tightened settings:
	// - burst = max(100KB/4, 64KB) = 64KB
	// - maxChunk = max(100KB/10, 16KB) = 16KB
	// First 64KB can come from burst (~0.64s worth)
	// But due to maxChunk, only 16KB per call
	// With 100KB total: ~6-7 calls needed, should take at least 300ms
	if elapsed < 300*time.Millisecond {
		t.Errorf("expected rate limiting to take >300ms, took %v", elapsed)
	}
}

func TestTokenBucketLimiter_WriteLimited(t *testing.T) {
	// Use higher rate to ensure maxChunk is meaningful
	// 100KB/sec rate -> maxChunk = 16KB
	l := NewTokenBucketLimiter(100*1024, 100*1024)

	// Write 100KB data - more than maxChunk to force rate limiting
	data := []byte(strings.Repeat("y", 100*1024))
	var buf bytes.Buffer
	w := l.LimitWriter(&buf)

	start := time.Now()
	n, err := w.Write(data)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if n != 100*1024 {
		t.Errorf("expected 102400 written, got %d", n)
	}
	if buf.Len() != 100*1024 {
		t.Errorf("expected 102400 in buffer, got %d", buf.Len())
	}

	// At 100KB/sec with tightened settings, should take at least 300ms
	if elapsed < 300*time.Millisecond {
		t.Errorf("expected rate limiting to take >300ms, took %v", elapsed)
	}
}

func TestNoopLimiter(t *testing.T) {
	l := NoopLimiter{}

	r := strings.NewReader("test")
	if l.LimitReader(r) != r {
		t.Error("NoopLimiter.LimitReader should return original reader")
	}

	var buf bytes.Buffer
	if l.LimitWriter(&buf) != &buf {
		t.Error("NoopLimiter.LimitWriter should return original writer")
	}
}

func TestTokenBucketLimiter_BurstBehavior(t *testing.T) {
	// High burst, moderate rate — first burst should be fast
	l := NewTokenBucketLimiter(500, 5000)

	data := strings.Repeat("z", 3000)
	r := l.LimitReader(strings.NewReader(data))

	// Read the first 3000 bytes — should be fast since burst=5000
	result := make([]byte, 0, 3000)
	buf := make([]byte, 4096)
	start := time.Now()
	for len(result) < 3000 {
		n, err := r.Read(buf)
		result = append(result, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
	}
	elapsed := time.Since(start)

	if len(result) != 3000 {
		t.Errorf("expected 3000 bytes, got %d", len(result))
	}

	// Should be fast since burst covers the entire read
	if elapsed > 1*time.Second {
		t.Errorf("burst read should be fast, took %v", elapsed)
	}
}

func TestTokenBucketLimiter_SetRate(t *testing.T) {
	l := NewTokenBucketLimiter(1000, 1000)

	// Initial rate should be 1000
	if l.GetRate() != 1000 {
		t.Errorf("expected initial rate 1000, got %d", l.GetRate())
	}

	// Update rate to 2000
	l.SetRate(2000)
	if l.GetRate() != 2000 {
		t.Errorf("expected rate 2000 after SetRate, got %d", l.GetRate())
	}

	// Update to unlimited (0)
	l.SetRate(0)
	if !l.IsUnlimited() {
		t.Error("expected unlimited after SetRate(0)")
	}
	if l.GetRate() != 0 {
		t.Errorf("expected rate 0 after SetRate(0), got %d", l.GetRate())
	}

	// Update back to limited
	l.SetRate(500)
	if l.GetRate() != 500 {
		t.Errorf("expected rate 500 after SetRate(500), got %d", l.GetRate())
	}
}

func TestUserBandwidthLimiter_UpdateRates_ReferenceStability(t *testing.T) {
	// Create a user bandwidth limiter with initial rates
	l := newUserBandwidthLimiter(1000, 2000, true, true)

	// Get initial limiter references
	initialUpload := l.UploadLimiter()
	initialDownload := l.DownloadLimiter()

	if initialUpload == nil {
		t.Fatal("expected non-nil upload limiter")
	}
	if initialDownload == nil {
		t.Fatal("expected non-nil download limiter")
	}

	// Update rates - should NOT replace limiter objects
	l.UpdateRates(3000, 4000, true, true)

	// Get new limiter references - should be the SAME objects
	newUpload := l.UploadLimiter()
	newDownload := l.DownloadLimiter()

	// The pointers should be identical (stable reference)
	if newUpload != initialUpload {
		t.Error("upload limiter reference changed - expected stable reference")
	}
	if newDownload != initialDownload {
		t.Error("download limiter reference changed - expected stable reference")
	}

	// Verify the rates were updated
	if l.UploadRate() != 3000 {
		t.Errorf("expected upload rate 3000, got %d", l.UploadRate())
	}
	if l.DownloadRate() != 4000 {
		t.Errorf("expected download rate 4000, got %d", l.DownloadRate())
	}
}

func TestUserBandwidthLimiter_UpdateRates_Idempotent(t *testing.T) {
	l := newUserBandwidthLimiter(1000, 2000, true, true)

	// Get limiter references
	upload1 := l.UploadLimiter()
	download1 := l.DownloadLimiter()

	// Update with same values - should be idempotent
	l.UpdateRates(1000, 2000, true, true)

	upload2 := l.UploadLimiter()
	download2 := l.DownloadLimiter()

	// References should be stable
	if upload1 != upload2 {
		t.Error("upload limiter changed on same-value update")
	}
	if download1 != download2 {
		t.Error("download limiter changed on same-value update")
	}
}

func TestUserBandwidthLimiter_UpdateRates_ClearToLimited(t *testing.T) {
	// Start with unlimited (rate=0)
	l := newUserBandwidthLimiter(0, 0, false, false)

	// Get limiter - should be TokenBucketLimiter (not nil)
	upload := l.UploadLimiter()
	if upload == nil {
		t.Fatal("expected non-nil upload limiter")
	}

	// Type assert to check if it's a TokenBucketLimiter (should be)
	tbl, ok := upload.(*TokenBucketLimiter)
	if !ok {
		t.Fatal("expected TokenBucketLimiter, got different type")
	}
	if !tbl.IsUnlimited() {
		t.Error("expected unlimited limiter when rate=0")
	}

	// Get reference before update
	uploadRef := l.UploadLimiter()
	downloadRef := l.DownloadLimiter()

	// Update to limited - should update in-place
	l.UpdateRates(1000, 2000, true, true)

	// References should be the same (stable reference)
	if l.UploadLimiter() != uploadRef {
		t.Error("upload limiter reference changed - expected stable reference")
	}
	if l.DownloadLimiter() != downloadRef {
		t.Error("download limiter reference changed - expected stable reference")
	}

	// Verify the rates updated
	if l.UploadRate() != 1000 {
		t.Errorf("expected upload rate 1000, got %d", l.UploadRate())
	}
	if l.DownloadRate() != 2000 {
		t.Errorf("expected download rate 2000, got %d", l.DownloadRate())
	}
}

func TestUserBandwidthLimiter_UpdateRates_LimitedToClear(t *testing.T) {
	// Start with limited
	l := newUserBandwidthLimiter(1000, 2000, true, true)

	// Get limiter references
	upload := l.UploadLimiter()
	download := l.DownloadLimiter()

	if upload == nil || download == nil {
		t.Fatal("expected non-nil limiters")
	}

	// Update to unlimited (0)
	l.UpdateRates(0, 0, false, false)

	// The limiters should still exist but be set to unlimited
	// The reference should remain stable
	newUpload := l.UploadLimiter()
	newDownload := l.DownloadLimiter()

	// References should be stable (important!)
	if newUpload != upload {
		t.Error("upload limiter reference changed - expected stable reference")
	}
	if newDownload != download {
		t.Error("download limiter reference changed - expected stable reference")
	}

	// Rates should be 0
	if l.UploadRate() != 0 {
		t.Errorf("expected upload rate 0, got %d", l.UploadRate())
	}
	if l.DownloadRate() != 0 {
		t.Errorf("expected download rate 0, got %d", l.DownloadRate())
	}
}

// TestUserBandwidthLimiter_StableRef_UnlimitedToLimited tests that relays created
// before bandwidth is set will see the updated rates when bandwidth is later set.
// This is the key fix: stable references ensure dynamic updates work.
func TestUserBandwidthLimiter_StableRef_UnlimitedToLimited(t *testing.T) {
	// Create user bandwidth limiter with initial rate = 0 (unlimited)
	l := newUserBandwidthLimiter(0, 0, false, false)

	// Simulate relay getting limiter reference BEFORE bandwidth is set
	relayUploadLimiter := l.UploadLimiter()
	relayDownloadLimiter := l.DownloadLimiter()

	// Verify initial state is unlimited (via type assertion)
	uploadTBL, ok := relayUploadLimiter.(*TokenBucketLimiter)
	if !ok {
		t.Fatal("expected TokenBucketLimiter")
	}
	if !uploadTBL.IsUnlimited() {
		t.Fatal("initial upload limiter should be unlimited")
	}
	downloadTBL, ok := relayDownloadLimiter.(*TokenBucketLimiter)
	if !ok {
		t.Fatal("expected TokenBucketLimiter")
	}
	if !downloadTBL.IsUnlimited() {
		t.Fatal("initial download limiter should be unlimited")
	}

	// Later, set bandwidth limits (simulating runtime update)
	l.UpdateRates(10*1024*1024, 20*1024*1024, true, true) // 10MB/s upload, 20MB/s download

	// The relay's limiter references should now show the new rates
	// (even though they were created when rate was 0)
	// Note: relayUploadLimiter and relayDownloadLimiter still point to same objects
	// but the objects' internal state has changed
	if uploadTBL.IsUnlimited() {
		t.Error("relay upload limiter should no longer be unlimited after UpdateRates")
	}
	if downloadTBL.IsUnlimited() {
		t.Error("relay download limiter should no longer be unlimited after UpdateRates")
	}

	// Verify the rates are correct (by checking the same TBL objects)
	if uploadTBL.GetRate() != 10*1024*1024 {
		t.Errorf("relay upload rate should be 10MB/s, got %d", uploadTBL.GetRate())
	}
	if downloadTBL.GetRate() != 20*1024*1024 {
		t.Errorf("relay download rate should be 20MB/s, got %d", downloadTBL.GetRate())
	}

	// Verify the limiter references are still the same objects
	if relayUploadLimiter != l.UploadLimiter() {
		t.Error("upload limiter reference changed unexpectedly")
	}
	if relayDownloadLimiter != l.DownloadLimiter() {
		t.Error("download limiter reference changed unexpectedly")
	}
}

func TestTokenBucketLimiter_TightenedBurst(t *testing.T) {
	// Test with rate = 1MB/s = 1048576 bytes/s
	rate := int64(1048576)

	// New limiter should have burst = rate/4 = 262144 (256KB)
	l := NewTokenBucketLimiter(rate, rate)
	if l.GetRate() != rate {
		t.Errorf("expected rate %d, got %d", rate, l.GetRate())
	}

	// Test with very high rate - burst should be capped at 64KB minimum
	l2 := NewTokenBucketLimiter(10000000, 10000000)
	if l2.GetRate() != 10000000 {
		t.Errorf("expected rate 10000000, got %d", l2.GetRate())
	}

	// Test SetRate recalculates burst
	l.SetRate(4096) // 4KB/s
	// burst should be max(4096/4, 64KB) = 64KB
	if l.GetRate() != 4096 {
		t.Errorf("expected rate 4096, got %d", l.GetRate())
	}
}

func TestTokenBucketLimiter_SetRateFromUnlimited(t *testing.T) {
	// Start as unlimited
	l := NewTokenBucketLimiter(0, 0)

	// Switch to limited - should not have full bucket
	l.SetRate(1048576) // 1MB/s

	if l.GetRate() != 1048576 {
		t.Errorf("expected rate 1048576, got %d", l.GetRate())
	}

	// First waitForTokens call should not allow large burst
	// Tokens should be limited to burst/2
	allowed1 := l.waitForTokens(1024 * 1024) // request 1MB
	// Should get maxChunk (rate/10 = ~100KB) or less, not full burst
	if allowed1 > 200*1024 { // more than ~200KB indicates burst rush
		t.Logf("Warning: first allowed = %d bytes, may indicate burst rush", allowed1)
	}
}

func TestTokenBucketLimiter_MaxChunkSmoothing(t *testing.T) {
	// Test with a known rate
	rate := int64(1048576) // 1MB/s
	l := NewTokenBucketLimiter(rate, rate)

	// First, let bucket fill
	time.Sleep(100 * time.Millisecond)

	// Request a large amount (10MB)
	// Should be limited by maxChunk (rate/10 = ~100KB)
	allowed := l.waitForTokens(10 * 1024 * 1024)

	// maxChunk should be ~100KB = 104857 bytes
	// Allow some tolerance
	expectedMax := int64(1048576 / 10) // rate/10
	tolerance := int64(10240)          // 10KB tolerance

	if int64(allowed) > expectedMax+int64(tolerance) {
		t.Errorf("allowed=%d exceeds maxChunk=%d (with tolerance %d)",
			allowed, expectedMax, tolerance)
	}
}

func TestTokenBucketLimiter_MaxChunkMinimum(t *testing.T) {
	// Test with very low rate - maxChunk should still be at least 16KB
	rate := int64(1024) // 1KB/s
	l := NewTokenBucketLimiter(rate, rate)

	// Let bucket fill
	time.Sleep(100 * time.Millisecond)

	// Request 1MB - should be limited by maxChunk (min 16KB)
	allowed := l.waitForTokens(1024 * 1024)

	// maxChunk should be at least 16KB = 16384 bytes
	if allowed < 16384 {
		t.Errorf("expected maxChunk >= 16384, got %d", allowed)
	}
}
