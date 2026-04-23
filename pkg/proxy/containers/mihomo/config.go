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
	// ReleaseTag is the mihomo release tag used by the updater / auto-download
	// path. The default "latest" resolves via GitHub's /releases/latest endpoint
	// (returns the most recent non-prerelease stable). To track the permanent
	// Alpha pre-release, set this to "Prerelease-Alpha" explicitly.
	//
	// Name: "ReleaseTag" (not "Version") to avoid collision with the running
	// mihomo build string exposed by Container.Version().
	ReleaseTag string `json:"release_tag"`
	// AutoDownload enables fetching the binary via the updater when it is
	// missing at Start, and makes Container.Update() a real implementation
	// instead of an ErrNotSupported stub. Default true — set to false only
	// when the binary is pre-installed and its version is externally managed.
	AutoDownload bool `json:"auto_download"`
}

// Decode implements container.ContainerConfig.
func (c *MihomoConfig) Decode(cfg map[string]any) error {
	c.BinaryPath = "/usr/local/bin/mihomo"
	c.ConfigFilePath = "/etc/v2raymg/mihomo.yaml"
	c.DataDir = "/var/lib/v2raymg/mihomo"
	c.ExternalController = "127.0.0.1:9090"
	c.Secret = ""
	c.ReleaseTag = "latest"
	c.AutoDownload = true

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
	if v, ok := cfg["release_tag"].(string); ok {
		c.ReleaseTag = v
	}
	if v, ok := cfg["auto_download"].(bool); ok {
		c.AutoDownload = v
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
	if c.ReleaseTag == "" {
		return fmt.Errorf("mihomo: release_tag must not be empty")
	}
	return nil
}
