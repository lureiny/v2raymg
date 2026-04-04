package appconfig

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"gopkg.in/yaml.v3"
)

// legacyConfig represents the old v2raymg configuration format.
// Used for detection and migration only.
type legacyConfig struct {
	Cert struct {
		DNSProvider string `yaml:"dns_provider"`
		Email       string `yaml:"email"`
		Secrets     map[string]string `yaml:"secrets"`
	} `yaml:"cert"`
	Cluster struct {
		CenterNode struct {
			Host string `yaml:"host"`
			Port int    `yaml:"port"`
		} `yaml:"center_node"`
		Name  string `yaml:"name"`
		Token string `yaml:"token"`
	} `yaml:"cluster"`
	Proxy struct {
		Host                  string `yaml:"host"`
		Port                  int    `yaml:"port"`
		Version               string `yaml:"version"`
		XrayOrV2rayConfigFile string `yaml:"xray_or_v2ray_config_file"`
		HysteriaConfigFile    string `yaml:"hysteria_config_file"`
	} `yaml:"proxy"`
	Server struct {
		Http struct {
			Port               int    `yaml:"port"`
			SupportPrometheus  bool   `yaml:"support_prometheus"`
			Token              string `yaml:"token"`
		} `yaml:"http"`
		IcmpPingCheck bool   `yaml:"icmp_ping_check"`
		Listen        string `yaml:"listen"`
		Name          string `yaml:"name"`
		PingCheck     bool   `yaml:"ping_check"`
		Rpc           struct {
			OnlyGateway bool `yaml:"only_gateway"`
			Port        int  `yaml:"port"`
		} `yaml:"rpc"`
		Type string `yaml:"type"`
	} `yaml:"server"`
}

// isLegacyConfig returns true if the raw YAML looks like the old config format.
// Detection heuristic: has "server" and "proxy" top-level keys but no "node_type".
func isLegacyConfig(raw map[string]interface{}) bool {
	_, hasServer := raw["server"]
	_, hasProxy := raw["proxy"]
	_, hasNodeType := raw["node_type"]
	return hasServer && hasProxy && !hasNodeType
}

// migrateLegacyConfig converts a legacyConfig to the current AppConfig.
func migrateLegacyConfig(old *legacyConfig) *AppConfig {
	cfg := defaultAppConfig()

	// node_type
	cfg.NodeType = old.Server.Type
	if cfg.NodeType == "" {
		cfg.NodeType = "end"
	}

	// store — keep default path
	cfg.Store.DSN = "./v2raymg.db"

	// cert_mgmt
	cfg.CertMgmt.Email = old.Cert.Email
	cfg.CertMgmt.Path = "./certs"
	cfg.CertMgmt.Challenge.Type = "dns"
	cfg.CertMgmt.Challenge.DNS.ProviderName = old.Cert.DNSProvider
	cfg.CertMgmt.Challenge.DNS.Credentials = old.Cert.Secrets

	// containers — xray only (hysteria not yet supported in new framework)
	xrayCfg := map[string]any{
		"binary_path": "/usr/local/bin/xray",
	}
	if old.Proxy.XrayOrV2rayConfigFile != "" {
		xrayCfg["config_file"] = old.Proxy.XrayOrV2rayConfigFile
	}
	cfg.Containers = container.ContainerMgrConfig{
		Containers: []container.ContainerEntry{
			{
				Type:    contracts.ContainerXray,
				Enabled: true,
				Config:  xrayCfg,
			},
		},
	}

	// end_node
	cfg.EndNode.Listen = old.Server.Listen
	if cfg.EndNode.Listen == "" {
		cfg.EndNode.Listen = "0.0.0.0"
	}
	cfg.EndNode.Name = old.Server.Name
	cfg.EndNode.ProxyHost = old.Proxy.Host
	cfg.EndNode.RpcPort = old.Server.Rpc.Port
	cfg.EndNode.HttpPort = old.Server.Http.Port
	cfg.EndNode.HttpToken = old.Server.Http.Token
	cfg.EndNode.Ping.EnableICMPPing = old.Server.IcmpPingCheck
	cfg.EndNode.EnablePrometheus = old.Server.Http.SupportPrometheus
	cfg.EndNode.OnlyGateway = old.Server.Rpc.OnlyGateway

	// cluster
	cfg.EndNode.Cluster.Name = old.Cluster.Name
	cfg.EndNode.Cluster.Token = old.Cluster.Token
	cfg.EndNode.Cluster.CenterNodeHost = old.Cluster.CenterNode.Host
	cfg.EndNode.Cluster.CenterNodePort = old.Cluster.CenterNode.Port

	return cfg
}

