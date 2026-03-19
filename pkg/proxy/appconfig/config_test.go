package appconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeContainers returns n enabled ContainerEntry values (type=xray).
func makeContainers(n int) []container.ContainerEntry {
	entries := make([]container.ContainerEntry, n)
	for i := range entries {
		entries[i] = container.ContainerEntry{
			Type:    contracts.ContainerXray,
			Enabled: true,
		}
	}
	return entries
}

func TestLoadFromFile_YAML(t *testing.T) {
	content := `
store:
  dsn: /var/lib/v2raymg/data.db
forward:
  min_port: 10000
  max_port: 60000
  use_random: true
cert_mgmt:
  email: admin@example.com
  path: /etc/certs
  renew_before_days: 30
containers:
  containers:
    - type: xray
      enabled: true
      config:
        binary: /usr/local/bin/xray
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "/var/lib/v2raymg/data.db", cfg.Store.DSN)
	assert.Equal(t, uint32(10000), cfg.Forward.MinPort)
	assert.Equal(t, uint32(60000), cfg.Forward.MaxPort)
	assert.True(t, cfg.Forward.UseRandom)
	assert.Equal(t, "admin@example.com", cfg.CertMgmt.Email)
	assert.Equal(t, "/etc/certs", cfg.CertMgmt.Path)
	assert.Equal(t, 30, cfg.CertMgmt.RenewBeforeDays)
	require.Len(t, cfg.Containers.Containers, 1)
	assert.Equal(t, contracts.ContainerXray, cfg.Containers.Containers[0].Type)
	assert.True(t, cfg.Containers.Containers[0].Enabled)
}

func TestLoadFromFile_YML(t *testing.T) {
	content := `
store:
  dsn: /tmp/data.db
forward:
  minport: 1000
  maxport: 2000
containers:
  containers:
    - type: xray
      enabled: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/data.db", cfg.Store.DSN)
	require.Len(t, cfg.Containers.Containers, 1)
	assert.True(t, cfg.Containers.Containers[0].Enabled)
}

func TestLoadFromFile_JSON(t *testing.T) {
	payload := map[string]interface{}{
		"store": map[string]interface{}{
			"dsn": "/var/lib/v2raymg/data.db",
		},
		"forward": map[string]interface{}{
			"min_port":   float64(10000),
			"max_port":   float64(60000),
			"use_random": true,
		},
		"cert_mgmt": map[string]interface{}{
			"email":              "admin@example.com",
			"path":               "/etc/certs",
			"renew_before_days":  float64(30),
		},
		// ContainerMgrConfig and ContainerEntry have json:"..." tags.
		"containers": map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{
					"type":    "xray",
					"enabled": true,
				},
			},
		},
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "/var/lib/v2raymg/data.db", cfg.Store.DSN)
	assert.Equal(t, uint32(10000), cfg.Forward.MinPort)
	assert.Equal(t, uint32(60000), cfg.Forward.MaxPort)
	assert.True(t, cfg.Forward.UseRandom)
	assert.Equal(t, "admin@example.com", cfg.CertMgmt.Email)
	assert.Equal(t, "/etc/certs", cfg.CertMgmt.Path)
	assert.Equal(t, 30, cfg.CertMgmt.RenewBeforeDays)
	require.Len(t, cfg.Containers.Containers, 1)
	assert.Equal(t, contracts.ContainerXray, cfg.Containers.Containers[0].Type)
	assert.True(t, cfg.Containers.Containers[0].Enabled)
}

func TestLoadFromFile_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

	_, err := LoadFromFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported file extension")
}

func TestLoadFromFile_ReadError(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/path/config.yaml")
	require.Error(t, err)
}

func TestValidate_DSNEmpty(t *testing.T) {
	cfg := &AppConfig{}
	cfg.Forward.MinPort = 1000
	cfg.Forward.MaxPort = 2000
	cfg.Containers.Containers = makeContainers(1)

	err := Validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dsn")
}

func TestValidate_MinPortGEMaxPort(t *testing.T) {
	tests := []struct {
		name string
		min  uint32
		max  uint32
	}{
		{"equal", 5000, 5000},
		{"min greater", 6000, 5000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &AppConfig{}
			cfg.Store.DSN = "/tmp/test.db"
			cfg.Forward.MinPort = tc.min
			cfg.Forward.MaxPort = tc.max
			cfg.Containers.Containers = makeContainers(1)

			err := Validate(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "min_port")
		})
	}
}

func TestValidate_NoEnabledContainer(t *testing.T) {
	tests := []struct {
		name     string
		entries  []container.ContainerEntry
	}{
		{
			name:    "empty list",
			entries: nil,
		},
		{
			name: "all disabled",
			entries: []container.ContainerEntry{
				{Type: contracts.ContainerXray, Enabled: false},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &AppConfig{}
			cfg.Store.DSN = "/tmp/test.db"
			cfg.Forward.MinPort = 1000
			cfg.Forward.MaxPort = 2000
			cfg.Containers.Containers = tc.entries

			err := Validate(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "enabled")
		})
	}
}

