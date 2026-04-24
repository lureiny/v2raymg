package mihomo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/tools"
)

// ErrUpdateFailed is the mihomo updater's stage-tagged error. It carries a
// stage string ("fetch", "download", "verify", "extract", "swap", "restart")
// so callers can triage without string-matching the Cause.
//
// Unwrap returns BOTH the underlying Cause and the sentinel Wrapped (e.g.
// ErrRestartFailed) so errors.Is / errors.As can reach either:
//   - errors.Is(err, ErrRestartFailed) → true when Wrapped == ErrRestartFailed
//   - errors.Is(err, context.Canceled) → true when the download got cancelled
//     and Cause wrapped context.Canceled
// A previous version returned only Wrapped, which hid Cause from the error
// chain and silently failed the second kind of probe.
type ErrUpdateFailed struct {
	Stage   string
	Cause   error
	Wrapped error
}

func (e *ErrUpdateFailed) Error() string {
	return fmt.Sprintf("mihomo update %s failed: %v", e.Stage, e.Cause)
}

// Unwrap exposes both Cause and Wrapped. nil entries are filtered so callers
// don't observe spurious empty hops.
func (e *ErrUpdateFailed) Unwrap() []error {
	errs := make([]error, 0, 2)
	if e.Cause != nil {
		errs = append(errs, e.Cause)
	}
	if e.Wrapped != nil {
		errs = append(errs, e.Wrapped)
	}
	return errs
}

// ErrRestartFailed is the sentinel wrapped by the restart-stage ErrUpdateFailed.
var ErrRestartFailed = errors.New("mihomo updater: restart failed")

// ReleaseClient / AssetDownloader / BinarySwapper / ProcessController mirror
// the xray updater's seams so tests can inject fakes for deterministic runs
// (no network, no real swap, no real process). All four are satisfied by the
// default implementations in pkg/proxy/tools.
type ReleaseClient interface {
	FetchRelease(ctx context.Context, owner, repo, tag string) (*tools.GitHubRelease, error)
}

type AssetDownloader interface {
	DownloadToFile(ctx context.Context, url, output string) error
}

type BinarySwapper interface {
	SwapAtomic(binaryPath, newBinaryPath string) (backupPath string, err error)
	Rollback(binaryPath, backupPath string) error
}

type ProcessController interface {
	Start() error
	Stop() error
	IsRunning() bool
	// WaitReady blocks until the process has passed its readiness probe or ctx
	// fires. Added for stage 10a A2-3: Start() only proves fork+exec succeeded;
	// a binary that crashes on startup (GOAMD64 mismatch, missing shared libs,
	// bad config) still lets Start() return nil, so Update would silently
	// report Restarted=true with a dead process and an already-swapped binary.
	// WaitReady closes that gap. Callers pass a bounded ctx.
	WaitReady(ctx context.Context) error
}

// UpdaterConfig holds the mihomo updater configuration.
type UpdaterConfig struct {
	BinaryPath  string // absolute path of the mihomo binary (required)
	Owner       string // GitHub owner; defaults to "MetaCubeX"
	Repo        string // GitHub repo;  defaults to "mihomo"
	DownloadDir string // scratch dir for .gz + extracted binary; defaults to /tmp
	// ReadinessTimeout bounds the post-restart WaitReady probe. Defaults to
	// 10s to match process.go's readinessTimeout (same REST endpoint, same
	// expected response time). Use 0 to accept the default.
	ReadinessTimeout time.Duration
}

// Updater owns the download → verify → swap → restart flow for mihomo.
// Update is idempotent-safe against retry (a failed run either completes the
// swap + restart or rolls both back so the binary on disk matches the process
// running). Concurrent Update calls are serialized by the internal mutex to
// avoid fixed-name collisions in DownloadDir (e.g. two calls writing the
// shared "mihomo-checksums.txt" or the same asset filename simultaneously).
type Updater struct {
	cfg           UpdaterConfig
	releaseClient ReleaseClient
	downloader    AssetDownloader
	swapper       BinarySwapper
	processCtrl   ProcessController

	// mu serializes Update calls. No lock is taken around other methods
	// (SetProcessController) — those are expected to be configured once
	// before Update is invoked.
	mu sync.Mutex
}

