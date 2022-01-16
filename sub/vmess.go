package sub

import (
	"encoding/json"
	"errors"
	"fmt"

	protocolP "github.com/lureiny/v2raymg/protocol"
	"github.com/v2fly/v2ray-core/v4/common/protocol"
	"github.com/v2fly/v2ray-core/v4/infra/conf"
)

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

func NewVmessShareConfig(in *protocolP.InboundDetourConfig, email string, host string, port uint32) (*VmessShareConfig, error) {
	// 获取UUID
	id, aid, err := getVmessUserUUID(in, email)
	if err != nil {
		return nil, err
	}

	if host == "" {
		host = in.ListenOn
	}

	if port == 0 {
		port = in.PortRange
	}

	v := NewDefaultVmessShareConfig()
	v.Add = host
	v.Port = fmt.Sprint(port)
	v.ID = id
	v.Aid = fmt.Sprint(aid)
	v.TLS = "tls"

	if err := insertVmessStreamSetting(v, in.StreamSetting); err != nil {
		return nil, err
	}
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
				return errors.New("invalid mKCP header config")
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
				return errors.New("invalid quic header config.")
			}
			if headerType, ok := quicHeader["type"]; ok {
				v.Type = headerType
			}
		}
	case "grpc":
		v.Net = "grpc"
		v.Path = streamSetting.GRPCSettings.ServiceName
		// 暂时不考虑xray-core
	case "http":
		v.Net = "h2"
		v.Host = (*streamSetting.HTTPSettings.Host)[0]
		v.Path = streamSetting.HTTPSettings.Path
	default:
		return errors.New(fmt.Sprintf("Unsupport transport protocol %s", *streamSetting.Network))
	}
	return nil
}

func NewDefaultVmessShareConfig() *VmessShareConfig {
	return &VmessShareConfig{V: "2"}
}

func getVmessUserUUID(in *protocolP.InboundDetourConfig, email string) (string, int, error) {
	vmessConfig := new(conf.VMessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vmessConfig)
	if err != nil {
		return "", 0, err
	}

	for _, rawData := range vmessConfig.Users {
		user := new(protocol.User)
		if err := json.Unmarshal(rawData, user); err != nil || user.Email != email {
			continue
		}
		account := new(conf.VMessAccount)
		json.Unmarshal(rawData, account)
		return account.ID, int(account.AlterIds), nil

	}

	return "", 0, errors.New("%d not in")
}
