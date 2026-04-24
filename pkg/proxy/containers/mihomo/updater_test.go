package mihomo

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakes ---

// fakeReleaseClient returns a canned release or error.
type fakeReleaseClient struct {
	release *tools.GitHubRelease
	err     error
	lastTag string // records the tag most recently requested (for "latest" resolution tests)
}

func (f *fakeReleaseClient) FetchRelease(_ context.Context, _, _, tag string) (*tools.GitHubRelease, error) {
	f.lastTag = tag
	if f.err != nil {
		return nil, f.err
	}
	return f.release, nil
}

// fakeDownloader writes deterministic content for each URL it sees. Returns
// err when set, otherwise writes bytes[url] to output (creating whatever the
// underlying file system needs). Counts calls per URL for assertions.
type fakeDownloader struct {
	bytes map[string][]byte
	err   error
	calls map[string]int
}

func newFakeDownloader() *fakeDownloader {
	return &fakeDownloader{bytes: map[string][]byte{}, calls: map[string]int{}}
}

func (f *fakeDownloader) DownloadToFile(_ context.Context, url, output string) error {
	f.calls[url]++
	if f.err != nil {
		return f.err
	}
	data, ok := f.bytes[url]
	if !ok {
		return fmt.Errorf("fakeDownloader: no content registered for %s", url)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	return os.WriteFile(output, data, 0644)
}

// fakeSwapper records calls; optional per-call errors surface as configured.
type fakeSwapper struct {
	swapErr       error
	rollbackErr   error
	swapCount     atomic.Int32
	rollbackCount atomic.Int32
}

func (f *fakeSwapper) SwapAtomic(binaryPath, _ string) (string, error) {
	f.swapCount.Add(1)
	if f.swapErr != nil {
		return "", f.swapErr
	}
	return binaryPath + ".bak", nil
}

func (f *fakeSwapper) Rollback(_, _ string) error {
	f.rollbackCount.Add(1)
	return f.rollbackErr
}

// fakeProcessCtrl records IsRunning/Start/Stop/WaitReady call order + optional
// errors. readyErr is consumed by WaitReady (stage 10a A2-3 probe).
type fakeProcessCtrl struct {
	isRunning bool
	stopErr   error
	startErr  error
	readyErr  error
	startN    atomic.Int32
	stopN     atomic.Int32
	readyN    atomic.Int32
}

func (f *fakeProcessCtrl) IsRunning() bool { return f.isRunning }
func (f *fakeProcessCtrl) Stop() error {
	f.stopN.Add(1)
	if f.stopErr != nil {
		return f.stopErr
	}
	f.isRunning = false
	return nil
}
func (f *fakeProcessCtrl) Start() error {
	f.startN.Add(1)
	if f.startErr != nil {
		return f.startErr
	}
	f.isRunning = true
	return nil
}
func (f *fakeProcessCtrl) WaitReady(ctx context.Context) error {
	f.readyN.Add(1)
	if f.readyErr != nil {
		return f.readyErr
	}
	// Honour cancelled ctx so tests exercising timeout can rely on it even
	// when readyErr is unset.
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// --- helpers ---

// gzipBytes returns a gzip-encoded stream of the supplied payload. Used by
// fakeDownloader to simulate a valid .gz asset.
func gzipBytes(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(payload); err != nil {
		t.Fatalf("gzip.Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip.Close: %v", err)
	}
	return buf.Bytes()
}

// sha256Hex returns the hex-encoded SHA256 digest of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// newUpdaterForTest returns a ready-to-drive updater with the supplied fakes.
// BinaryPath and DownloadDir live under t.TempDir so nothing leaks across tests.
func newUpdaterForTest(t *testing.T, rel ReleaseClient, dl AssetDownloader, sw BinarySwapper, pc ProcessController) *Updater {
	t.Helper()
	dir := t.TempDir()
	return &Updater{
		cfg: UpdaterConfig{
			BinaryPath:  filepath.Join(dir, "mihomo"),
			Owner:       "MetaCubeX",
			Repo:        "mihomo",
			DownloadDir: dir,
		},
		releaseClient: rel,
		downloader:    dl,
		swapper:       sw,
		processCtrl:   pc,
	}
}

// stableRelease returns a *tools.GitHubRelease with the canonical stable
// layout: tag "v1.19.24", v1 gz asset, plus a couple of toolchain variants we
// should NOT match.
func stableRelease() *tools.GitHubRelease {
	return &tools.GitHubRelease{
		TagName: "v1.19.24",
		Assets: []tools.GitHubAsset{
			{Name: "mihomo-linux-amd64-v1-v1.19.24.gz", URL: "https://gh/stable-v1.gz"},
			{Name: "mihomo-linux-amd64-v1-go120-v1.19.24.gz", URL: "https://gh/stable-v1-go120.gz"},
			{Name: "mihomo-linux-amd64-v1-go123-v1.19.24.gz", URL: "https://gh/stable-v1-go123.gz"},
			{Name: "mihomo-linux-amd64-compatible-v1.19.24.gz", URL: "https://gh/compat.gz"},
		},
	}
}

// alphaRelease returns a *tools.GitHubRelease with the Alpha layout: tag
// "Prerelease-Alpha", v1-alpha gz asset with a short hash, a toolchain
// variant we must skip, and a checksums.txt entry.
func alphaRelease() *tools.GitHubRelease {
	return &tools.GitHubRelease{
		TagName: "Prerelease-Alpha",
		Assets: []tools.GitHubAsset{
			{Name: "mihomo-linux-amd64-v1-alpha-1fea551.gz", URL: "https://gh/alpha-v1.gz"},
			{Name: "mihomo-linux-amd64-v1-go120-alpha-1fea551.gz", URL: "https://gh/alpha-v1-go120.gz"},
			{Name: "mihomo-linux-amd64-alpha-1fea551.gz", URL: "https://gh/alpha-default.gz"}, // this one is GOAMD64=v3
			{Name: "checksums.txt", URL: "https://gh/checksums.txt"},
		},
	}
}

// --- Stage 9 tests ---

func TestUpdater_NewUpdater_DefaultsAndValidation(t *testing.T) {
	_, err := NewUpdater(UpdaterConfig{})
	assert.Error(t, err, "empty BinaryPath must fail")

	u, err := NewUpdater(UpdaterConfig{BinaryPath: "/tmp/mihomo"})
	require.NoError(t, err)
	assert.Equal(t, "MetaCubeX", u.cfg.Owner)
	assert.Equal(t, "mihomo", u.cfg.Repo)
	assert.Equal(t, "/tmp", u.cfg.DownloadDir)
}

func TestUpdater_Update_DryRun_SkipsEverything(t *testing.T) {
	rc := &fakeReleaseClient{}
	dl := newFakeDownloader()
	sw := &fakeSwapper{}
	pc := &fakeProcessCtrl{}
	u := newUpdaterForTest(t, rc, dl, sw, pc)

	result, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag: "Prerelease-Alpha",
		DryRun:    true,
	})
	require.NoError(t, err)
	assert.Equal(t, "Prerelease-Alpha", result.ToVersion)
	assert.Empty(t, rc.lastTag, "DryRun must not call the release client")
	assert.Equal(t, int32(0), sw.swapCount.Load())
	assert.Equal(t, int32(0), pc.startN.Load())
}

