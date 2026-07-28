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
  renew_before_hours: 12
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
	assert.Equal(t, 12, cfg.CertMgmt.RenewBeforeHours)
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
			"renew_before_hours": float64(12),
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
	assert.Equal(t, 12, cfg.CertMgmt.RenewBeforeHours)
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
		name    string
		entries []container.ContainerEntry
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
	cfg.EndNode.JWTSecret = "a-sufficiently-long-secret"

	require.NoError(t, Validate(cfg))
}

func TestValidate_EmptyEmailAllowed(t *testing.T) {
	cfg := &AppConfig{}
	cfg.Store.DSN = "/tmp/test.db"
	cfg.Forward.MinPort = 10000
	cfg.Forward.MaxPort = 60000
	// Email left empty — should be fine
	cfg.Containers.Containers = makeContainers(1)
	cfg.EndNode.JWTSecret = "a-sufficiently-long-secret"

	require.NoError(t, Validate(cfg))
}

func TestValidate_JWTSecretEmpty_NowAllowed(t *testing.T) {
	// jwt_secret is no longer validated — it is auto-generated by LoadFromFile.
	// Validate() must not reject a config with an empty jwt_secret.
	cfg := &AppConfig{}
	cfg.NodeType = "end"
	cfg.Store.DSN = "/tmp/test.db"
	cfg.Forward.MinPort = 10000
	cfg.Forward.MaxPort = 60000
	cfg.Containers.Containers = makeContainers(1)
	// JWTSecret intentionally left empty

	require.NoError(t, Validate(cfg))
}

func TestValidate_JWTSecretTooShort_NowAllowed(t *testing.T) {
	// jwt_secret is no longer validated — it is auto-generated by LoadFromFile.
	// Validate() must not reject a config with a short jwt_secret.
	cfg := &AppConfig{}
	cfg.NodeType = "end"
	cfg.Store.DSN = "/tmp/test.db"
	cfg.Forward.MinPort = 10000
	cfg.Forward.MaxPort = 60000
	cfg.Containers.Containers = makeContainers(1)
	cfg.EndNode.JWTSecret = "short"

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
end_node:
  jwt_secret: "a-sufficiently-long-secret-value"
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

// TestLoadFromFile_LegacyMigration_FillsJWTAndNodeSources covers finding #1:
// the legacy-migration path must still apply the shared runtime defaults
// (jwt_secret + ping NodeSources) that a normal load gets — before the fix it
// returned early and left them empty, breaking /login.
func TestLoadFromFile_LegacyMigration_FillsJWTAndNodeSources(t *testing.T) {
	content := "server:\n" +
		"  type: end\n" +
		"  name: node-a\n" +
		"  listen: 0.0.0.0\n" +
		"proxy:\n" +
		"  host: example.com\n" +
		"  port: 443\n" +
		"cluster:\n" +
		"  name: c1\n" +
		"  token: t1\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.EndNode.JWTSecret, "migrated end node must get an auto jwt_secret")
	assert.NotEmpty(t, cfg.EndNode.Ping.NodeSources, "migrated config must get default NodeSources")
}

// TestLoadFromFile_StaticNodeNameIsAcceptedAndIgnored is the upgrade guard for
// dropping the seed name.
//
// static_nodes entries used to carry a `name:`. The field is gone from the
// struct — a seed says WHERE to dial and nothing else, since the peer reports
// its own name in the first response and a label typed here could only ever be
// an unverified guess presented as fact. Decoding is lenient, so an existing
// config keeps loading untouched and the leftover key is silently dropped.
// If anyone ever switches this loader to strict decoding, this test is what
// tells them they just broke every deployed config.
func TestLoadFromFile_StaticNodeNameIsAcceptedAndIgnored(t *testing.T) {
	content := `
store:
  dsn: /var/lib/v2raymg/data.db
end_node:
  name: node-1
  proxy_host: 10.0.0.9
  rpc_port: 9090
  cluster:
    token: cluster-token-abcdef01
    static_nodes:
      - name: a-label-from-an-older-config
        host: 10.0.0.1
        port: 9090
      - host: 10.0.0.2
        port: 9090
`
	path := filepath.Join(t.TempDir(), "conf.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err, "a config still carrying static_nodes[].name must keep loading")

	seeds := cfg.EndNode.Cluster.StaticNodes
	require.Len(t, seeds, 2)
	assert.Equal(t, "10.0.0.1", seeds[0].Host)
	assert.Equal(t, int32(9090), seeds[0].Port)
	assert.Equal(t, "10.0.0.2", seeds[1].Host)
	assert.Equal(t, int32(9090), seeds[1].Port)
}
