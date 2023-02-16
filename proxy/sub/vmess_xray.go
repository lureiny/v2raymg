//go:build !v2ray

package sub

import (
	"encoding/json"
	"fmt"

	"github.com/lureiny/v2raymg/proxy/config"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/infra/conf"
)

// https://github.com/2dust/v2rayN/wiki/%E5%88%86%E4%BA%AB%E9%93%BE%E6%8E%A5%E6%A0%BC%E5%BC%8F%E8%AF%B4%E6%98%8E(ver-2)
type VmessShareConfig struct {
	V    string `json:"v"`
	PS   string `json:"ps"`
	Add  string `json:"add"`
	Port string `json:"port"`
	ID   string `json:"id"`
	Aid  string `json:"aid"`
	Scy  string `json:"scy"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Host string `json:"host"`
	Path string `json:"path"`
	TLS  string `json:"tls"`
	Sni  string `json:"sni"`
}

func NewVmessShareConfig(in *config.InboundDetourConfig, email string, host string, port uint32) (*VmessShareConfig, error) {
	v := NewDefaultVmessShareConfig()
	// 获取UUID
	err := parseVmessAccountInfo(in, email, v)
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

	v.Port = fmt.Sprint(port)
	v.TLS = "tls"

	if err := insertVmessStreamSetting(v, in.StreamSetting); err != nil {
		return nil, err
	}
	if err := insertVmessTlsSetting(v, in.StreamSetting); err != nil {
		return nil, err
	}

	upstreamInbound, err := proxyManager.GetUpstreamInbound(v.Port)
	if err == nil {
		// 如果有上游fallback的inbound, 需要替换对应的port和tls配置
		v.Port = fmt.Sprintf("%d", upstreamInbound.PortRange)
		insertVmessTlsSetting(v, upstreamInbound.StreamSetting)
	}
	// XTLS only supports TCP, mKCP and DomainSocket for now.
	if v.TLS == "xtls" && (v.Net != "tcp" && v.Net != "kcp") {
		v.TLS = "tls"
	}

	// 根据sni和host设置remote host
	v.Add = parseHost(host, v.Sni)

	return v, nil
}

func insertVmessStreamSetting(v *VmessShareConfig, streamSetting *conf.StreamConfig) error {
	switch string(*streamSetting.Network) {
	case "tcp":
		v.Net = "tcp"
	case "kcp":
		v.Net = "kcp"
		kcpConfig := streamSetting.KCPSettings
		v.Path = *kcpConfig.Seed

		var kcpHeader map[string]string
		if len(kcpConfig.HeaderConfig) > 0 {
			err := json.Unmarshal(kcpConfig.HeaderConfig, &kcpHeader)
			if err != nil {
				return fmt.Errorf("invalid mKCP header config")
			}
			if headerType, ok := kcpHeader["type"]; ok {
				v.Type = headerType
			}
		}
	case "ws":
		v.Net = "ws"
		v.Host = streamSetting.WSSettings.Headers["Host"]
		v.Path = streamSetting.WSSettings.Path
	case "quic":
		v.Net = "quic"
		v.Path = streamSetting.QUICSettings.Key
		v.Host = streamSetting.QUICSettings.Security
		var quicHeader map[string]string
		if len(streamSetting.QUICSettings.Header) > 0 {
			err := json.Unmarshal(streamSetting.QUICSettings.Header, &quicHeader)
			if err != nil {
				return fmt.Errorf("invalid quic header config.")
			}
			if headerType, ok := quicHeader["type"]; ok {
				v.Type = headerType
			}
		}
	case "grpc":
		v.Net = "grpc"
		v.Path = streamSetting.GRPCConfig.ServiceName
		// 暂时不考虑xray-core
	case "http":
		v.Net = "h2"
		v.Host = (*streamSetting.HTTPSettings.Host)[0]
		v.Path = streamSetting.HTTPSettings.Path
	default:
		return fmt.Errorf("Unsupport transport protocol %s", *streamSetting.Network)
	}
	return nil
}

func NewDefaultVmessShareConfig() *VmessShareConfig {
	return &VmessShareConfig{V: "2"}
}

func parseVmessAccountInfo(in *config.InboundDetourConfig, email string, sharedConfig *VmessShareConfig) error {
	vmessConfig := new(conf.VMessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vmessConfig)
	if err != nil {
		return err
	}

	for _, rawData := range vmessConfig.Users {
		user := new(protocol.User)
		if err := json.Unmarshal(rawData, user); err != nil || user.Email != email {
			continue
		}
		account := new(conf.VMessAccount)
		json.Unmarshal(rawData, account)
		sharedConfig.ID = account.ID
		sharedConfig.Aid = fmt.Sprint(account.AlterIds)
		return nil

	}

	return fmt.Errorf("%s not in %s", email, in.Tag)
}

func insertVmessTlsSetting(v *VmessShareConfig, streamSetting *conf.StreamConfig) error {
	switch string(streamSetting.Security) {
	case "none":
		return nil
	case "tls":
		v.TLS = "tls"
		v.Sni = streamSetting.TLSSettings.ServerName
	case "xtls":
		v.TLS = "xtls"
		v.Sni = streamSetting.XTLSSettings.ServerName
	default:
		return fmt.Errorf("Unsupport Security protocol %s", streamSetting.Security)
	}
	return nil
}