func TestUpdater_Update_FetchFailure(t *testing.T) {
	rc := &fakeReleaseClient{err: errors.New("network down")}
	u := newUpdaterForTest(t, rc, newFakeDownloader(), &fakeSwapper{}, nil)

	_, err := u.Update(context.Background(), container.UpdateRequest{TargetTag: "latest"})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "fetch", uerr.Stage)
}

func TestUpdater_Update_EmptyTag_ResolvesToLatest(t *testing.T) {
	rc := &fakeReleaseClient{err: errors.New("stop before asset pick")}
	u := newUpdaterForTest(t, rc, newFakeDownloader(), &fakeSwapper{}, nil)

	_, err := u.Update(context.Background(), container.UpdateRequest{TargetTag: ""})
	require.Error(t, err)
	assert.Equal(t, "latest", rc.lastTag, "empty TargetTag must resolve to \"latest\"")
}

func TestUpdater_Update_Stable_HappyPath(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}

	payload := []byte("mock mihomo stable binary")
	gz := gzipBytes(t, payload)

	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	sw := &fakeSwapper{}
	pc := &fakeProcessCtrl{isRunning: true}

	u := newUpdaterForTest(t, rc, dl, sw, pc)

	result, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.NoError(t, err, "stable happy path must succeed")
	assert.Equal(t, "v1.19.24", result.ToVersion)
	assert.False(t, result.ChecksumVerified, "stable release has no checksums.txt, so nothing was verified")
	assert.True(t, result.Restarted)
	assert.Equal(t, int32(1), sw.swapCount.Load())
	assert.Equal(t, int32(1), pc.stopN.Load())
	assert.Equal(t, int32(1), pc.startN.Load())
	// Only the v1 exact-match asset was downloaded — not the go120/go123 variants.
	assert.Equal(t, 1, dl.calls["https://gh/stable-v1.gz"])
	assert.Equal(t, 0, dl.calls["https://gh/stable-v1-go120.gz"])
}

