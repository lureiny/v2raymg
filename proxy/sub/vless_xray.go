//go:build !v2ray

package sub

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/lureiny/v2raymg/proxy/config"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/proxy/vless"
)

type URIAdapter interface {
	// Build 生成对应URI
	Build() string
}

type VlessShareConfig struct {
	BaseConfig      BaseConfig
	ProtocolConfig  *ProtocolConfig
	TransportConfig *VlessTransportConfig
	TLSConfig       *VlessTLSConfig
	NodeName        string
}

func (c *VlessShareConfig) Build() string {
	params := []string{c.ProtocolConfig.Build(), c.TransportConfig.Build(), c.TLSConfig.Build()}
	if c.BaseConfig.Flow != "" {
		params = append(params, fmt.Sprintf("flow=%s", c.BaseConfig.Flow))
	}
	paramsURI := strings.Join(params, "&")
	return fmt.Sprintf("%s?%s#%s", c.BaseConfig.Build(), fixUri(paramsURI), url.QueryEscape(c.NodeName))
}

func parseVlessAccountInfo(in *config.InboundDetourConfig, email string, sharedConfig *VlessShareConfig) error {
	vlessConfig := new(conf.VLessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vlessConfig)
	if err != nil {
		return err
	}

	for _, rawData := range vlessConfig.Clients {
		client := new(protocol.User)
		if err := json.Unmarshal(rawData, client); err != nil || client.Email != email {
			continue
		}
		account := new(vless.Account)
		json.Unmarshal(rawData, account)
		sharedConfig.BaseConfig.UUID = account.Id
		sharedConfig.BaseConfig.Flow = account.Flow
		return nil

	}
	return fmt.Errorf("%s not in %s", email, in.Tag)
}

func NewVlessShareConfig(in *config.InboundDetourConfig, email string, host string, port uint32) (*VlessShareConfig, error) {
	sharedConfig := newDefaultVlessShareConfig()
	// 获取UUID
	err := parseVlessAccountInfo(in, email, sharedConfig)
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

func newDefaultVlessShareConfig() *VlessShareConfig {
	return &VlessShareConfig{}
}