// defaultAppConfig returns an AppConfig pre-populated with sensible defaults.
// Fields explicitly set in the config file will override these values after unmarshalling.
func defaultAppConfig() *AppConfig {
	cfg := &AppConfig{}
	cfg.Store.DSN = "./v2raymg.db"
	cfg.Forward.MinPort = 10000
	cfg.Forward.MaxPort = 60000
	cfg.EndNode.Ping.EnableICMPPing = true
	cfg.EndNode.Ping.ICMPPingInterval = 5
	cfg.EndNode.Ping.ICMPPingTimeout = 1
	cfg.EndNode.Ping.EnableTCPPing = true
	cfg.EndNode.Ping.TCPPingInterval = 5
	cfg.EndNode.Ping.TCPPingTimeout = 1
	cfg.EndNode.JWTExpireHours = 24
	return cfg
}

// LoadFromFile reads a YAML or JSON configuration file and returns an AppConfig.
// Automatically detects legacy config format and migrates it transparently.
// The decoder is selected based on the file extension (.yaml/.yml → YAML, .json → JSON).
// Fields not present in the file retain their default values from defaultAppConfig.
func LoadFromFile(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("appconfig: read file %q: %w", path, err)
	}

	ext := strings.ToLower(filepath.Ext(path))

	// For YAML, detect legacy format first
	if ext == ".yaml" || ext == ".yml" {
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err == nil && isLegacyConfig(raw) {
			// Parse as legacy and migrate
			var old legacyConfig
			if err := yaml.Unmarshal(data, &old); err != nil {
				return nil, fmt.Errorf("appconfig: decode legacy YAML %q: %w", path, err)
			}
			cfg := migrateLegacyConfig(&old)
			fmt.Fprintf(os.Stderr, "[appconfig] detected legacy config format, migrating automatically\n")
			return cfg, nil
		}
	}

	cfg := defaultAppConfig()

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("appconfig: decode YAML %q: %w", path, err)
		}
	case ".json":
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("appconfig: decode JSON %q: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("appconfig: unsupported file extension %q (want .yaml, .yml, or .json)", ext)
	}

	// Set default NodeSources if not configured
	if len(cfg.EndNode.Ping.NodeSources) == 0 {
		cfg.EndNode.Ping.NodeSources = []PingNodeSource{
			{Type: "file", Source: "./config/ping_nodes.yaml"},
		}
	}

	// Auto-generate jwt_secret when not configured.
	// The secret is only used to sign in-memory session tokens; each restart
	// with a new secret simply invalidates existing sessions (users re-login).
	// This allows old configs to upgrade without modification.
	if !strings.EqualFold(cfg.NodeType, "center") && cfg.EndNode.JWTSecret == "" {
		secret, err := generateRandomSecret(32)
		if err != nil {
			return nil, fmt.Errorf("appconfig: generate jwt_secret: %w", err)
		}
		cfg.EndNode.JWTSecret = secret
		fmt.Fprintf(os.Stderr, "[appconfig] end_node.jwt_secret not set — using a random secret (sessions will be invalidated on restart)\n")
	}

	return cfg, nil
}

// generateRandomSecret returns a URL-safe base64-encoded random string of n bytes.
func generateRandomSecret(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// SaveToFile writes an AppConfig to a YAML file, overwriting any existing content.
func SaveToFile(cfg *AppConfig, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("appconfig: marshal YAML: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("appconfig: write file %q: %w", path, err)
	}
	return nil
}

// LoadFromFileWithValidate loads and validates a configuration file.
// It calls LoadFromFile (which applies defaults) followed by Validate.
func LoadFromFileWithValidate(path string) (*AppConfig, error) {
	cfg, err := LoadFromFile(path)
	if err != nil {
		return nil, err
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks that cfg contains a consistent, usable configuration.
// It returns a descriptive error for the first violation found.
func Validate(cfg *AppConfig) error {
	if cfg.Store.DSN == "" {
		return fmt.Errorf("appconfig: store.dsn must not be empty")
	}

	if cfg.Forward.MinPort >= cfg.Forward.MaxPort {
		return fmt.Errorf("appconfig: forward.min_port (%d) must be less than forward.max_port (%d)",
			cfg.Forward.MinPort, cfg.Forward.MaxPort)
	}

	if cfg.CertMgmt.Email != "" && !strings.Contains(cfg.CertMgmt.Email, "@") {
		return fmt.Errorf("appconfig: cert_mgmt.email %q is not a valid email address", cfg.CertMgmt.Email)
	}

	validSourceTypes := map[string]bool{"file": true, "remote": true}
	for i, ns := range cfg.EndNode.Ping.NodeSources {
		if !validSourceTypes[ns.Type] {
			return fmt.Errorf(
				"appconfig: ping.node_sources[%d].type %q is invalid; valid values are: \"file\", \"remote\"",
				i, ns.Type,
			)
		}
	}

	enabledCount := 0
	for _, c := range cfg.Containers.Containers {
		if c.Enabled {
			enabledCount++
		}
	}
	if enabledCount == 0 {
		return fmt.Errorf("appconfig: at least one container entry must be enabled")
	}

	return nil
}