func TestUpdater_Update_Alpha_HappyPath_WithChecksum(t *testing.T) {
	rc := &fakeReleaseClient{release: alphaRelease()}

	payload := []byte("mock mihomo alpha binary")
	gz := gzipBytes(t, payload)
	digest := sha256Hex(gz)
	checksumsFile := digest + "  mihomo-linux-amd64-v1-alpha-1fea551.gz\n" +
		sha256Hex([]byte("other")) + "  mihomo-linux-amd64-v3-alpha-1fea551.gz\n"

	dl := newFakeDownloader()
	dl.bytes["https://gh/alpha-v1.gz"] = gz
	dl.bytes["https://gh/checksums.txt"] = []byte(checksumsFile)

	sw := &fakeSwapper{}
	pc := &fakeProcessCtrl{isRunning: true}

	u := newUpdaterForTest(t, rc, dl, sw, pc)

	result, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "Prerelease-Alpha",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.NoError(t, err)
	assert.True(t, result.ChecksumVerified, "alpha release has checksums.txt, must verify")
	assert.True(t, result.Restarted)
	// alpha-default.gz (unsuffixed, GOAMD64=v3) must NOT be downloaded.
	assert.Equal(t, 0, dl.calls["https://gh/alpha-default.gz"])
	// v1-go120 toolchain variant must NOT be downloaded.
	assert.Equal(t, 0, dl.calls["https://gh/alpha-v1-go120.gz"])
}

func TestUpdater_Update_Alpha_ChecksumMismatch(t *testing.T) {
	rc := &fakeReleaseClient{release: alphaRelease()}

	gz := gzipBytes(t, []byte("real binary"))
	// A digest for a *different* payload — mismatches what we download.
	wrongDigest := sha256Hex([]byte("not what you expect"))
	checksumsFile := wrongDigest + "  mihomo-linux-amd64-v1-alpha-1fea551.gz\n"

	dl := newFakeDownloader()
	dl.bytes["https://gh/alpha-v1.gz"] = gz
	dl.bytes["https://gh/checksums.txt"] = []byte(checksumsFile)

	sw := &fakeSwapper{}
	u := newUpdaterForTest(t, rc, dl, sw, nil)

	_, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag: "Prerelease-Alpha",
	})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "verify", uerr.Stage)
	assert.Equal(t, int32(0), sw.swapCount.Load(), "swap must not happen on verify failure")
}

func TestUpdater_Update_Alpha_ChecksumMissingEntry(t *testing.T) {
	rc := &fakeReleaseClient{release: alphaRelease()}

	gz := gzipBytes(t, []byte("real binary"))

	dl := newFakeDownloader()
	dl.bytes["https://gh/alpha-v1.gz"] = gz
	// checksums.txt exists but has no entry for our asset.
	dl.bytes["https://gh/checksums.txt"] = []byte("beef  mihomo-linux-amd64-v3-alpha-1fea551.gz\n")

	u := newUpdaterForTest(t, rc, dl, &fakeSwapper{}, nil)

	_, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag: "Prerelease-Alpha",
	})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "verify", uerr.Stage)
	assert.Contains(t, uerr.Cause.Error(), "no entry for")
}

func TestUpdater_Update_DownloadFailure(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	dl := newFakeDownloader()
	dl.err = errors.New("connection reset")

	u := newUpdaterForTest(t, rc, dl, &fakeSwapper{}, nil)
	_, err := u.Update(context.Background(), container.UpdateRequest{TargetTag: "v1.19.24"})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "download", uerr.Stage)
}

func TestUpdater_Update_MissingAsset_Stable(t *testing.T) {
	rc := &fakeReleaseClient{release: &tools.GitHubRelease{
		TagName: "v1.99.0",
		// No asset matching mihomo-linux-amd64-v1-v1.99.0.gz
		Assets: []tools.GitHubAsset{{Name: "mihomo-linux-amd64-compatible-v1.99.0.gz", URL: "x"}},
	}}
	u := newUpdaterForTest(t, rc, newFakeDownloader(), &fakeSwapper{}, nil)

	_, err := u.Update(context.Background(), container.UpdateRequest{TargetTag: "v1.99.0"})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "fetch", uerr.Stage)
}

func TestUpdater_Update_MissingAsset_Alpha(t *testing.T) {
	rc := &fakeReleaseClient{release: &tools.GitHubRelease{
		TagName: "Prerelease-Alpha",
		// Only the v3 variant is published.
		Assets: []tools.GitHubAsset{{Name: "mihomo-linux-amd64-alpha-xxxx.gz", URL: "x"}},
	}}
	u := newUpdaterForTest(t, rc, newFakeDownloader(), &fakeSwapper{}, nil)

	_, err := u.Update(context.Background(), container.UpdateRequest{TargetTag: "Prerelease-Alpha"})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "fetch", uerr.Stage)
}

func TestUpdater_Update_BadGzip_ExtractFails(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = []byte("this is not gzip") // intentionally broken

	u := newUpdaterForTest(t, rc, dl, &fakeSwapper{}, nil)

	_, err := u.Update(context.Background(), container.UpdateRequest{TargetTag: "v1.19.24"})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "extract", uerr.Stage)
}

func TestUpdater_Update_SwapFailure(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	sw := &fakeSwapper{swapErr: errors.New("disk full")}
	u := newUpdaterForTest(t, rc, dl, sw, nil)

	_, err := u.Update(context.Background(), container.UpdateRequest{TargetTag: "v1.19.24"})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "swap", uerr.Stage)
}

