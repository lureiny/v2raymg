package sub

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	protocolP "github.com/lureiny/v2raymg/protocol"
	"github.com/v2fly/v2ray-core/v4/common/protocol"
	"github.com/v2fly/v2ray-core/v4/infra/conf"
	"github.com/v2fly/v2ray-core/v4/proxy/vless"
)

type URIAdapter interface {
	// Build 生成对应URI
	Build() string
}

type VlessShareConfig struct {
	BaseConfig      VlessBaseConfig
	ProtocolConfig  *VlessProtocolConfig
	TransportConfig *VlessTransportConfig
	TLSConfig       *VlessTLSConfig
}

func (c *VlessShareConfig) Build() string {
	params := []string{c.ProtocolConfig.Build(), c.TransportConfig.Build(), c.TLSConfig.Build()}
	paramsURI := strings.Join(params, "&")
	return fmt.Sprintf("%s?%s", c.BaseConfig.Build(), paramsURI)
}

func getVlessUserUUID(in *protocolP.InboundDetourConfig, email string) (string, error) {
	vlessConfig := new(conf.VLessInboundConfig)

	err := json.Unmarshal([]byte(*(in.Settings)), vlessConfig)
	if err != nil {
		return "", err
	}

	for _, rawData := range vlessConfig.Clients {
		client := new(protocol.User)
		if err := json.Unmarshal(rawData, client); err != nil || client.Email != email {
			continue
		}
		account := new(vless.Account)
		json.Unmarshal(rawData, account)
		return account.Id, nil

	}
	errMsg := fmt.Sprintf("%s not in %s", email, in.Tag)
	return "", errors.New(errMsg)
}

func NewVlessShareConfig(in *protocolP.InboundDetourConfig, email string, host string, port uint32) (*VlessShareConfig, error) {
	// 获取UUID
	id, err := getVlessUserUUID(in, email)
	if err != nil {
		return nil, err
	}

	if host == "" {
		host = in.ListenOn
	}

	if port == 0 {
		port = in.PortRange
	}

	v := newDefaultVlessShareConfig()
	v.BaseConfig.UUID = id
	v.BaseConfig.RemoteHost = host
	v.BaseConfig.RemotePort = port

	p, err := newProtocolConfig(in.StreamSetting)
	if err != nil {
		return nil, err
	}
	v.ProtocolConfig = p

	t, err := newTransportConfig(in.StreamSetting)
	if err != nil {
		return nil, err
	}
	v.TransportConfig = t

	v.TLSConfig = newTLSConfig(in.StreamSetting.TLSSettings)

	return v, nil
}

func newDefaultVlessShareConfig() *VlessShareConfig {
	return &VlessShareConfig{}
}
