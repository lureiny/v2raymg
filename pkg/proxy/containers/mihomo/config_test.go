package mihomo

import (
	"strings"
	"testing"
)

func TestMihomoConfig_Decode_Defaults(t *testing.T) {
	var c MihomoConfig
	if err := c.Decode(map[string]any{}); err != nil {
		t.Fatalf("Decode empty: %v", err)
	}
	if c.BinaryPath != "/usr/local/bin/mihomo" {
		t.Errorf("BinaryPath default = %q", c.BinaryPath)
	}
	if c.ConfigFilePath != "/etc/v2raymg/mihomo.yaml" {
		t.Errorf("ConfigFilePath default = %q", c.ConfigFilePath)
	}
	if c.DataDir != "/var/lib/v2raymg/mihomo" {
		t.Errorf("DataDir default = %q", c.DataDir)
	}
	if c.ExternalController != "127.0.0.1:9090" {
		t.Errorf("ExternalController default = %q", c.ExternalController)
	}
	if c.Secret != "" {
		t.Errorf("Secret default = %q (want empty)", c.Secret)
	}
	if c.ReleaseTag != "latest" {
		t.Errorf("ReleaseTag default = %q (want \"latest\")", c.ReleaseTag)
	}
	if !c.AutoDownload {
		t.Error("AutoDownload default = false (want true)")
	}
}

func TestMihomoConfig_Decode_Overrides(t *testing.T) {
	var c MihomoConfig
	err := c.Decode(map[string]any{
		"binary_path":         "/opt/mihomo",
		"config_file_path":    "/opt/mihomo.yaml",
		"data_dir":            "/var/opt/mihomo",
		"external_controller": "127.0.0.1:19090",
		"secret":              "s3cr3t",
		"release_tag":         "Prerelease-Alpha",
		"auto_download":       false,
	})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if c.BinaryPath != "/opt/mihomo" {
		t.Errorf("BinaryPath = %q", c.BinaryPath)
	}
	if c.ConfigFilePath != "/opt/mihomo.yaml" {
		t.Errorf("ConfigFilePath = %q", c.ConfigFilePath)
	}
	if c.DataDir != "/var/opt/mihomo" {
		t.Errorf("DataDir = %q", c.DataDir)
	}
	if c.ExternalController != "127.0.0.1:19090" {
		t.Errorf("ExternalController = %q", c.ExternalController)
	}
	if c.Secret != "s3cr3t" {
		t.Errorf("Secret = %q", c.Secret)
	}
	if c.ReleaseTag != "Prerelease-Alpha" {
		t.Errorf("ReleaseTag = %q", c.ReleaseTag)
	}
	if c.AutoDownload {
		t.Error("AutoDownload = true (want false after override)")
	}
}

func TestMihomoConfig_Decode_ValidationFailures(t *testing.T) {
	cases := []struct {
		name         string
		cfg          map[string]any
		wantErrMatch string
	}{
		{
			name:         "empty binary_path",
			cfg:          map[string]any{"binary_path": ""},
			wantErrMatch: "binary_path",
		},
		{
			name:         "empty config_file_path",
			cfg:          map[string]any{"config_file_path": ""},
			wantErrMatch: "config_file_path",
		},
		{
			name:         "empty data_dir",
			cfg:          map[string]any{"data_dir": ""},
			wantErrMatch: "data_dir",
		},
		{
			name:         "empty external_controller",
			cfg:          map[string]any{"external_controller": ""},
			wantErrMatch: "external_controller",
		},
		{
			name:         "external_controller without port",
			cfg:          map[string]any{"external_controller": "127.0.0.1"},
			wantErrMatch: "host:port",
		},
		{
			name:         "empty release_tag",
			cfg:          map[string]any{"release_tag": ""},
			wantErrMatch: "release_tag",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c MihomoConfig
			err := c.Decode(tc.cfg)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErrMatch) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrMatch)
			}
		})
	}
}