func TestUpdater_Update_RestartPolicyNever_SkipsProcessCtrl(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	sw := &fakeSwapper{}
	pc := &fakeProcessCtrl{isRunning: true}
	u := newUpdaterForTest(t, rc, dl, sw, pc)

	result, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyNever,
	})
	require.NoError(t, err)
	assert.False(t, result.Restarted)
	assert.Equal(t, int32(0), pc.stopN.Load())
	assert.Equal(t, int32(0), pc.startN.Load())
}

func TestUpdater_Update_NilProcessCtrl_NeverRestarts(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	sw := &fakeSwapper{}
	u := newUpdaterForTest(t, rc, dl, sw, nil) // no processCtrl

	result, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.NoError(t, err)
	assert.False(t, result.Restarted, "Restarted must be false when processCtrl is nil")
	assert.Equal(t, int32(1), sw.swapCount.Load())
}

func TestUpdater_Update_RestartStopFailure_Rollback(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	sw := &fakeSwapper{}
	pc := &fakeProcessCtrl{isRunning: true, stopErr: errors.New("SIGKILL refused")}
	u := newUpdaterForTest(t, rc, dl, sw, pc)

	_, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "restart", uerr.Stage)
	assert.True(t, errors.Is(err, ErrRestartFailed))
	assert.Equal(t, int32(1), sw.rollbackCount.Load(), "rollback must be invoked")
	assert.Equal(t, int32(0), pc.startN.Load(), "Start must not be reached after Stop failure")
}

// TestUpdater_Update_RestartStartFailure_RollbackAlsoFails covers the double-
// failure path added by P1-1: when the post-swap Start fails AND the rollback
// also fails, both causes must be joined so callers can observe either via
// errors.Is. The restart sentinel is still set via Wrapped.
func TestUpdater_Update_RestartStartFailure_RollbackAlsoFails(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	startErr := errors.New("exec: no such file")
	rollbackErr := errors.New("disk full")
	sw := &fakeSwapper{rollbackErr: rollbackErr}
	pc := &fakeProcessCtrl{isRunning: true, startErr: startErr}
	u := newUpdaterForTest(t, rc, dl, sw, pc)

	_, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.Error(t, err)

	// Both causes must be reachable via errors.Is — this is the P1-3 fix.
	assert.True(t, errors.Is(err, ErrRestartFailed), "sentinel must still chain")
	assert.True(t, errors.Is(err, startErr), "original start cause must chain")
	assert.True(t, errors.Is(err, rollbackErr), "rollback cause must also chain")

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "restart", uerr.Stage)
}

// TestUpdater_Update_ConcurrentCallsSerialized exercises the P1-4 mutex. Two
// Update calls concurrently into the same Updater would otherwise collide on
// shared DownloadDir filenames (checksums.txt and the gz asset). With the
// mutex both calls must succeed sequentially without race-detector flags.
func TestUpdater_Update_ConcurrentCallsSerialized(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("payload"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	sw := &fakeSwapper{}
	u := newUpdaterForTest(t, rc, dl, sw, nil)

	const N = 8
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			_, err := u.Update(context.Background(), container.UpdateRequest{
				TargetTag:     "v1.19.24",
				RestartPolicy: container.RestartPolicyNever,
			})
			errs <- err
		}()
	}
	for i := 0; i < N; i++ {
		require.NoError(t, <-errs)
	}
	assert.Equal(t, int32(N), sw.swapCount.Load(), "every call must have attempted a swap")
}

// TestUpdater_Unwrap_ExposesBothCauseAndWrapped is the direct unit-level test
// of the P1-3 Unwrap() []error contract: an ErrUpdateFailed with distinct
// Cause and Wrapped should make errors.Is true for both.
func TestUpdater_Unwrap_ExposesBothCauseAndWrapped(t *testing.T) {
	sentinel := errors.New("sentinel")
	cause := errors.New("cause")
	e := &ErrUpdateFailed{Stage: "x", Cause: cause, Wrapped: sentinel}

	assert.True(t, errors.Is(e, sentinel))
	assert.True(t, errors.Is(e, cause))
	// And Unwrap should filter nil entries
	onlyCause := &ErrUpdateFailed{Stage: "y", Cause: cause}
	unwrapped := onlyCause.Unwrap()
	assert.Len(t, unwrapped, 1)
	assert.Same(t, cause, unwrapped[0])
}

func TestUpdater_Update_RestartStartFailure_Rollback(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	sw := &fakeSwapper{}
	pc := &fakeProcessCtrl{isRunning: true, startErr: errors.New("binary malformed")}
	u := newUpdaterForTest(t, rc, dl, sw, pc)

	_, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "restart", uerr.Stage)
	assert.True(t, errors.Is(err, ErrRestartFailed))
	assert.Equal(t, int32(1), pc.stopN.Load(), "Stop should run before Start fails")
	assert.Equal(t, int32(1), sw.rollbackCount.Load(), "rollback must be invoked")
}

