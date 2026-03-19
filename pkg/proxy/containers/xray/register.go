// Package xray provides Xray container implementation.
package xray

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func init() {
	container.RegisterFactory(contracts.ContainerXray, &xrayFactory{})
}

type xrayFactory struct{}

func (f *xrayFactory) NewConfigObj() container.ContainerConfig {
	return &ExecutorConfig{}
}

func (f *xrayFactory) New(opts container.BuildOptions) (container.Container, error) {
	cfg, ok := opts.Config.(*ExecutorConfig)
	if !ok || cfg == nil {
		return nil, fmt.Errorf("xray: invalid config type, expected *ExecutorConfig")
	}
	cfg.UserManager = opts.UserManager
	cfg.CertManager, _ = opts.CertManager.(CertManagerGetter)
	cfg.StoreMgr = opts.StoreMgr
	return NewExecutor(*cfg)
}

// Decode implements container.ContainerConfig for ExecutorConfig.
// It decodes a generic map into the fields relevant for xray configuration.
func (c *ExecutorConfig) Decode(cfg map[string]any) error {
	// 设置默认值
	c.BinaryPath = "/tmp/xray-bin/xray"
	c.ConfigFilePath = "/tmp/xray-config.json"
	c.GRPCAPIAddress = "127.0.0.1:62789"
	c.AutoDownload = true // 默认自动下载 xray 二进制

	// 用配置文件值覆盖
	if v, ok := cfg["binary_path"].(string); ok && v != "" {
		c.BinaryPath = v
	}
	if v, ok := cfg["config_file"].(string); ok && v != "" {
		c.ConfigFilePath = v
	}
	if v, ok := cfg["grpc_port"].(float64); ok {
		c.GRPCAPIAddress = fmt.Sprintf("127.0.0.1:%d", int(v))
	}
	if v, ok := cfg["debug"].(bool); ok {
		c.Debug = v
	}
	if v, ok := cfg["auto_download"].(bool); ok {
		c.AutoDownload = v
	}
	return nil
}
