//go:build integration

package systemtest

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/containers/xray"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"golang.org/x/net/proxy"
)

// noopForwardManager is a no-op implementation for tests that don't need real forwarding.
type noopForwardManager struct{}

func (n *noopForwardManager) AddRule(rule forward.ForwardRule) (*forward.ForwardRule, error) {
	return &rule, nil
}
func (n *noopForwardManager) RemoveRule(string) error                      { return nil }
func (n *noopForwardManager) RemoveRulesByUser(string) error               { return nil }
func (n *noopForwardManager) RemoveRulesByInbound(string) error            { return nil }
func (n *noopForwardManager) GetRule(string) *forward.ForwardRule          { return nil }
func (n *noopForwardManager) GetRulesByUser(string) []*forward.ForwardRule { return nil }
func (n *noopForwardManager) GetAllRules() []*forward.ForwardRule          { return nil }
func (n *noopForwardManager) GetTraffic(string, bool) (*forward.TrafficSnapshot, error) {
	return &forward.TrafficSnapshot{}, nil
}
func (n *noopForwardManager) GetAllTraffic(bool) *forward.ForwardManagerStats { return nil }
func (n *noopForwardManager) GetAllTrafficRecords(bool) []forward.ForwardTrafficRecord {
	return nil
}
func (n *noopForwardManager) QueryTrafficStats(forward.TrafficQuery) forward.TrafficQueryResult {
	return forward.TrafficQueryResult{}
}
func (n *noopForwardManager) UpdateRateLimit(string, int64, int64) error { return nil }
func (n *noopForwardManager) SetUserBandwidthLimit(string, forward.BandwidthLimitKind, int64) error {
	return nil
}
func (n *noopForwardManager) GetUserBandwidthLimit(string, forward.BandwidthLimitKind) (int64, bool) {
	return 0, false
}
func (n *noopForwardManager) SetUserClientLimitConfig(string, forward.ClientLimitConfig) error {
	return nil
}
func (n *noopForwardManager) GetUserClientLimitConfig(string) (forward.ClientLimitConfig, bool) {
	return forward.ClientLimitConfig{}, false
}
func (n *noopForwardManager) DropUser(string) bool { return false }
func (n *noopForwardManager) AllocatePort() (uint32, error) {
	return 0, fmt.Errorf("noopForwardManager: AllocatePort is not supported in this test harness")
}
func (n *noopForwardManager) AllocateSpecificPort(uint32) error { return nil }
func (n *noopForwardManager) ReleasePort(uint32)                {}
func (n *noopForwardManager) Close() error                      { return nil }

// newTestUserManager creates a minimal UserManager for testing.
func newTestUserManager() *usermanager.UserManager {
	return usermanager.NewUserManager(&noopForwardManager{}, "test-node")
}

// freeTCPPort allocates a free TCP port by binding to :0 and immediately closing.
func freeTCPPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}

// waitPort polls until the given TCP address accepts connections or timeout.
func waitPort(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("port %s not ready after %s", addr, timeout)
}

// httpClientViaSocks5 creates an http.Client that dials through a SOCKS5 proxy.
func httpClientViaSocks5(socksAddr string) (*http.Client, error) {
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.Dial(network, addr)
	}
	return &http.Client{Transport: tr, Timeout: 15 * time.Second}, nil
}

// writeTempCert generates a self-signed TLS certificate and writes it to temp files.
// Returns the cert file path, key file path, and PEM strings.
func writeTempCert(dir string) (certPath, keyPath, certPEM, keyPEM string, err error) {
	certPEM, keyPEM, err = xray.GenerateSelfSignedCert("localhost")
	if err != nil {
		return "", "", "", "", fmt.Errorf("generate cert: %w", err)
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, []byte(certPEM), 0600); err != nil {
		return "", "", "", "", fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(keyPath, []byte(keyPEM), 0600); err != nil {
		return "", "", "", "", fmt.Errorf("write key: %w", err)
	}
	return certPath, keyPath, certPEM, keyPEM, nil
}

// checkNetwork verifies that the test can reach the internet.
func checkNetwork() error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://www.google.com")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