// TestUpdater_Update_StartSucceededButNotReady_Rollback is the stage 10a A2-3
// scenario: Start() returns nil (fork+exec worked) but WaitReady fails because
// the new binary crashed on startup or cannot serve /version. The Updater must
// treat this as a restart-stage failure — Stop the half-alive process, roll
// back the binary, and surface ErrUpdateFailed{Stage:"restart"} wrapping
// ErrRestartFailed so callers that `errors.Is(err, ErrRestartFailed)` still
// trigger. Without WaitReady the old code would report Restarted=true.
func TestUpdater_Update_StartSucceededButNotReady_Rollback(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	sw := &fakeSwapper{}
	readyErr := errors.New("mihomo: /version: connection refused")
	pc := &fakeProcessCtrl{isRunning: true, readyErr: readyErr}
	u := newUpdaterForTest(t, rc, dl, sw, pc)

	result, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "restart", uerr.Stage, "readiness failure is reported as a restart-stage error")
	assert.True(t, errors.Is(err, ErrRestartFailed), "sentinel must chain so callers can detect restart failures")
	assert.True(t, errors.Is(err, readyErr), "original readiness cause must remain reachable via errors.Is")

	// Full call ordering: Stop (pre-swap) → Start → WaitReady (fail) → Stop
	// (post-failure cleanup) → Rollback.
	assert.Equal(t, int32(1), pc.startN.Load(), "Start must have been invoked once")
	assert.Equal(t, int32(1), pc.readyN.Load(), "WaitReady must have been invoked once")
	assert.Equal(t, int32(2), pc.stopN.Load(), "Stop runs both pre-swap and post-readiness-failure")
	assert.Equal(t, int32(1), sw.rollbackCount.Load(), "rollback must be invoked")
	assert.False(t, result.Restarted, "Restarted must remain false when readiness fails")
}

// TestUpdater_Update_ReadinessFailure_StopAlsoFails covers the double-failure
// path inside the stage 10a branch: WaitReady fails AND the cleanup Stop also
// fails. Both causes must be reachable via errors.Is, mirroring the rollback-
// also-fails contract (P1-3 test nearby).
func TestUpdater_Update_ReadinessFailure_StopAlsoFails(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	sw := &fakeSwapper{}
	readyErr := errors.New("post-start probe timed out")
	stopErr := errors.New("SIGKILL refused")
	// isRunning must be false so the initial pre-swap Stop is skipped and
	// the first Stop we exercise is the post-readiness cleanup one. Then
	// set stopErr so that cleanup Stop fails.
	pc := &fakeProcessCtrl{isRunning: false, readyErr: readyErr, stopErr: stopErr}
	u := newUpdaterForTest(t, rc, dl, sw, pc)

	_, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "restart", uerr.Stage)
	assert.True(t, errors.Is(err, ErrRestartFailed), "restart sentinel must chain")
	assert.True(t, errors.Is(err, readyErr), "readiness cause must chain")
	assert.True(t, errors.Is(err, stopErr), "cleanup-Stop cause must chain via errors.Join")
	assert.Equal(t, int32(1), sw.rollbackCount.Load(), "rollback still runs after the joined cleanup failure")
}

// TestUpdater_Update_NotReady_RestartPolicyNever_SkipsProbe locks the
// invariant that the A2-3 probe only fires on policies that actually restart
// the process. RestartPolicyNever must not invoke Start/Stop/WaitReady.
func TestUpdater_Update_NotReady_RestartPolicyNever_SkipsProbe(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	sw := &fakeSwapper{}
	// readyErr would trip the probe IF it ran — must not.
	pc := &fakeProcessCtrl{isRunning: true, readyErr: errors.New("should never be read")}
	u := newUpdaterForTest(t, rc, dl, sw, pc)

	result, err := u.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyNever,
	})
	require.NoError(t, err, "RestartPolicyNever must skip the entire restart block including WaitReady")
	assert.False(t, result.Restarted)
	assert.Equal(t, int32(0), pc.startN.Load())
	assert.Equal(t, int32(0), pc.stopN.Load())
	assert.Equal(t, int32(0), pc.readyN.Load())
}

// --- pickLinuxAmd64V1Asset pure function tests ---

func TestPickLinuxAmd64V1Asset_AlphaPrefix(t *testing.T) {
	got, url, err := pickLinuxAmd64V1Asset(alphaRelease())
	require.NoError(t, err)
	assert.Equal(t, "mihomo-linux-amd64-v1-alpha-1fea551.gz", got)
	assert.Equal(t, "https://gh/alpha-v1.gz", url)
}

func TestPickLinuxAmd64V1Asset_StableExact(t *testing.T) {
	got, url, err := pickLinuxAmd64V1Asset(stableRelease())
	require.NoError(t, err)
	assert.Equal(t, "mihomo-linux-amd64-v1-v1.19.24.gz", got)
	assert.Equal(t, "https://gh/stable-v1.gz", url)
}