func TestValidate_InvalidEmail(t *testing.T) {
	cfg := &AppConfig{}
	cfg.Store.DSN = "/tmp/test.db"
	cfg.Forward.MinPort = 1000
	cfg.Forward.MaxPort = 2000
	cfg.CertMgmt.Email = "not-an-email"
	cfg.Containers.Containers = makeContainers(1)

	err := Validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email")
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &AppConfig{}
	cfg.Store.DSN = "/tmp/test.db"
	cfg.Forward.MinPort = 10000
	cfg.Forward.MaxPort = 60000
	cfg.CertMgmt.Email = "admin@example.com"
	cfg.Containers.Containers = makeContainers(1)

	require.NoError(t, Validate(cfg))
}

func TestValidate_EmptyEmailAllowed(t *testing.T) {
	cfg := &AppConfig{}
	cfg.Store.DSN = "/tmp/test.db"
	cfg.Forward.MinPort = 10000
	cfg.Forward.MaxPort = 60000
	// Email left empty — should be fine
	cfg.Containers.Containers = makeContainers(1)

	require.NoError(t, Validate(cfg))
}

func TestLoadFromFile_DefaultsApplied(t *testing.T) {
	// Minimal YAML — omit store, forward, and demo to verify defaults kick in.
	content := `
containers:
  containers:
    - type: xray
      enabled: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "./v2raymg.db", cfg.Store.DSN)
	assert.Equal(t, uint32(10000), cfg.Forward.MinPort)
	assert.Equal(t, uint32(60000), cfg.Forward.MaxPort)
}

func TestLoadFromFile_UserValuesOverrideDefaults(t *testing.T) {
	content := `
store:
  dsn: /custom/path.db
forward:
  min_port: 20000
  max_port: 50000
containers:
  containers:
    - type: xray
      enabled: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "/custom/path.db", cfg.Store.DSN)
	assert.Equal(t, uint32(20000), cfg.Forward.MinPort)
	assert.Equal(t, uint32(50000), cfg.Forward.MaxPort)
}

func TestLoadFromFileWithValidate_Valid(t *testing.T) {
	content := `
containers:
  containers:
    - type: xray
      enabled: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadFromFileWithValidate(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "./v2raymg.db", cfg.Store.DSN)
}

func TestLoadFromFileWithValidate_LoadError(t *testing.T) {
	_, err := LoadFromFileWithValidate("/nonexistent/config.yaml")
	require.Error(t, err)
}

func TestLoadFromFileWithValidate_ValidationError(t *testing.T) {
	// No enabled containers — Validate should reject this.
	content := `
containers:
  containers:
    - type: xray
      enabled: false
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	_, err := LoadFromFileWithValidate(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enabled")
}

// --- HttpListen tests ---

func TestLoadFromFile_HttpListen_Set(t *testing.T) {
	content := `
end_node:
  listen: "0.0.0.0"
  rpc_port: 62789
  http_listen: "127.0.0.1"
  http_port: 62790
containers:
  containers:
    - type: xray
      enabled: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", cfg.EndNode.Listen)
	assert.Equal(t, "127.0.0.1", cfg.EndNode.HttpListen)
	assert.Equal(t, 62789, cfg.EndNode.RpcPort)
	assert.Equal(t, 62790, cfg.EndNode.HttpPort)
}

func TestLoadFromFile_HttpListen_Empty(t *testing.T) {
	content := `
end_node:
  listen: "0.0.0.0"
  rpc_port: 62789
  http_port: 62790
containers:
  containers:
    - type: xray
      enabled: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)

	// http_listen not set → empty string (default 127.0.0.1 is applied in cmd/server.go)
	assert.Equal(t, "", cfg.EndNode.HttpListen)
	assert.Equal(t, "0.0.0.0", cfg.EndNode.Listen)
}

func TestLoadFromFile_HttpListen_BindAll(t *testing.T) {
	content := `
end_node:
  listen: "0.0.0.0"
  http_listen: "0.0.0.0"
  http_port: 62790
containers:
  containers:
    - type: xray
      enabled: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", cfg.EndNode.HttpListen)
}

func TestLoadFromFile_HttpListen_SpecificIP(t *testing.T) {
	content := `
end_node:
  listen: "0.0.0.0"
  http_listen: "10.0.0.1"
  http_port: 62790
containers:
  containers:
    - type: xray
      enabled: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "10.0.0.1", cfg.EndNode.HttpListen)
}

func TestLoadFromFile_HttpListen_JSON(t *testing.T) {
	payload := map[string]interface{}{
		"end_node": map[string]interface{}{
			"listen":      "0.0.0.0",
			"http_listen": "127.0.0.1",
			"http_port":   float64(62790),
		},
		"containers": map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{
					"type":    "xray",
					"enabled": true,
				},
			},
		},
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", cfg.EndNode.Listen)
	assert.Equal(t, "127.0.0.1", cfg.EndNode.HttpListen)
}