// NewUpdater constructs an Updater with default production dependencies.
// Inject custom implementations by assigning the relevant fields after
// construction (the test suite does this).
func NewUpdater(cfg UpdaterConfig) (*Updater, error) {
	if cfg.BinaryPath == "" {
		return nil, errors.New("mihomo updater: BinaryPath required")
	}
	if cfg.Owner == "" {
		cfg.Owner = "MetaCubeX"
	}
	if cfg.Repo == "" {
		cfg.Repo = "mihomo"
	}
	if cfg.DownloadDir == "" {
		cfg.DownloadDir = "/tmp"
	}
	if cfg.ReadinessTimeout == 0 {
		cfg.ReadinessTimeout = 10 * time.Second
	}
	// Ensure the scratch dir exists up front. Idempotent; avoids re-MkdirAll
	// on every Update call. On failure we still surface at Update time (the
	// downloader's first write will error).
	if err := os.MkdirAll(cfg.DownloadDir, 0755); err != nil {
		return nil, fmt.Errorf("mihomo updater: ensure download dir: %w", err)
	}
	return &Updater{
		cfg:           cfg,
		releaseClient: tools.NewGitHubReleaseClient(),
		downloader:    tools.NewDownloader(),
		swapper:       tools.NewBinarySwapper(),
	}, nil
}

// SetProcessController wires the process controller used by the restart step.
// When nil, RestartPolicy is treated as Never regardless of req.RestartPolicy
// (i.e. "swap the binary and leave caller to restart manually").
func (u *Updater) SetProcessController(pc ProcessController) { u.processCtrl = pc }

// alphaAssetPrefix is the linux/amd64 GOAMD64=v1 Alpha asset prefix:
// "mihomo-linux-amd64-v1-alpha-<git-short-hash>.gz". The "-v1-" infix
// distinguishes from "-v1-go120-alpha-" / "-v1-go123-alpha-" toolchain
// variants (go12x sits between "v1" and "alpha", shifting the prefix) and
// from the "-compatible-" / "-v2-" / "-v3-" GOAMD64 variants. Same value as
// mihomoV1AssetPrefix in downloader.go — duplicated so each file reads
// standalone.
const alphaAssetPrefix = "mihomo-linux-amd64-v1-alpha-"

// runtime check vars. Package-level so tests can stub (A2-12): the prod
// guard in Update() still fires on non-linux/amd64, but tests running on
// linux/amd64 CI can override these to exercise the guard path.
var (
	updaterRuntimeGOOS   = runtime.GOOS
	updaterRuntimeGOARCH = runtime.GOARCH
)

