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
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"gopkg.in/yaml.v3"
)

// legacyConfig represents the old v2raymg configuration format.
// Used for detection and migration only.
type legacyConfig struct {
	Cert struct {
		DNSProvider string            `yaml:"dns_provider"`
		Email       string            `yaml:"email"`
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
			Port              int    `yaml:"port"`
			SupportPrometheus bool   `yaml:"support_prometheus"`
			Token             string `yaml:"token"`
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

	// cluster. The legacy `name` and `center_node` keys are intentionally dropped:
	// the center node was removed in 2026-07 and the cluster name was a redundant
	// second membership factor. warnRemovedClusterKeys tells the operator.
	cfg.EndNode.Cluster.Token = old.Cluster.Token

	return cfg
}

// defaultAppConfig returns an AppConfig pre-populated with sensible defaults.
// Fields explicitly set in the config file will override these values after unmarshalling.
func defaultAppConfig() *AppConfig {
	cfg := &AppConfig{}
	cfg.Store.DSN = "./v2raymg.db"
	cfg.Forward.MinPort = 10000
	cfg.Forward.MaxPort = 60000
	cfg.Forward.ListenStack = forward.ListenStackDual
	cfg.EndNode.Ping.EnableICMPPing = true
	cfg.EndNode.Ping.ICMPPingInterval = 5
	cfg.EndNode.Ping.ICMPPingTimeout = 1
	cfg.EndNode.Ping.EnableTCPPing = true
	cfg.EndNode.Ping.TCPPingInterval = 5
	cfg.EndNode.Ping.TCPPingTimeout = 1
	cfg.EndNode.JWTExpireHours = 24
	cfg.EndNode.Cluster.NodeSumSync = true
	cfg.EndNode.Cluster.HeartbeatIntervalSec = 10
	cfg.ClusterUser = DefaultClusterUserConfig()
	cfg.Subscription.EnableUserInfoHeader = true
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

	// For YAML, decode a raw view once: it drives legacy detection AND the
	// removed-key warnings, which cannot use the struct because those fields no
	// longer exist. Warnings go to stderr because the logger is not configured
	// until after Validate, and they must be visible before any error they explain.
	if ext == ".yaml" || ext == ".yml" {
		var raw map[string]interface{}
		rawErr := yaml.Unmarshal(data, &raw)
		if rawErr == nil {
			for _, w := range WarnRemovedConfig(raw) {
				fmt.Fprintf(os.Stderr, "[appconfig] WARNING: %s\n", w)
			}
		}
		if rawErr == nil && isLegacyConfig(raw) {
			// Parse as legacy and migrate
			var old legacyConfig
			if err := yaml.Unmarshal(data, &old); err != nil {
				return nil, fmt.Errorf("appconfig: decode legacy YAML %q: %w", path, err)
			}
			cfg := migrateLegacyConfig(&old)
			fmt.Fprintf(os.Stderr, "[appconfig] detected legacy config format, migrating automatically\n")
			// Migrated configs must get the same runtime defaults as a normal
			// load — without this the legacy path skipped jwt_secret generation
			// (breaking /login) and the ping NodeSources default.
			if err := applyRuntimeDefaults(cfg); err != nil {
				return nil, err
			}
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

	if err := applyRuntimeDefaults(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyRuntimeDefaults fills defaults needed by BOTH the normal load path and
// legacy migration: the ping NodeSources default and the auto-generated
// jwt_secret. It is extracted so migrated configs are not left without a
// jwt_secret (which breaks /login) or NodeSources. It is idempotent and never
// overwrites an already-set jwt_secret.
func applyRuntimeDefaults(cfg *AppConfig) error {
	if len(cfg.EndNode.Ping.NodeSources) == 0 {
		cfg.EndNode.Ping.NodeSources = []PingNodeSource{
			{Type: "file", Source: "./config/ping_nodes.yaml"},
		}
	}
	// The secret is only used to sign in-memory session tokens; each restart
	// with a new secret simply invalidates existing sessions (users re-login).
	// This allows old configs to upgrade without modification.
	if cfg.EndNode.JWTSecret == "" {
		secret, err := generateRandomSecret(32)
		if err != nil {
			return fmt.Errorf("appconfig: generate jwt_secret: %w", err)
		}
		cfg.EndNode.JWTSecret = secret
		fmt.Fprintf(os.Stderr, "[appconfig] end_node.jwt_secret not set — using a random secret (sessions will be invalidated on restart)\n")
	}
	return nil
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
//
// The config embeds secrets — cluster/center tokens, jwt_secret, DNS API
// credentials — so it is written 0600 (not world-readable) and atomically via a
// temp file + rename, so a crash mid-write can't leave a truncated config and no
// world-readable window exists. `server --migrate` persists a freshly generated
// jwt_secret through here, which makes the 0600 guarantee load-bearing.
func SaveToFile(cfg *AppConfig, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("appconfig: marshal YAML: %w", err)
	}
	// Temp in the same directory so the rename is atomic (same filesystem).
	tmp, err := os.CreateTemp(filepath.Dir(path), ".appconfig-*.tmp")
	if err != nil {
		return fmt.Errorf("appconfig: create temp for %q: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below succeeds
	// os.CreateTemp already makes the file 0600; set it explicitly to be robust
	// against a permissive umask override on some platforms.
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("appconfig: chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("appconfig: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("appconfig: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("appconfig: rename into place %q: %w", path, err)
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

	switch strings.ToLower(strings.TrimSpace(cfg.Forward.ListenStack)) {
	case "", forward.ListenStackDual, forward.ListenStackIPv4, forward.ListenStackIPv6:
		// ok
	default:
		return fmt.Errorf("appconfig: forward.listen_stack %q is invalid; valid values are %q, %q, %q",
			cfg.Forward.ListenStack, forward.ListenStackDual, forward.ListenStackIPv4, forward.ListenStackIPv6)
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

	// The cluster token is the RPC encryption key (via HKDF). A short token derives
	// a low-entropy key; require a floor. (Length is not entropy — operators must
	// still use a random token.)
	const minClusterTokenLen = 16
	{
		enabledCount := 0
		for _, c := range cfg.Containers.Containers {
			if c.Enabled {
				enabledCount++
			}
		}
		if enabledCount == 0 {
			return fmt.Errorf("appconfig: at least one container entry must be enabled")
		}
		// When the RPC plane is enabled, the cluster token is the encryption key.
		if cfg.EndNode.RpcPort >= 1000 && len(cfg.EndNode.Cluster.Token) < minClusterTokenLen {
			return fmt.Errorf("appconfig: end_node.cluster.token must be >= %d chars when rpc is enabled", minClusterTokenLen)
		}
		// 0 means "unset" and falls back to the built-in default; a negative or
		// over-long interval is a misconfiguration. Peers expire after
		// cluster.NodeTimeOut (60s) without a beat, so an interval at or beyond
		// half that would let a healthy node be reclaimed between heartbeats.
		if v := cfg.EndNode.Cluster.HeartbeatIntervalSec; v < 0 || v > 30 {
			return fmt.Errorf("appconfig: end_node.cluster.heartbeat_interval_sec must be between 1 and 30 (0 = default 10), got %d", v)
		}
	}

	return nil
}

// WarnRemovedConfig reports configuration that no longer does anything, so an
// operator upgrading an old file learns why their cluster behaves differently
// instead of debugging silence. It takes the RAW decoded document because the
// removed keys have no struct fields left to inspect.
//
// Deliberately warnings, not errors: refusing to start would turn a cosmetic
// leftover key into an outage. The one case that still fails is a former center
// node, which has no containers configured and so trips the "at least one
// container" rule in Validate — the warning below is emitted first, which is
// what makes that error interpretable.
func WarnRemovedConfig(raw map[string]any) []string {
	var warnings []string
	add := func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}

	if v, ok := raw["node_type"]; ok {
		if s, _ := v.(string); strings.EqualFold(s, "center") {
			add("node_type: center — the center node was removed; this process will " +
				"start as an end node. Use end_node.cluster.static_nodes for discovery.")
		}
	}
	if _, ok := raw["center_node"]; ok {
		add("center_node: — the center node was removed; this section is ignored.")
	}
	if endNode, ok := raw["end_node"].(map[string]any); ok {
		if cl, ok := endNode["cluster"].(map[string]any); ok {
			for _, key := range []string{"center_node_host", "center_node_port", "center_token"} {
				if _, ok := cl[key]; ok {
					add("end_node.cluster.%s — the center node was removed; this key is "+
						"ignored. Use end_node.cluster.static_nodes instead.", key)
				}
			}
			if _, ok := cl["name"]; ok {
				add("end_node.cluster.name — removed; the cluster token is now the only " +
					"membership boundary, so the name was a redundant second factor.")
			}
		}
	}
	return warnings
}

// WarnRuntimeConfig reports settings that are valid but likely a mistake.
func WarnRuntimeConfig(cfg *AppConfig) []string {
	var warnings []string
	// static_nodes is the only bootstrap path now that the center is gone. A node
	// with none can still be *found* (a peer that knows it will register inbound)
	// but can never initiate discovery itself, and on a cold start with an empty
	// persisted directory it is simply isolated.
	if cfg.EndNode.RpcPort >= 1000 && len(cfg.EndNode.Cluster.StaticNodes) == 0 {
		warnings = append(warnings,
			"end_node.cluster.static_nodes is empty: this node cannot discover any peer "+
				"on its own. Configure at least one reachable peer unless this is a "+
				"single-node deployment.")
	}
	return warnings
}