func TestPickLinuxAmd64V1Asset_AlphaNoMatch(t *testing.T) {
	_, _, err := pickLinuxAmd64V1Asset(&tools.GitHubRelease{
		TagName: "Prerelease-Alpha",
		Assets:  []tools.GitHubAsset{{Name: "mihomo-linux-amd64-alpha-xxxx.gz", URL: "x"}},
	})
	require.Error(t, err)
}

func TestPickLinuxAmd64V1Asset_StableNoMatch(t *testing.T) {
	_, _, err := pickLinuxAmd64V1Asset(&tools.GitHubRelease{
		TagName: "v2.0.0",
		Assets:  []tools.GitHubAsset{{Name: "mihomo-linux-amd64-v2-v2.0.0.gz", URL: "x"}},
	})
	require.Error(t, err)
}

// --- extractChecksumFor tests ---

// TestExtractChecksumFor uses the exact format mihomo publishes in
// Prerelease-Alpha's checksums.txt: GNU coreutils shape with "./" prefix on
// the filename and 64-char lowercase hex digests. Verified 2026-04-23 via
// curl against the real release asset.
func TestExtractChecksumFor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.txt")
	body := `
4c6b29d8a1c3fefee8ff45c7512fdaa0ed549ac0a4a83ec657d7cc7493ca51ba  ./mihomo-linux-amd64-v1-alpha-1fea551.gz
da586a6bd0e921f68bfd660a168674bc95d62c06b1dcd8f162c640594b6f2188  ./mihomo-linux-amd64-v1-alpha-1fea551.deb
e6de5c1fdfd91d3d8c1f4774f94818b07aa9946c4afbe74aeaa9fc34adc27d04  ./mihomo-android-386-alpha-1fea551.gz
`
	require.NoError(t, os.WriteFile(path, []byte(body), 0644))

	// The ./ prefix on the filename must be stripped before comparison.
	got, err := extractChecksumFor(path, "mihomo-linux-amd64-v1-alpha-1fea551.gz")
	require.NoError(t, err)
	assert.Equal(t, "4c6b29d8a1c3fefee8ff45c7512fdaa0ed549ac0a4a83ec657d7cc7493ca51ba", got)

	got, err = extractChecksumFor(path, "mihomo-android-386-alpha-1fea551.gz")
	require.NoError(t, err)
	assert.Equal(t, "e6de5c1fdfd91d3d8c1f4774f94818b07aa9946c4afbe74aeaa9fc34adc27d04", got)
}

// TestExtractChecksumFor_NoDotSlashPrefix confirms we also match entries
// emitted without the "./" — any future mihomo release workflow change would
// fall into this path.
func TestExtractChecksumFor_NoDotSlashPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.txt")
	digest := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	require.NoError(t, os.WriteFile(path, []byte(digest+"  plainname.gz\n"), 0644))

	got, err := extractChecksumFor(path, "plainname.gz")
	require.NoError(t, err)
	assert.Equal(t, digest, got)
}

func TestExtractChecksumFor_MissingFile(t *testing.T) {
	_, err := extractChecksumFor("/nonexistent/path/checksum.txt", "x")
	require.Error(t, err)
}

func TestExtractChecksumFor_NoEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.txt")
	require.NoError(t, os.WriteFile(path, []byte("ff  other-asset.gz\n"), 0644))

	_, err := extractChecksumFor(path, "target.gz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no entry for")
}

// --- OS/arch guard ---

// TestUpdater_Update_NonLinuxAmd64Fails exercises the platform guard on ALL
// build hosts by stubbing the package-level runtime vars. Prior version used
// runtime.GOOS/GOARCH directly and skipped on linux/amd64 — equivalent to no
// coverage on CI. This version runs everywhere.
func TestUpdater_Update_NonLinuxAmd64Fails(t *testing.T) {
	t.Cleanup(func() {
		updaterRuntimeGOOS = runtime.GOOS
		updaterRuntimeGOARCH = runtime.GOARCH
	})
	updaterRuntimeGOOS = "darwin"
	updaterRuntimeGOARCH = "arm64"

	u := newUpdaterForTest(t, &fakeReleaseClient{}, newFakeDownloader(), &fakeSwapper{}, nil)
	_, err := u.Update(context.Background(), container.UpdateRequest{TargetTag: "latest"})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "fetch", uerr.Stage)
	assert.Contains(t, uerr.Cause.Error(), "darwin/arm64")
}

// --- A2-13: coverage for previously-missing edge cases ---

