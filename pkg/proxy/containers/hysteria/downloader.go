package hysteria

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lureiny/v2raymg/pkg/proxy/tools"
)

// downloadHysteria downloads the hysteria binary for linux-amd64.
// version must be the full GitHub release tag, e.g. "app/v2.7.1".
func downloadHysteria(version, destPath string) error {
	url := fmt.Sprintf("https://github.com/apernet/hysteria/releases/download/%s/hysteria-linux-amd64", version)

	tmpPath := destPath + ".tmp"
	defer os.Remove(tmpPath)

	if err := tools.NewDownloader().DownloadToFile(context.Background(), url, tmpPath); err != nil {
		return fmt.Errorf("hysteria: download %s: %w", url, err)
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("hysteria: chmod binary: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("hysteria: create directory: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("hysteria: move binary: %w", err)
	}

	return nil
}
