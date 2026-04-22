package mihomo

import (
	"fmt"
	"net"
)

// MihomoConfig holds configuration for the mihomo container.
type MihomoConfig struct {
	BinaryPath     string `json:"binary_path"`
	ConfigFilePath string `json:"config_file_path"`
	// DataDir maps to mihomo's -d flag: the home directory where mihomo reads
	// GeoIP/GeoSite data files and writes runtime state.
	DataDir string `json:"data_dir"`
	// ExternalController is the mihomo REST API listen address (host:port).
	// v2raymg uses it as the sole control channel; bind to loopback only —
	// the API is not meant to be exposed.
	ExternalController string `json:"external_controller"`
	// Secret is the REST API bearer token. When empty, mihomo skips
	// authentication entirely; acceptable as long as ExternalController is
	// bound to 127.0.0.1.
	Secret string `json:"secret"`
	// Version is the mihomo release tag used when the binary is missing and
	// must be auto-downloaded (e.g. "Prerelease-Alpha"). Alpha branch is the
	// development target; see docs/mihomo-container-design.md.
	Version string `json:"version"`
}

// Decode implements container.ContainerConfig.
func (c *MihomoConfig) Decode(cfg map[string]any) error {
	c.BinaryPath = "/usr/local/bin/mihomo"
	c.ConfigFilePath = "/etc/v2raymg/mihomo.yaml"
	c.DataDir = "/var/lib/v2raymg/mihomo"
	c.ExternalController = "127.0.0.1:9090"
	c.Secret = ""
	c.Version = "Prerelease-Alpha"

	if v, ok := cfg["binary_path"].(string); ok {
		c.BinaryPath = v
	}
	if v, ok := cfg["config_file_path"].(string); ok {
		c.ConfigFilePath = v
	}
	if v, ok := cfg["data_dir"].(string); ok {
		c.DataDir = v
	}
	if v, ok := cfg["external_controller"].(string); ok {
		c.ExternalController = v
	}
	if v, ok := cfg["secret"].(string); ok {
		c.Secret = v
	}
	if v, ok := cfg["version"].(string); ok {
		c.Version = v
	}

	return c.validate()
}

func (c *MihomoConfig) validate() error {
	if c.BinaryPath == "" {
		return fmt.Errorf("mihomo: binary_path must not be empty")
	}
	if c.ConfigFilePath == "" {
		return fmt.Errorf("mihomo: config_file_path must not be empty")
	}
	if c.DataDir == "" {
		return fmt.Errorf("mihomo: data_dir must not be empty")
	}
	if c.ExternalController == "" {
		return fmt.Errorf("mihomo: external_controller must not be empty")
	}
	if _, _, err := net.SplitHostPort(c.ExternalController); err != nil {
		return fmt.Errorf("mihomo: external_controller %q is not a valid host:port: %w", c.ExternalController, err)
	}
	if c.Version == "" {
		return fmt.Errorf("mihomo: version must not be empty")
	}
	return nil
}
