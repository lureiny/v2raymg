// Package tools provides download utilities.
package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Downloader downloads files.
type Downloader struct {
	HTTPClient *http.Client
}

// NewDownloader creates a new downloader.
func NewDownloader() *Downloader {
	return &Downloader{HTTPClient: &http.Client{}}
}

// DownloadToFile downloads a URL to a file.
func (d *Downloader) DownloadToFile(ctx context.Context, url, output string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %d", resp.StatusCode)
	}

	out, err := os.Create(output)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