// TestUpdater_Update_ContextCancelledMidDownload confirms that a caller
// cancelling ctx propagates through as a download-stage failure whose cause
// chains context.Canceled (verified via P1-3 Unwrap).
func TestUpdater_Update_ContextCancelledMidDownload(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	dl := newFakeDownloader()
	dl.err = context.Canceled // simulate downloader honoring ctx
	u := newUpdaterForTest(t, rc, dl, &fakeSwapper{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before we even start

	_, err := u.Update(ctx, container.UpdateRequest{TargetTag: "v1.19.24"})
	require.Error(t, err)

	var uerr *ErrUpdateFailed
	require.True(t, errors.As(err, &uerr))
	assert.Equal(t, "download", uerr.Stage)
	assert.True(t, errors.Is(err, context.Canceled), "context.Canceled must reach callers via the P1-3 multi-err Unwrap")
}

// TestPickLinuxAmd64V1Asset_AlphaAmbiguous verifies behavior when a future
// release lists multiple assets matching the Alpha prefix+.gz filter. Current
// behavior: pick the first, which is deterministic within a given release JSON
// but not under our control. Locks in the contract so a future implementation
// change is noticed.
func TestPickLinuxAmd64V1Asset_AlphaAmbiguous(t *testing.T) {
	rel := &tools.GitHubRelease{
		TagName: "Prerelease-Alpha",
		Assets: []tools.GitHubAsset{
			{Name: "mihomo-linux-amd64-v1-alpha-aaaa111.gz", URL: "https://first"},
			{Name: "mihomo-linux-amd64-v1-alpha-bbbb222.gz", URL: "https://second"},
		},
	}
	name, url, err := pickLinuxAmd64V1Asset(rel)
	require.NoError(t, err)
	assert.Equal(t, "mihomo-linux-amd64-v1-alpha-aaaa111.gz", name, "ambiguous matches: picker returns first")
	assert.Equal(t, "https://first", url)
}

// TestPickLinuxAmd64V1Asset_AlphaRejectsGz_Sig confirms that the .gz suffix
// check keeps e.g. .gz.sig or .gz.asc variants from masquerading as the
// payload asset.
func TestPickLinuxAmd64V1Asset_AlphaRejectsGzSig(t *testing.T) {
	rel := &tools.GitHubRelease{
		TagName: "Prerelease-Alpha",
		Assets: []tools.GitHubAsset{
			{Name: "mihomo-linux-amd64-v1-alpha-abc.gz.sig", URL: "https://sig"},
			{Name: "mihomo-linux-amd64-v1-alpha-abc.gz", URL: "https://gz"},
		},
	}
	_, url, err := pickLinuxAmd64V1Asset(rel)
	require.NoError(t, err)
	assert.Equal(t, "https://gz", url)
}

// --- Container.Update integration ---

// makeBaseContainerCfg returns the minimal MihomoConfig needed to construct a
// MihomoContainer — the goal here is to exercise the Container.Update path,
// not Start/Stop. Paths point into t.TempDir so nothing sticks around.
func makeBaseContainerCfg(t *testing.T, autoDownload bool) MihomoConfig {
	t.Helper()
	dir := t.TempDir()
	return MihomoConfig{
		BinaryPath:         filepath.Join(dir, "mihomo"),
		ConfigFilePath:     filepath.Join(dir, "mihomo.yaml"),
		DataDir:            dir,
		ExternalController: "127.0.0.1:0",
		Secret:             "",
		ReleaseTag:         "latest",
		AutoDownload:       autoDownload,
	}
}

func TestContainer_AutoDownloadTrue_BuildsUpdater(t *testing.T) {
	c, err := NewMihomoContainer(makeBaseContainerCfg(t, true))
	require.NoError(t, err)
	defer c.Close()
	assert.NotNil(t, c.updater, "AutoDownload=true must build a default updater")
}

func TestContainer_AutoDownloadFalse_NoUpdater(t *testing.T) {
	c, err := NewMihomoContainer(makeBaseContainerCfg(t, false))
	require.NoError(t, err)
	defer c.Close()
	assert.Nil(t, c.updater, "AutoDownload=false without WithUpdater must leave updater nil")
}

func TestContainer_WithUpdaterOption_Overrides(t *testing.T) {
	injected, _ := NewUpdater(UpdaterConfig{BinaryPath: "/tmp/injected"})

	c, err := NewMihomoContainer(makeBaseContainerCfg(t, true), WithUpdater(injected))
	require.NoError(t, err)
	defer c.Close()
	assert.Same(t, injected, c.updater, "WithUpdater must win over AutoDownload's default constructor")
}

func TestContainer_Update_NoUpdater_ErrNotSupported(t *testing.T) {
	c, err := NewMihomoContainer(makeBaseContainerCfg(t, false))
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Update(context.Background(), container.UpdateRequest{TargetTag: "latest"})
	assert.ErrorIs(t, err, container.ErrNotSupported)
}

// TestContainer_Update_RefreshesCachedVersionAfterRestart exercises the Q6
// path: after a successful update with Restarted=true, the container probes
// mihomo's /version and updates cachedVersion. Requires a real RESTClient
// pointing at an httptest server because refreshCachedVersionAfterRestart
// reads c.restClient directly.
func TestContainer_Update_RefreshesCachedVersionAfterRestart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"version":"post-update-1fea551","meta":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	u, err := NewUpdater(UpdaterConfig{BinaryPath: filepath.Join(t.TempDir(), "mihomo")})
	require.NoError(t, err)
	u.releaseClient = rc
	u.downloader = dl
	u.swapper = &fakeSwapper{}
	u.processCtrl = &fakeProcessCtrl{isRunning: true}

	c, err := NewMihomoContainer(makeBaseContainerCfg(t, false), WithUpdater(u))
	require.NoError(t, err)
	defer c.Close()

	// Seed a prior cachedVersion + wire a real RESTClient at the test server.
	c.mu.Lock()
	c.cachedVersion = "pre-update-stale"
	c.restClient = NewRESTClient(srv.URL, "")
	c.mu.Unlock()

	result, err := c.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.NoError(t, err)
	require.True(t, result.Restarted, "fake processCtrl Start succeeded")

	c.mu.RLock()
	got := c.cachedVersion
	c.mu.RUnlock()
	assert.Equal(t, "post-update-1fea551", got, "cachedVersion must be refreshed from /version after restart")
}

// TestContainer_Update_RestClientNilAfterRestart_NoRefresh covers the case
// where the REST client is nil (container never got a Start() that built one)
// — the refresh step must be a no-op rather than panic.
func TestContainer_Update_RestClientNilAfterRestart_NoRefresh(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	u, err := NewUpdater(UpdaterConfig{BinaryPath: filepath.Join(t.TempDir(), "mihomo")})
	require.NoError(t, err)
	u.releaseClient = rc
	u.downloader = dl
	u.swapper = &fakeSwapper{}
	u.processCtrl = &fakeProcessCtrl{isRunning: true}

	c, err := NewMihomoContainer(makeBaseContainerCfg(t, false), WithUpdater(u))
	require.NoError(t, err)
	defer c.Close()

	c.mu.Lock()
	c.cachedVersion = "pre-update-value"
	c.restClient = nil // no REST client wired — refresh should skip
	c.mu.Unlock()

	result, err := c.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.NoError(t, err)
	require.True(t, result.Restarted)

	c.mu.RLock()
	got := c.cachedVersion
	c.mu.RUnlock()
	assert.Equal(t, "pre-update-value", got, "no REST client → cachedVersion must stay as-is")
}

// TestContainer_Update_RestFails_PreservesCachedVersion: the REST probe after
// restart fails (server returns 500). Log a warn, keep the prior cachedVersion
// — don't blank it.
func TestContainer_Update_RestFails_PreservesCachedVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	u, err := NewUpdater(UpdaterConfig{BinaryPath: filepath.Join(t.TempDir(), "mihomo")})
	require.NoError(t, err)
	u.releaseClient = rc
	u.downloader = dl
	u.swapper = &fakeSwapper{}
	u.processCtrl = &fakeProcessCtrl{isRunning: true}

	c, err := NewMihomoContainer(makeBaseContainerCfg(t, false), WithUpdater(u))
	require.NoError(t, err)
	defer c.Close()

	c.mu.Lock()
	c.cachedVersion = "preserved-on-rest-failure"
	c.restClient = NewRESTClient(srv.URL, "")
	c.mu.Unlock()

	result, err := c.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.NoError(t, err, "Update itself should succeed even if post-restart probe fails")
	require.True(t, result.Restarted)

	c.mu.RLock()
	got := c.cachedVersion
	c.mu.RUnlock()
	assert.Equal(t, "preserved-on-rest-failure", got, "REST failure must not blank cachedVersion")
}

