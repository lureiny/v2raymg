//go:build !v2ray

package sub

import (
	"fmt"
	"sort"

	"github.com/xtls/xray-core/infra/conf"
)

// for trojan and vless
type ProtocolConfig struct {
	Type       string
	Encryption string
}

func (c *ProtocolConfig) Build() string {
	// 参考https://github.com/XTLS/Xray-core/discussions/716，暂时省略"encryption"字段
	// 不支持vmess指定加密方式
	return fmt.Sprintf("type=%s", c.Type)
}

func inProtocols(p string) bool {
	protocols := []string{"grpc", "http", "quic", "tcp", "ws"}
	index := sort.SearchStrings(protocols, p)
	return index != len(protocols)
}

func newProtocolConfig(streamSetting *conf.StreamConfig) (*ProtocolConfig, error) {
	if inProtocols(string(*streamSetting.Network)) {
		return &ProtocolConfig{Type: string(*streamSetting.Network), Encryption: "none"}, nil
	}
	errMsg := fmt.Sprintf("unsupoort network config: %v", string(*streamSetting.Network))
	return nil, fmt.Errorf(errMsg)
}
