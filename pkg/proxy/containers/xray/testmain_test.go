// Package xray provides Xray container implementation.
package xray

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// xrayGRPCAddr is the gRPC address of the xray process started by TestMain.
// Integration tests that require a running xray should use this address via
// newIntegrationExecutor(t) which also calls t.Skip if xray is unavailable.
var xrayGRPCAddr string

// xrayTestProcess is the background xray process started by TestMain.
var xrayTestProcess *exec.Cmd

// TestMain starts a shared xray process for integration tests and cleans up after.
// If xray binary is not found, xrayGRPCAddr is left empty and integration tests skip.
func TestMain(m *testing.M) {
	xrayBin := findXrayBinary()
	if xrayBin != "" {
		addr, proc := startTestXray(xrayBin)
		xrayGRPCAddr = addr
		xrayTestProcess = proc
	}

	code := m.Run()

	if xrayTestProcess != nil {
		_ = xrayTestProcess.Process.Kill()
	}

	os.Exit(code)
}

// findXrayBinary returns the path of a working xray binary, or "" if not found.
func findXrayBinary() string {
	candidates := []string{
		"/nonexistent/xray", // present in this test environment
		"/usr/bin/xray",
		"/usr/local/bin/xray",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			if err := exec.Command(p, "version").Run(); err == nil {
				return p
			}
		}
	}
	return ""
}

// startTestXray starts an xray process on a fixed test gRPC port and returns
// the gRPC address and the running process. Returns ("", nil) on failure.
func startTestXray(bin string) (string, *exec.Cmd) {
	const grpcPort = 62779 // dedicated test port, distinct from default 62789

	config := map[string]interface{}{
		"log": map[string]interface{}{"loglevel": "none"},
		"api": map[string]interface{}{
			"tag":      "api",
			"services": []string{"HandlerService", "StatsService", "LoggerService"},
		},
		"inbounds": []map[string]interface{}{
			{
				"tag":      "api",
				"port":     grpcPort,
				"listen":   "127.0.0.1",
				"protocol": "dokodemo-door",
				"settings": map[string]interface{}{"address": "127.0.0.1"},
			},
		},
		"outbounds": []map[string]interface{}{
			{"protocol": "freedom"},
			{"protocol": "blackhole", "tag": "blocked"},
		},
		"routing": map[string]interface{}{
			"rules": []map[string]interface{}{
				{"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api"},
			},
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		return "", nil
	}

	f, err := os.CreateTemp("", "xray-test-*.json")
	if err != nil {
		return "", nil
	}
	configPath := f.Name()
	defer os.Remove(configPath)

	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", nil
	}
	f.Close()

	cmd := exec.Command(bin, "run", "-c", configPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return "", nil
	}

	// Wait for xray to start up: poll until gRPC is reachable.
	// "connection refused" means not up yet; any other response means xray is ready.
	addr := "127.0.0.1:62779"
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		api := NewXrayAPI(addr)
		err := api.RemoveInbound("__probe__")
		if err == nil {
			break // unexpectedly succeeded (fine)
		}
		errStr := err.Error()
		if !isConnRefused(errStr) {
			break // got a real gRPC response (e.g. "inbound not found") — xray is up
		}
		// still connection refused — keep waiting
	}

	return addr, cmd
}

// generateTempSelfSignedCert generates a self-signed cert and key in t.TempDir().
// Returns (certFile, keyFile) paths. Skips on generation failure.
func generateTempSelfSignedCert(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Skipf("failed to generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.com"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Skipf("failed to create cert: %v", err)
	}

	certPath := dir + "/cert.pem"
	keyPath := dir + "/key.pem"

	cf, _ := os.Create(certPath)
	pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	cf.Close()

	kf, _ := os.Create(keyPath)
	keyDER, _ := x509.MarshalECPrivateKey(priv)
	pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	kf.Close()

	return certPath, keyPath
}

// isConnRefused returns true if the error represents a TCP connection refusal.
func isConnRefused(errMsg string) bool {
	return strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "no connection could be made")
}

// newIntegrationExecutor creates an Executor pointing at the shared xray gRPC instance.
// Skips the test if xray is not running (xrayGRPCAddr is empty).
func newIntegrationExecutor(t *testing.T) *Executor {
	t.Helper()
	return newIntegrationExecutorWith(t, nil)
}

// newIntegrationExecutorWith creates an Executor with optional config overrides.
func newIntegrationExecutorWith(t *testing.T, cfgFn func(*ExecutorConfig)) *Executor {
	t.Helper()
	if xrayGRPCAddr == "" {
		t.Skip("xray binary not available; skipping integration test")
	}

	xrayBin := findXrayBinary()
	if xrayBin == "" {
		t.Skip("xray binary not available")
	}

	cfg := ExecutorConfig{
		BinaryPath:     xrayBin,
		GRPCAPIAddress: xrayGRPCAddr,
	}
	if cfgFn != nil {
		cfgFn(&cfg)
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("newIntegrationExecutorWith: %v", err)
	}
	return exec
}
