//go:build v2ray

package sub

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/lureiny/v2raymg/proxy/config"
	"github.com/v2fly/v2ray-core/v5/infra/conf/v4"
)

type TrojanShareConfig struct {
	BaseConfig      BaseConfig
	ProtocolConfig  *ProtocolConfig
	TransportConfig *VlessTransportConfig
	TLSConfig       *VlessTLSConfig
	NodeName        string
}

func (c *TrojanShareConfig) Build() string {
	params := []string{c.ProtocolConfig.Build(), c.TransportConfig.Build(), c.TLSConfig.Build()}
	paramsURI := strings.Join(params, "&")
	return fmt.Sprintf("%s?%s#%s", c.BaseConfig.Build(), fixUri(paramsURI), url.QueryEscape(c.NodeName))
}

func parseTrojanAccountInfo(in *config.InboundDetourConfig, email string, sharedConfig *TrojanShareConfig) error {
	trojanConfig := new(v4.TrojanServerConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), trojanConfig)
	if err != nil {
		return err
	}

	for _, client := range trojanConfig.Clients {
		if client.Email != email {
			continue
		}
		sharedConfig.BaseConfig.UUID = client.Password
		return nil

	}
	return fmt.Errorf("%s not in %s", email, in.Tag)
}

func NewTrojanShareConfig(in *config.InboundDetourConfig, email string, host string, port uint32) (*TrojanShareConfig, error) {
	sharedConfig := newDefaultTrojanShareConfig()
	// 获取UUID
	err := parseTrojanAccountInfo(in, email, sharedConfig)
	if err != nil {
		return nil, err
	}

	// 外部host为空， 则使用监听地址
	if host == "" {
		host = in.ListenOn
	}

	// 外部传过来的port为0的话, 则使用当前监听的端口
	if port == 0 {
		port = in.PortRange
	}

	sharedConfig.BaseConfig.RemotePort = port

	p, err := newProtocolConfig(in.StreamSetting)
	if err != nil {
		return nil, err
	}
	if p.Type == "tcp" {
		p.Type = "original"
	}
	sharedConfig.ProtocolConfig = p

	t, err := newTransportConfig(in.StreamSetting)
	if err != nil {
		return nil, err
	}
	sharedConfig.TransportConfig = t

	sharedConfig.TLSConfig = newTLSOrXTLSConfig(in.StreamSetting)

	upstreamInbound, err := proxyManager.GetUpstreamInbound(fmt.Sprintf("%d", sharedConfig.BaseConfig.RemotePort))
	if err == nil {
		// 如果有上游fallback的inbound, 需要替换对应的port和tls配置
		sharedConfig.BaseConfig.RemotePort = upstreamInbound.PortRange
		sharedConfig.TLSConfig = newTLSOrXTLSConfig(upstreamInbound.StreamSetting)
	}

	// 根据sni和host设置remote host
	sharedConfig.BaseConfig.RemoteHost = parseHost(host, sharedConfig.TLSConfig.SNI)

	return sharedConfig, nil
}

func newDefaultTrojanShareConfig() *TrojanShareConfig {
	return &TrojanShareConfig{}
}