// Update runs the full update flow on linux/amd64. Other OS/arch combinations
// fail fast at the fetch stage — see docs/mihomo-container-design.md decision
// D3 (Q2 of stage 9).
//
// Flow: fetch release → pick asset → download .gz → optional SHA256 verify
// (if checksums.txt is published) → gunzip to temp → atomic swap → optional
// stop+start per RestartPolicy. Any failure after the swap triggers a
// Rollback; any failure before leaves the disk untouched.
//
// TargetTag handling:
//   - "" or "latest": resolves via GitHub's /releases/latest (returns the
//     most recent non-prerelease). To track the permanent Alpha pre-release
//     a caller must pass "Prerelease-Alpha" explicitly — /releases/latest
//     excludes pre-releases by design.
//   - any other string: looked up via /releases/tags/<tag>.
func (u *Updater) Update(ctx context.Context, req container.UpdateRequest) (*container.UpdateResult, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if updaterRuntimeGOOS != "linux" || updaterRuntimeGOARCH != "amd64" {
		return nil, &ErrUpdateFailed{
			Stage: "fetch",
			Cause: fmt.Errorf("only linux/amd64 is supported (current runtime is %s/%s)", updaterRuntimeGOOS, updaterRuntimeGOARCH),
		}
	}

	tag := strings.TrimSpace(req.TargetTag)
	if tag == "" {
		tag = "latest"
	}
	// Empty RestartPolicy is legally "the zero value" and under the "x !=
	// Never" check below it would take the restart branch. Make that implicit
	// behavior explicit so callers reading the struct know what they get —
	// and so a future default change doesn't silently shift semantics.
	if req.RestartPolicy == "" {
		req.RestartPolicy = container.RestartPolicyAlways
	}

	result := &container.UpdateResult{
		Tag:       tag,
		ToVersion: tag,
	}
	if req.DryRun {
		return result, nil
	}

	// Step 1: fetch release metadata.
	log.Info("[Mihomo Updater] fetching release", "owner", u.cfg.Owner, "repo", u.cfg.Repo, "tag", tag)
	release, err := u.releaseClient.FetchRelease(ctx, u.cfg.Owner, u.cfg.Repo, tag)
	if err != nil {
		return result, &ErrUpdateFailed{Stage: "fetch", Cause: err}
	}
	result.Tag = release.TagName
	result.ToVersion = release.TagName
	log.Info("[Mihomo Updater] release resolved", "tag", release.TagName)

	// Step 2: pick the linux/amd64 v1 asset for this release layout.
	assetName, assetURL, err := pickLinuxAmd64V1Asset(release)
	if err != nil {
		return result, &ErrUpdateFailed{Stage: "fetch", Cause: err}
	}
	log.Info("[Mihomo Updater] selected asset", "name", assetName)

	// Step 3: download the gzipped binary. DownloadDir was created in
	// NewUpdater; no per-call MkdirAll.
	gzPath := filepath.Join(u.cfg.DownloadDir, assetName)
	defer os.Remove(gzPath)
	if err := u.downloader.DownloadToFile(ctx, assetURL, gzPath); err != nil {
		return result, &ErrUpdateFailed{Stage: "download", Cause: err}
	}
	log.Info("[Mihomo Updater] downloaded", "path", gzPath)

	// Step 4: verify SHA256 when checksums.txt is published. The Alpha release
	// always ships one; current stable releases (as of v1.19.x) do not, so the
	// branch silently skips on stable. A future stable may add it; the same
	// code path will pick it up automatically.
	if checksumsURL := findAssetURL(release, "checksums.txt"); checksumsURL != "" {
		checksumsPath := filepath.Join(u.cfg.DownloadDir, "mihomo-checksums.txt")
		defer os.Remove(checksumsPath)
		if err := u.downloader.DownloadToFile(ctx, checksumsURL, checksumsPath); err != nil {
			return result, &ErrUpdateFailed{Stage: "verify", Cause: fmt.Errorf("download checksums.txt: %w", err)}
		}
		expected, err := extractChecksumFor(checksumsPath, assetName)
		if err != nil {
			return result, &ErrUpdateFailed{Stage: "verify", Cause: err}
		}
		if err := tools.VerifyChecksum(gzPath, expected); err != nil {
			return result, &ErrUpdateFailed{Stage: "verify", Cause: err}
		}
		result.ChecksumVerified = true
		log.Info("[Mihomo Updater] checksum verified")
	} else {
		// Stable releases currently ship without a checksums.txt (only the
		// Alpha branch does as of 2026-04). This is expected for stable, so
		// log at Debug — a Warn every Update would fill ops logs.
		log.Debugf("[Mihomo Updater] no checksums.txt in release %s; skipping SHA256 verification", release.TagName)
	}

	// Step 5: gunzip to a temp file alongside the download.
	extractedPath, err := gunzipToTemp(gzPath, u.cfg.DownloadDir)
	if err != nil {
		return result, &ErrUpdateFailed{Stage: "extract", Cause: err}
	}
	defer os.Remove(extractedPath)

	// Step 6: atomic swap — moves current binary aside, moves new into place.
	backupPath, err := u.swapper.SwapAtomic(u.cfg.BinaryPath, extractedPath)
	if err != nil {
		return result, &ErrUpdateFailed{Stage: "swap", Cause: err}
	}
	log.Info("[Mihomo Updater] binary swapped", "backup", backupPath)

	// Step 7: restart when a process controller is wired AND policy allows it.
	// A restart failure at any point rolls the binary back so disk and running
	// process stay consistent. If rollback itself also fails, the combined
	// cause is surfaced via errors.Join — disk is now inconsistent and the
	// operator must reconcile manually (the backup is at <BinaryPath>.bak).
	if req.RestartPolicy != container.RestartPolicyNever && u.processCtrl != nil {
		if u.processCtrl.IsRunning() {
			if err := u.processCtrl.Stop(); err != nil {
				return result, u.restartFailure(err, backupPath)
			}
		}
		if err := u.processCtrl.Start(); err != nil {
			return result, u.restartFailure(err, backupPath)
		}
		// Post-Start liveness probe (stage 10a A2-3). Start() returning nil
		// only proves fork+exec succeeded; a new binary can still crash on
		// startup (GOAMD64 mismatch, missing dynamic deps, config parse
		// error) and the old code would still report Restarted=true with a
		// dead process and an already-swapped binary. Bound the probe by
		// ReadinessTimeout; treat failure the same as Start failure — stop
		// the (possibly half-alive) process, roll back the binary, and
		// surface a restart-stage ErrUpdateFailed.
		//
		// Fallback to 10s when the config was built by hand without running
		// NewUpdater (mirrors the RestartPolicy "" → Always normalisation
		// above; sub-callers and test harnesses that assign cfg literally
		// shouldn't fire a zero-timeout cancelled context at every probe).
		readinessTimeout := u.cfg.ReadinessTimeout
		if readinessTimeout <= 0 {
			readinessTimeout = 10 * time.Second
		}
		readyCtx, cancel := context.WithTimeout(ctx, readinessTimeout)
		readyErr := u.processCtrl.WaitReady(readyCtx)
		cancel()
		if readyErr != nil {
			// Stop the half-alive process before rollback so Rollback's
			// atomic rename doesn't race a running process still holding
			// an fd on the new binary path. Stop failures are joined into
			// the returned Cause so callers see both the readiness error
			// and any Stop failure.
			if stopErr := u.processCtrl.Stop(); stopErr != nil {
				readyErr = errors.Join(readyErr, fmt.Errorf("stop after readiness failure: %w", stopErr))
			}
			return result, u.restartFailure(readyErr, backupPath)
		}
		result.Restarted = true
	}

	return result, nil
}

