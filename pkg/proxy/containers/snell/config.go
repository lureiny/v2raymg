package snell

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// SnellConfig holds configuration for the snell container.
type SnellConfig struct {
	BinaryPath     string `json:"binary_path"`
	ConfigFilePath string `json:"config_file_path"`
	Listen         string `json:"listen"`
	Port           int    `json:"port"`
	PSK            string `json:"psk"`
	Version        string `json:"version"`
}

// Decode implements container.ContainerConfig.
func (c *SnellConfig) Decode(cfg map[string]any) error {
	c.BinaryPath = "/usr/local/bin/snell-server"
	c.ConfigFilePath = "/etc/v2raymg/snell.conf"
	c.Listen = "127.0.0.1"
	c.Port = 16160
	c.Version = "v5.0.1"

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
	}
	if v, ok := cfg["psk"].(string); ok {
		c.PSK = v
	}
	if v, ok := cfg["version"].(string); ok {
		c.Version = v
	}

	if c.PSK == "" || c.PSK == "auto" {
		psk, err := generatePSK()
		if err != nil {
			return fmt.Errorf("snell: generate psk: %w", err)
		}
		c.PSK = psk
	}

	return nil
}

// generatePSK generates a random 16-byte hex PSK.
func generatePSK() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
