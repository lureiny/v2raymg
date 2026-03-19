// Package xray provides Xray container implementation.
package xray

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"github.com/lureiny/v2raymg/pkg/log"
	"os"
	"path/filepath"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/tools"
)

// ErrUpdateFailed is the main error for update failures.
type ErrUpdateFailed struct {
	Stage   string // fetch/download/verify/swap/restart/rollback
	Cause   error
	Wrapped error
}

func (e *ErrUpdateFailed) Error() string {
	return fmt.Sprintf("update %s failed: %v", e.Stage, e.Cause)
}

func (e *ErrUpdateFailed) Unwrap() error {
	return e.Wrapped
}

// ReleaseClient interface for fetching releases.
type ReleaseClient interface {
	FetchRelease(ctx context.Context, owner, repo, tag string) (*tools.GitHubRelease, error)
}

// AssetDownloader interface for downloading files.
type AssetDownloader interface {
	DownloadToFile(ctx context.Context, url, output string) error
}

// BinarySwapper interface for atomic swaps.
type BinarySwapper interface {
	SwapAtomic(binaryPath, newBinaryPath string) (backupPath string, err error)
	Rollback(binaryPath, backupPath string) error
}

// ProcessController interface for process control.
type ProcessController interface {
	Start() error
	Stop() error
	IsRunning() bool
}

// Updater handles Xray binary updates.
type Updater struct {
	config        UpdaterConfig
	releaseClient ReleaseClient
	downloader    AssetDownloader
	swapper       BinarySwapper
	processCtrl   ProcessController
}

// UpdaterConfig holds updater configuration.
type UpdaterConfig struct {
	BinaryPath   string
	Owner        string
	Repo         string
	AssetName    string
	ChecksumName string
	DownloadDir  string
	BinaryName   string // Name of binary inside zip, e.g., "xray"
}

// NewUpdater creates a new Xray updater.
func NewUpdater(cfg UpdaterConfig) (*Updater, error) {
	if cfg.BinaryPath == "" {
		return nil, errors.New("binary path required")
	}
	if cfg.Owner == "" {
		cfg.Owner = "XTLS"
	}
	if cfg.Repo == "" {
		cfg.Repo = "Xray-core"
	}
	if cfg.AssetName == "" {
		cfg.AssetName = "Xray-linux-64.zip"
	}
	if cfg.DownloadDir == "" {
		cfg.DownloadDir = "/tmp"
	}
	if cfg.BinaryName == "" {
		cfg.BinaryName = "xray"
	}

	u := &Updater{
		config:        cfg,
		releaseClient: tools.NewGitHubReleaseClient(),
		downloader:    tools.NewDownloader(),
		swapper:       tools.NewBinarySwapper(),
	}
	return u, nil
}

