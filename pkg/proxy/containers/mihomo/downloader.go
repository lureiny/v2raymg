package mihomo

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/tools"
)

// mihomo Alpha release asset prefix for the default Go toolchain, GOAMD64=v1
// build. Full name shape: "mihomo-linux-amd64-v1-alpha-<git-short-hash>.gz".
// The "-v1-" segment distinguishes from "-v2-/-v3-/-compatible-" variants and
// from the "-v1-go120-/-v1-go123-" toolchain variants (those have a different
// prefix because go12x sits between "v1" and "alpha").
const mihomoV1AssetPrefix = "mihomo-linux-amd64-v1-alpha-"

// downloadMihomo fetches the mihomo linux-amd64 v1 binary for the given release
// tag and writes it atomically to destPath (chmod 0755). It is the convenience
// wrapper used by the container at start-up.
//
// Only linux/amd64 is supported — the asset filter and binary are hard-coded
// for that platform. Other OS/arch combinations are rejected early so a
// developer running on macOS/arm64 gets a clear error instead of a downloaded
// binary that won't execute. Stage 9's updater will add OS/arch autodetection.
func downloadMihomo(ctx context.Context, tag, destPath string) error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("mihomo: auto-download only supports linux/amd64 (current runtime is %s/%s); pre-install the binary at %s", runtime.GOOS, runtime.GOARCH, destPath)
	}
	return downloadMihomoWith(ctx, tools.NewGitHubReleaseClient(), tag, destPath)
}

// downloadMihomoWith is the testable form: the caller provides the GitHub
// client so tests can redirect BaseURL to a local httptest server.
func downloadMihomoWith(ctx context.Context, gh *tools.GitHubReleaseClient, tag, destPath string) error {
	rel, err := gh.FetchRelease(ctx, "MetaCubeX", "mihomo", tag)
	if err != nil {
		return fmt.Errorf("mihomo: fetch release %s: %w", tag, err)
	}

	assetURL := findMihomoV1AssetURL(rel)
	if assetURL == "" {
		return fmt.Errorf("mihomo: no asset matching %q*.gz in release %s", mihomoV1AssetPrefix, tag)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("mihomo: create directory: %w", err)
	}

	gzPath := destPath + ".gz.tmp"
	defer os.Remove(gzPath)

	if err := tools.NewDownloader().DownloadToFile(ctx, assetURL, gzPath); err != nil {
		return fmt.Errorf("mihomo: download %s: %w", assetURL, err)
	}

	tmpPath := destPath + ".tmp"
	defer os.Remove(tmpPath)

	if err := gunzipFile(gzPath, tmpPath); err != nil {
		return fmt.Errorf("mihomo: gunzip: %w", err)
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("mihomo: chmod: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("mihomo: move binary: %w", err)
	}

	return nil
}

// findMihomoV1AssetURL returns the first asset whose name matches the v1
// alpha build pattern. See mihomoV1AssetPrefix for the exact shape and why
// this filter rejects the go120/go123/v2/v3/compatible variants.
func findMihomoV1AssetURL(rel *tools.GitHubRelease) string {
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, mihomoV1AssetPrefix) && strings.HasSuffix(a.Name, ".gz") {
			return a.URL
		}
	}
	return ""
}

func gunzipFile(src, dst string) error {
	r, err := os.Open(src)
	if err != nil {
		return err
	}
	defer r.Close()

	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gzr.Close()

	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer w.Close()

	_, err = io.Copy(w, gzr)
	return err
}
