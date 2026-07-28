// Package xray provides Xray container implementation.
package xray

import (
	"strings"
	"sync"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

func TestNewExecutor_Defaults(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec == nil {
		t.Fatal("executor should not be nil")
	}

	// Check default values
	if exec.config.ContainerType != contracts.ContainerXray {
		t.Errorf("ContainerType = %v, want %v", exec.config.ContainerType, contracts.ContainerXray)
	}

	// NewExecutor picks a free port dynamically; verify format only
	if !strings.HasPrefix(exec.config.GRPCAPIAddress, "127.0.0.1:") {
		t.Errorf("GRPCAPIAddress = %v, want 127.0.0.1:<port>", exec.config.GRPCAPIAddress)
	}
}

func TestNewExecutor_BinaryPathRequired(t *testing.T) {
	cfg := ExecutorConfig{
		ConfigFilePath: "/etc/xray/config.json",
	}

	_, err := NewExecutor(cfg)
	if err == nil {
		t.Fatal("expected error for empty binary path")
	}
}

func TestNewExecutor_WithUpdater(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
		UpdateConfig: UpdaterConfig{
			BinaryPath: "/usr/bin/xray",
			Owner:      "XTLS",
			Repo:       "Xray-core",
		},
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.updater == nil {
		t.Error("updater should be initialized when UpdateConfig is provided")
	}
}

func TestExecutor_Type(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
		ContainerType:  contracts.ContainerXray,
	}

	exec, _ := NewExecutor(cfg)

	if exec.Type() != contracts.ContainerXray {
		t.Errorf("Type() = %v, want %v", exec.Type(), contracts.ContainerXray)
	}
}

func TestExecutor_ConfigFile(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, _ := NewExecutor(cfg)

	if exec.ConfigFile() != "/etc/xray/config.json" {
		t.Errorf("ConfigFile() = %v, want %v", exec.ConfigFile(), "/etc/xray/config.json")
	}
}

func TestExecutor_IsRunning_Initial(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, _ := NewExecutor(cfg)

	if exec.IsRunning() {
		t.Error("IsRunning() should return false initially")
	}
}

func TestExecutor_GRPCAddress(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
		GRPCAPIAddress: "192.168.1.1:62789",
	}

	exec, _ := NewExecutor(cfg)

	if exec.config.GRPCAPIAddress != "192.168.1.1:62789" {
		t.Errorf("GRPCAPIAddress = %v, want %v", exec.config.GRPCAPIAddress, "192.168.1.1:62789")
	}
}

func TestExecutor_DefaultGRPCAddress(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, _ := NewExecutor(cfg)

	// NewExecutor picks a free port dynamically; verify format only
	if !strings.HasPrefix(exec.config.GRPCAPIAddress, "127.0.0.1:") {
		t.Errorf("GRPCAPIAddress = %v, want 127.0.0.1:<port>", exec.config.GRPCAPIAddress)
	}
}

// mockForwardManagerForTest is a mock implementation of forward.ForwardManager for testing.
type mockForwardManagerForTest struct {
	mu    sync.Mutex
	rules map[string]*forward.ForwardRule
}

func newMockForwardManagerForTest() *mockForwardManagerForTest {
	return &mockForwardManagerForTest{
		rules: make(map[string]*forward.ForwardRule),
	}
}

func (m *mockForwardManagerForTest) AddRule(rule forward.ForwardRule) (*forward.ForwardRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Auto-allocate port if not specified
	if rule.ListenPort == 0 {
		rule.ListenPort = uint32(10000 + len(m.rules) + 1)
	}
	m.rules[rule.Username] = &rule
	return &rule, nil
}

func (m *mockForwardManagerForTest) RemoveRule(ruleKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rules, ruleKey)
	return nil
}

func (m *mockForwardManagerForTest) RemoveRulesByUser(userEmail string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rules, userEmail)
	return nil
}

func (m *mockForwardManagerForTest) RemoveRulesByInbound(inboundTag string) error {
	// Mock implementation - delete all rules matching inbound tag
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.rules {
		// Note: In real implementation we'd match by InboundTag
		// For mock, we just return nil
		_ = key
	}
	return nil
}

func (m *mockForwardManagerForTest) GetRule(ruleKey string) *forward.ForwardRule {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rules[ruleKey]
}

func (m *mockForwardManagerForTest) GetRulesByUser(userEmail string) []*forward.ForwardRule {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*forward.ForwardRule
	for _, rule := range m.rules {
		if rule.Username == userEmail {
			result = append(result, rule)
		}
	}
	return result
}

func (m *mockForwardManagerForTest) GetAllRules() []*forward.ForwardRule {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*forward.ForwardRule, 0, len(m.rules))
	for _, rule := range m.rules {
		result = append(result, rule)
	}
	return result
}

func (m *mockForwardManagerForTest) GetTraffic(ruleKey string, reset bool) (*forward.TrafficSnapshot, error) {
	return nil, nil
}