// restartFailure assembles the ErrUpdateFailed for a restart-stage failure
// and attempts a Rollback. If the rollback also fails it is logged at Error
// level and joined into the returned Cause so callers can detect both via
// errors.Is / errors.As.
func (u *Updater) restartFailure(restartErr error, backupPath string) error {
	rbErr := u.swapper.Rollback(u.cfg.BinaryPath, backupPath)
	if rbErr == nil {
		return &ErrUpdateFailed{Stage: "restart", Cause: restartErr, Wrapped: ErrRestartFailed}
	}
	log.Errorf("[Mihomo Updater] rollback after restart failure also failed: restart=%v rollback=%v; disk now inconsistent, check %s.bak",
		restartErr, rbErr, u.cfg.BinaryPath)
	return &ErrUpdateFailed{
		Stage:   "restart",
		Cause:   errors.Join(restartErr, fmt.Errorf("rollback failed: %w", rbErr)),
		Wrapped: ErrRestartFailed,
	}
}

// pickLinuxAmd64V1Asset selects the GOAMD64=v1 gzip asset.
//
// Alpha release (tag "Prerelease-Alpha"): asset name carries the git short
// hash, e.g. "mihomo-linux-amd64-v1-alpha-1fea551.gz". We match by the
// "-v1-alpha-" infix prefix, which also rejects "-v1-go120-alpha-" and
// "-v1-go123-alpha-" toolchain variants whose names sit between the "v1" and
// "alpha" segments.
//
// Stable release (any other tag): asset name is deterministic — it contains
// the resolved tag directly, e.g. "mihomo-linux-amd64-v1-v1.19.24.gz". We
// reconstruct the expected name and match exactly, which naturally excludes
// the "-go120-" / "-go123-" toolchain variants.
func pickLinuxAmd64V1Asset(release *tools.GitHubRelease) (name, url string, err error) {
	if release.TagName == "Prerelease-Alpha" {
		for _, a := range release.Assets {
			if strings.HasPrefix(a.Name, alphaAssetPrefix) && strings.HasSuffix(a.Name, ".gz") {
				return a.Name, a.URL, nil
			}
		}
		return "", "", fmt.Errorf("no asset matching %q*.gz in release %s", alphaAssetPrefix, release.TagName)
	}

	expected := "mihomo-linux-amd64-v1-" + release.TagName + ".gz"
	for _, a := range release.Assets {
		if a.Name == expected {
			return a.Name, a.URL, nil
		}
	}
	return "", "", fmt.Errorf("no asset %q in release %s", expected, release.TagName)
}

// findAssetURL returns the URL of the exact-name-matching asset, or "" if
// none. Used to probe optional assets like checksums.txt.
func findAssetURL(release *tools.GitHubRelease, name string) string {
	for _, a := range release.Assets {
		if a.Name == name {
			return a.URL
		}
	}
	return ""
}

// extractChecksumFor parses a mihomo checksums.txt (GNU coreutils shape:
// "<hex-sha256>  <filename>" per line) and returns the digest for assetName.
// Tolerates blank lines and extra whitespace but requires the filename to be
// the last whitespace-separated field.
//
// Important: mihomo's release workflow emits filenames with a "./" prefix
// (e.g. "./mihomo-linux-amd64-v1-alpha-1fea551.gz"), so we strip that prefix
// before comparing. Without the strip the match silently fails for every
// entry and the Alpha-path SHA256 verify returns "no entry for" for assets
// that are in fact present.
func extractChecksumFor(path, assetName string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read checksums.txt: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "./")
		if name == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %q", assetName)
}

// gunzipToTemp gunzips src into a new temp file under dir, chmods it 0755,
// and returns the path. Caller is responsible for deleting the temp file.
func gunzipToTemp(src, dir string) (string, error) {
	tmp, err := os.CreateTemp(dir, "mihomo-extracted-*")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close() // gunzipFile reopens with O_TRUNC.

	if err := gunzipFile(src, tmpPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("gunzip: %w", err)
	}
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("chmod: %w", err)
	}
	return tmpPath, nil
}