// Update performs the full update flow: fetch -> download -> verify -> extract -> swap -> restart -> rollback on failure.
func (u *Updater) Update(ctx context.Context, req container.UpdateRequest) (*container.UpdateResult, error) {
	result := &container.UpdateResult{
		ToVersion: req.TargetTag,
		Tag:       req.TargetTag,
	}

	if req.DryRun {
		return result, nil
	}

	// Step 1: Fetch release
	log.Info("[Xray Updater] fetching release", "owner", u.config.Owner, "repo", u.config.Repo, "tag", req.TargetTag)
	release, err := u.releaseClient.FetchRelease(ctx, u.config.Owner, u.config.Repo, req.TargetTag)
	if err != nil {
		return result, &ErrUpdateFailed{Stage: "fetch", Cause: err}
	}
	result.ToVersion = release.TagName
	log.Info("[Xray Updater] found release", "tag", release.TagName)

	// Find asset
	assetURL := u.findAsset(release, u.config.AssetName)
	if assetURL == "" {
		return result, &ErrUpdateFailed{Stage: "fetch", Cause: errors.New("asset not found: " + u.config.AssetName)}
	}

	// Step 2: Download
	zipPath := filepath.Join(u.config.DownloadDir, u.config.AssetName)
	defer os.Remove(zipPath)

	log.Info("[Xray Updater] downloading asset", "asset", u.config.AssetName, "dest", zipPath)
	if err := u.downloader.DownloadToFile(ctx, assetURL, zipPath); err != nil {
		return result, &ErrUpdateFailed{Stage: "download", Cause: err}
	}
	log.Info("[Xray Updater] download complete", "path", zipPath)

	// Step 3: Verify checksum (if available)
	checksumVerified := false
	if u.config.ChecksumName != "" {
		log.Info("[Xray Updater] verifying checksum", "checksum", u.config.ChecksumName)
		checksumURL := u.findAsset(release, u.config.ChecksumName)
		if checksumURL != "" {
			checksumPath := filepath.Join(u.config.DownloadDir, u.config.ChecksumName)
			defer os.Remove(checksumPath)

			if err := u.downloader.DownloadToFile(ctx, checksumURL, checksumPath); err == nil {
				checksumData, _ := os.ReadFile(checksumPath)
				if err := tools.VerifyChecksum(zipPath, string(checksumData)); err != nil {
					return result, &ErrUpdateFailed{Stage: "verify", Cause: err}
				}
				checksumVerified = true
				log.Info("[Xray Updater] checksum verified")
			}
		}
	}
	result.ChecksumVerified = checksumVerified

	// Step 4: Extract binary from zip
	log.Info("[Xray Updater] extracting binary", "name", u.config.BinaryName)
	extractedPath, err := u.extractBinary(zipPath)
	if err != nil {
		return result, &ErrUpdateFailed{Stage: "extract", Cause: err}
	}
	log.Info("[Xray Updater] binary extracted", "path", extractedPath)
	defer os.Remove(extractedPath)

	// Step 5: Swap binary
	log.Info("[Xray Updater] swapping binary", "dest", u.config.BinaryPath, "src", extractedPath)
	backupPath, err := u.swapper.SwapAtomic(u.config.BinaryPath, extractedPath)
	if err != nil {
		return result, &ErrUpdateFailed{Stage: "swap", Cause: err}
	}
	log.Info("[Xray Updater] binary swapped successfully", "backup", backupPath)

	// Step 6: Restart based on policy (if running)
	restarted := false
	if req.RestartPolicy != container.RestartPolicyNever && u.processCtrl != nil {
		if u.processCtrl.IsRunning() {
			if err := u.processCtrl.Stop(); err != nil {
				// Stop failed, try to rollback
				u.swapper.Rollback(u.config.BinaryPath, backupPath)
				return result, &ErrUpdateFailed{Stage: "restart", Cause: err, Wrapped: ErrRestartFailed}
			}
		}
		if err := u.processCtrl.Start(); err != nil {
			// Start failed, rollback
			u.swapper.Rollback(u.config.BinaryPath, backupPath)
			return result, &ErrUpdateFailed{Stage: "restart", Cause: err, Wrapped: ErrRestartFailed}
		}
		restarted = true
	}

	result.Restarted = restarted
	return result, nil
}

// findAsset finds an asset URL by name.
func (u *Updater) findAsset(release *tools.GitHubRelease, name string) string {
	for _, a := range release.Assets {
		if a.Name == name {
			return a.URL
		}
	}
	return ""
}

// extractBinary extracts the binary from zip file.
func (u *Updater) extractBinary(zipPath string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	// Find the binary in the zip
	var binaryFile *zip.File
	for _, f := range r.File {
		if f.Name == u.config.BinaryName || f.Name == "xray" ||
			f.Name == "xray.exe" || (len(f.Name) > 4 && f.Name[len(f.Name)-4:] == ".exe") {
			binaryFile = f
			break
		}
	}

	if binaryFile == nil {
		return "", errors.New("binary not found in zip")
	}

	// Create temp file for extracted binary
	tmpFile, err := os.CreateTemp(u.config.DownloadDir, "xray-extracted-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	rc, err := binaryFile.Open()
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to open zip entry: %w", err)
	}

	_, err = io.Copy(tmpFile, rc)
	rc.Close()
	tmpFile.Close()

	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to extract binary: %w", err)
	}

	// Make executable
	os.Chmod(tmpPath, 0755)

	return tmpPath, nil
}

// Rollback reverses a failed update.
func (u *Updater) Rollback(binaryPath, backupPath string) error {
	return u.swapper.Rollback(binaryPath, backupPath)
}

// SetProcessController sets the process controller for restart handling.
func (u *Updater) SetProcessController(pc ProcessController) {
	u.processCtrl = pc
}

// ErrRestartFailed is a sentinel error for restart failures.
var ErrRestartFailed = errors.New("restart failed")