func (m *mockForwardManagerForTest) GetAllTraffic(reset bool) *forward.ForwardManagerStats {
	return nil
}

func (m *mockForwardManagerForTest) GetAllTrafficRecords(reset bool) []forward.ForwardTrafficRecord {
	return nil
}

func (m *mockForwardManagerForTest) QueryTrafficStats(query forward.TrafficQuery) forward.TrafficQueryResult {
	return forward.TrafficQueryResult{}
}

func (m *mockForwardManagerForTest) UpdateRateLimit(ruleKey string, uploadBPS, downloadBPS int64) error {
	return nil
}

func (m *mockForwardManagerForTest) SetUserBandwidthLimit(username string, kind forward.BandwidthLimitKind, bytesPerSec int64) error {
	return nil
}

func (m *mockForwardManagerForTest) GetUserBandwidthLimit(username string, kind forward.BandwidthLimitKind) (int64, bool) {
	return 0, false
}

func (m *mockForwardManagerForTest) SetUserConnectionLimit(username string, limit int) error {
	return nil
}

func (m *mockForwardManagerForTest) GetUserConnectionLimit(username string) (int, bool) {
	return 0, false
}

func (m *mockForwardManagerForTest) SetUserClientLimitConfig(username string, config forward.ClientLimitConfig) error {
	return nil
}

func (m *mockForwardManagerForTest) GetUserClientLimitConfig(username string) (forward.ClientLimitConfig, bool) {
	return forward.ClientLimitConfig{}, false
}

func (m *mockForwardManagerForTest) AllocatePort() (uint32, error) {
	return 0, nil
}

func (m *mockForwardManagerForTest) AllocateSpecificPort(port uint32) error {
	return nil
}

func (m *mockForwardManagerForTest) ReleasePort(port uint32) {}

func (m *mockForwardManagerForTest) Close() error {
	return nil
}

func (m *mockForwardManagerForTest) DropUser(username string) bool {
	return false
}

// mockUserManagerGetter is a mock implementation of usermanager.UserManagerGetter for testing.
type mockUserManagerGetter struct {
	um *usermanager.UserManager
}

func (m *mockUserManagerGetter) GetUserManager() *usermanager.UserManager {
	return m.um
}

// mockForwardManagerGetterForTest is a mock implementation of usermanager.ForwardManagerGetter.
type mockForwardManagerGetterForTest struct {
	fm *mockForwardManagerForTest
}

func (m *mockForwardManagerGetterForTest) GetForwardManager() forward.ForwardManager {
	return m.fm
}

// TestExtractNativeExtra_Flow tests that flow is extracted from VLESS settings.
func TestExtractNativeExtra_Flow(t *testing.T) {
	// Test with flow present
	raw := map[string]interface{}{
		"settings": map[string]interface{}{
			"clients": []interface{}{
				map[string]interface{}{
					"id":   "test-uuid",
					"flow": "xtls-rprx-vision",
				},
			},
		},
	}
	extra := extractNativeExtra(raw, contracts.TransportTCP, contracts.SecurityNone)

	if flow, ok := extra["flow"].(string); !ok || flow != "xtls-rprx-vision" {
		t.Errorf("expected flow 'xtls-rprx-vision', got %v", extra["flow"])
	}

	// Test without flow (should not have flow key)
	rawNoFlow := map[string]interface{}{
		"settings": map[string]interface{}{
			"clients": []interface{}{
				map[string]interface{}{
					"id": "test-uuid",
					// No flow field
				},
			},
		},
	}
	extraNoFlow := extractNativeExtra(rawNoFlow, contracts.TransportTCP, contracts.SecurityNone)
	if _, ok := extraNoFlow["flow"]; ok {
		t.Errorf("expected no flow key, got %v", extraNoFlow["flow"])
	}
}

// TestExtractNativeExtra_RealityShortId tests that shortId is extracted from Reality settings.
func TestExtractNativeExtra_RealityShortId(t *testing.T) {
	// Generate a valid key pair for testing
	privateKey, _, err := GenerateRealityKeyPairWithPublic()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	raw := map[string]interface{}{
		"streamSettings": map[string]interface{}{
			"security": "reality",
			"realitySettings": map[string]interface{}{
				"serverNames": []interface{}{"www.microsoft.com"},
				"shortIds":    []interface{}{"0123456789abcdef"},
				"privateKey":  privateKey,
			},
		},
	}
	extra := extractNativeExtra(raw, contracts.TransportTCP, contracts.SecurityReality)

	if sids, ok := extra["reality_short_ids"].([]string); !ok || len(sids) != 1 || sids[0] != "0123456789abcdef" {
		t.Errorf("expected shortIds []string{\"0123456789abcdef\"}, got %v", extra["reality_short_ids"])
	}
	if pbk, ok := extra["reality_public_key"].(string); !ok || pbk == "" {
		t.Errorf("expected derived public key, got %v", extra["reality_public_key"])
	}
}
