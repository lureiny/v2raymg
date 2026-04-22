package mihomo

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/tools"
)

func TestFindMihomoV1AssetURL(t *testing.T) {
	cases := []struct {
		name   string
		assets []tools.GitHubAsset
		want   string
	}{
		{
			name: "picks v1 alpha gz",
			assets: []tools.GitHubAsset{
				{Name: "mihomo-linux-amd64-v1-alpha-5a5e312.gz", URL: "https://example/v1.gz"},
			},
			want: "https://example/v1.gz",
		},
		{
			name: "rejects v2/v3/compatible variants",
			assets: []tools.GitHubAsset{
				{Name: "mihomo-linux-amd64-alpha-5a5e312.gz", URL: "https://example/default.gz"},
				{Name: "mihomo-linux-amd64-v2-alpha-5a5e312.gz", URL: "https://example/v2.gz"},
				{Name: "mihomo-linux-amd64-v3-alpha-5a5e312.gz", URL: "https://example/v3.gz"},
				{Name: "mihomo-linux-amd64-compatible-alpha-5a5e312.gz", URL: "https://example/compat.gz"},
			},
			want: "",
		},
		{
			name: "rejects go120/go123 variants",
			assets: []tools.GitHubAsset{
				{Name: "mihomo-linux-amd64-v1-go120-alpha-5a5e312.gz", URL: "https://example/go120.gz"},
				{Name: "mihomo-linux-amd64-v1-go123-alpha-5a5e312.gz", URL: "https://example/go123.gz"},
			},
			want: "",
		},
		{
			name: "rejects deb/rpm/pkg.tar.zst",
			assets: []tools.GitHubAsset{
				{Name: "mihomo-linux-amd64-v1-alpha-5a5e312.deb", URL: "https://example/v1.deb"},
				{Name: "mihomo-linux-amd64-v1-alpha-5a5e312.rpm", URL: "https://example/v1.rpm"},
				{Name: "mihomo-linux-amd64-v1-alpha-5a5e312.pkg.tar.zst", URL: "https://example/v1.zst"},
			},
			want: "",
		},
		{
			name: "picks v1 gz when mixed with others",
			assets: []tools.GitHubAsset{
				{Name: "mihomo-linux-amd64-v2-alpha-5a5e312.gz", URL: "https://example/v2.gz"},
				{Name: "mihomo-linux-amd64-v1-alpha-5a5e312.gz", URL: "https://example/v1.gz"},
				{Name: "mihomo-linux-amd64-v3-alpha-5a5e312.gz", URL: "https://example/v3.gz"},
			},
			want: "https://example/v1.gz",
		},
		{
			name:   "empty assets",
			assets: nil,
			want:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findMihomoV1AssetURL(&tools.GitHubRelease{Assets: tc.assets})
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDownloadMihomoWith_Success(t *testing.T) {
	// Gzipped payload that will land on disk after extraction.
	payload := []byte("#!/bin/sh\necho mihomo-fake\n")
	var gzBuf bytes.Buffer
	gzw := gzip.NewWriter(&gzBuf)
	if _, err := gzw.Write(payload); err != nil {
		t.Fatalf("write gz: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close gz: %v", err)
	}
	gzBytes := gzBuf.Bytes()

	var assetURL string
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/MetaCubeX/mihomo/releases/tags/Prerelease-Alpha", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{
			"tag_name": "Prerelease-Alpha",
			"assets": []map[string]any{
				{"name": "mihomo-linux-amd64-v2-alpha-5a5e312.gz", "browser_download_url": server.URL + "/download/v2.gz"},
				{"name": "mihomo-linux-amd64-v1-alpha-5a5e312.gz", "browser_download_url": assetURL},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("/download/v1.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gzBytes)
	})
	server = httptest.NewServer(mux)
	defer server.Close()
	assetURL = server.URL + "/download/v1.gz"

	gh := tools.NewGitHubReleaseClient()
	gh.BaseURL = server.URL

	dest := filepath.Join(t.TempDir(), "mihomo")
	if err := downloadMihomoWith(context.Background(), gh, "Prerelease-Alpha", dest); err != nil {
		t.Fatalf("downloadMihomoWith: %v", err)
	}

	// Verify content
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("content mismatch:\n got: %q\nwant: %q", got, payload)
	}

	// Verify chmod
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("dest %q is not executable, mode=%v", dest, info.Mode())
	}
}

func TestDownloadMihomoWith_NoMatchingAsset(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/MetaCubeX/mihomo/releases/tags/Prerelease-Alpha", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "Prerelease-Alpha",
			"assets": []map[string]any{
				{"name": "mihomo-linux-amd64-v2-alpha-5a5e312.gz", "browser_download_url": "http://ignored"},
			},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	gh := tools.NewGitHubReleaseClient()
	gh.BaseURL = server.URL

	dest := filepath.Join(t.TempDir(), "mihomo")
	err := downloadMihomoWith(context.Background(), gh, "Prerelease-Alpha", dest)
	if err == nil {
		t.Fatal("expected error when no v1 asset is present")
	}
	if !strings.Contains(err.Error(), "no asset matching") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownloadMihomoWith_ReleaseNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/MetaCubeX/mihomo/releases/tags/bogus", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	gh := tools.NewGitHubReleaseClient()
	gh.BaseURL = server.URL

	dest := filepath.Join(t.TempDir(), "mihomo")
	err := downloadMihomoWith(context.Background(), gh, "bogus", dest)
	if err == nil {
		t.Fatal("expected error when release tag is missing")
	}
}
