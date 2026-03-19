package snell

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// archMap maps GOARCH to snell download archive architecture names.
var archMap = map[string]string{
	"amd64": "amd64",
	"386":   "i386",
	"arm64": "aarch64",
	"arm":   "armv7l",
}

// downloadSnellServer downloads and installs the snell-server binary.
func downloadSnellServer(version, binaryPath string) error {
	arch, ok := archMap[runtime.GOARCH]
	if !ok {
		return fmt.Errorf("snell: unsupported architecture: %s", runtime.GOARCH)
	}

	url := fmt.Sprintf("https://dl.nssurge.com/snell/snell-server-%s-linux-%s.zip", version, arch)

	// Download zip to temp file
	tmpFile, err := os.CreateTemp("", "snell-*.zip")
	if err != nil {
		return fmt.Errorf("snell: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	resp, err := http.Get(url)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("snell: download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("snell: download %s: HTTP %d", url, resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("snell: write temp file: %w", err)
	}
	tmpFile.Close()

	// Extract snell-server binary from zip
	zr, err := zip.OpenReader(tmpPath)
	if err != nil {
		return fmt.Errorf("snell: open zip: %w", err)
	}
	defer zr.Close()

	var binaryFile *zip.File
	for _, f := range zr.File {
		if filepath.Base(f.Name) == "snell-server" {
			binaryFile = f
			break
		}
	}
	if binaryFile == nil {
		return fmt.Errorf("snell: snell-server not found in zip")
	}

	rc, err := binaryFile.Open()
	if err != nil {
		return fmt.Errorf("snell: open zip entry: %w", err)
	}
	defer rc.Close()

	// Ensure target directory exists
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0755); err != nil {
		return fmt.Errorf("snell: create directory: %w", err)
	}

	// Write binary to target path
	outFile, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("snell: create binary file: %w", err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, rc); err != nil {
		return fmt.Errorf("snell: write binary: %w", err)
	}

	return nil
}
