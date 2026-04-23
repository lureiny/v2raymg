package mihomo

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/tools"
)

// mihomoV1AssetPrefix is the Alpha GOAMD64=v1 asset prefix — full shape
// "mihomo-linux-amd64-v1-alpha-<git-short-hash>.gz". The "-v1-" segment
// distinguishes from "-v2-/-v3-/-compatible-" variants and from the
// "-v1-go120-/-v1-go123-" toolchain builds (go12x sits between "v1" and
// "alpha", producing a different prefix).
//
// Kept in downloader.go alongside downloadMihomoWith for the existing asset
// matching tests. The production download path is updater.go, which
// reconstructs the same prefix inline via alphaAssetPrefix for both Alpha and
// stable releases; they're duplicated intentionally so each file reads
// standalone.
const mihomoV1AssetPrefix = "mihomo-linux-amd64-v1-alpha-"

// downloadMihomoWith is the testable Alpha-only download helper kept for the
// asset-matching tests in downloader_test.go. Production uses updater.go
// instead, which handles both Alpha and stable releases and adds SHA256
// verification. Callers inject the GitHub client so tests can redirect
// BaseURL to a local httptest server.
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
