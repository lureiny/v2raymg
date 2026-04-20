package hysteria

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// HysteriaConfig holds configuration for the hysteria container.
type HysteriaConfig struct {
	BinaryPath     string `json:"binary_path"`
	ConfigFilePath string `json:"config_file_path"`
	Listen         string `json:"listen"`
	Port           int    `json:"port"`
	// Host is the public hostname clients use to reach this node. When
	// Domain is empty, Host is used as the TLS/ACME domain. Keeping the
	// two names separate lets configs that already use `host` (e.g.
	// subscription URIs) avoid duplicating the value.
	Host               string `json:"host"`
	Domain             string `json:"domain"`
	CertFile           string `json:"cert_file"` // TLS cert file path (optional fallback if certReader unavailable)
	KeyFile            string `json:"key_file"`  // TLS key file path (optional fallback if certReader unavailable)
	TrafficStatsPort   int    `json:"traffic_stats_port"`
	TrafficStatsSecret string `json:"traffic_stats_secret"`
	Version            string `json:"version"`
}

// Decode implements container.ContainerConfig.
func (c *HysteriaConfig) Decode(cfg map[string]any) error {
	c.BinaryPath = "/usr/local/bin/hysteria"
	c.ConfigFilePath = "/etc/v2raymg/hysteria.yaml"
	c.Listen = "0.0.0.0"
	// hysteria now sits behind the UDP forward layer and only listens on
	// 127.0.0.1, so the actual port number is an implementation detail.
	// Keep it below the default forward-pool range (10000–65535) to avoid
	// colliding with per-user public ports, and pick something other than
	// 443 so upgrading deployments release their public 443 automatically.
	c.Port = 9443
	c.TrafficStatsPort = 9999
	c.Version = "app/v2.7.1"

	if v, ok := cfg["binary_path"].(string); ok {
		c.BinaryPath = v
	}
	if v, ok := cfg["config_file_path"].(string); ok {
		c.ConfigFilePath = v
	}
	if v, ok := cfg["listen"].(string); ok {
		c.Listen = v
	}
	if v, ok := cfg["port"].(float64); ok {
		c.Port = int(v)
	} else if v, ok := cfg["port"].(int); ok {
		c.Port = v
	}
	if v, ok := cfg["host"].(string); ok {
		c.Host = v
	}
	if v, ok := cfg["domain"].(string); ok {
		c.Domain = v
	}
	if v, ok := cfg["traffic_stats_port"].(float64); ok {
		c.TrafficStatsPort = int(v)
	} else if v, ok := cfg["traffic_stats_port"].(int); ok {
		c.TrafficStatsPort = v
	}
	if v, ok := cfg["cert_file"].(string); ok {
		c.CertFile = v
	}
	if v, ok := cfg["key_file"].(string); ok {
		c.KeyFile = v
	}
	if v, ok := cfg["traffic_stats_secret"].(string); ok {
		c.TrafficStatsSecret = v
	}
	if v, ok := cfg["version"].(string); ok {
		c.Version = v
	}

	if c.TrafficStatsSecret == "" {
		secret, err := generateSecret()
		if err != nil {
			return fmt.Errorf("hysteria: generate traffic stats secret: %w", err)
		}
		c.TrafficStatsSecret = secret
	}

	// Fall back to host when domain is unset so deployments can configure a
	// single hostname instead of both `host` and `domain`. A further
	// fallback to the node-level ProxyHost is applied in hysteriaFactory.New
	// so upgrades don't require touching the hysteria config at all.
	if c.Domain == "" && c.Host != "" {
		c.Domain = c.Host
	}

	return nil
}

// generateSecret generates a random 16-byte hex string.
func generateSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