func TestContainer_Update_WithUpdater_ForwardsResult(t *testing.T) {
	rc := &fakeReleaseClient{release: stableRelease()}
	gz := gzipBytes(t, []byte("ok"))
	dl := newFakeDownloader()
	dl.bytes["https://gh/stable-v1.gz"] = gz

	u, err := NewUpdater(UpdaterConfig{BinaryPath: filepath.Join(t.TempDir(), "mihomo")})
	require.NoError(t, err)
	u.releaseClient = rc
	u.downloader = dl
	u.swapper = &fakeSwapper{}
	// No process controller — RestartPolicyAlways will no-op on restart,
	// Restarted stays false, refreshCachedVersionAfterRestart not called.

	c, err := NewMihomoContainer(makeBaseContainerCfg(t, false), WithUpdater(u))
	require.NoError(t, err)
	defer c.Close()

	result, err := c.Update(context.Background(), container.UpdateRequest{
		TargetTag:     "v1.19.24",
		RestartPolicy: container.RestartPolicyAlways,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "v1.19.24", result.ToVersion)
	assert.False(t, result.Restarted, "no processCtrl configured on fake updater")
}

// --- gunzipToTemp pure function test ---

func TestGunzipToTemp_Success(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.gz")
	payload := []byte("hello gunzip world")
	require.NoError(t, os.WriteFile(src, gzipBytes(t, payload), 0644))

	dst, err := gunzipToTemp(src, dir)
	require.NoError(t, err)
	defer os.Remove(dst)

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, payload, got)

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0100, "extracted file must be executable")
}

func TestGunzipToTemp_BadGzip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.gz")
	require.NoError(t, os.WriteFile(src, []byte("not gzip"), 0644))

	dst, err := gunzipToTemp(src, dir)
	require.Error(t, err)
	assert.Empty(t, dst)
}

