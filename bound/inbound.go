package bound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/protocol"
	v4 "github.com/v2fly/v2ray-core/v4"
	"github.com/v2fly/v2ray-core/v4/app/proxyman"
	"github.com/v2fly/v2ray-core/v4/app/proxyman/command"
	"github.com/v2fly/v2ray-core/v4/common/net"
	"github.com/v2fly/v2ray-core/v4/common/serial"
	"github.com/v2fly/v2ray-core/v4/infra/conf"
	"github.com/v2fly/v2ray-core/v4/transport/internet"
)

func AddInbound(con command.HandlerServiceClient, inboundConfig *protocol.InboundDetourConfig) error {
	inboundHandlerConfig, err := NewInboundHandlerConfig(inboundConfig)
	if err != nil {
		return err
	}

	_, err = con.AddInbound(context.Background(), &command.AddInboundRequest{
		Inbound: inboundHandlerConfig,
	})
	return err
}

func NewInboundHandlerConfig(inboundConfig *protocol.InboundDetourConfig) (*v4.InboundHandlerConfig, error) {
	r, err := NewReceiverSettings(inboundConfig)
	if err != nil {
		return nil, err
	}

	s, err := NewProxySettings(inboundConfig)
	if err != nil {
		return nil, err
	}

	inboundHandlerConfig := &v4.InboundHandlerConfig{
		Tag:              inboundConfig.Tag,
		ReceiverSettings: serial.ToTypedMessage(r), // 对应配置文件中的port, listen, streamSettings
		ProxySettings:    s,                        // 各种proto中定义的config，"github.com/v2fly/v2ray-core/v4/proxy/vless/inbound", 对应配置文件中的settings
	}
	return inboundHandlerConfig, nil
}

func NewReceiverSettings(inboundConfig *protocol.InboundDetourConfig) (*proxyman.ReceiverConfig, error) {
	s, err := NewProtoStreamSettings(inboundConfig.StreamSetting)
	if err != nil {
		return nil, err
	}

	receiverConfig := &proxyman.ReceiverConfig{
		Listen:         NewListen(inboundConfig.ListenOn),
		PortRange:      NewSinglePortRange(uint16(inboundConfig.PortRange)),
		StreamSettings: s,
	}
	return receiverConfig, nil
}

// 不支持指定监听ip
func NewListen(host string) *net.IPOrDomain {
	if len(host) > 0 && host != "127.0.0.1" && host != "localhost" {
		return net.NewIPOrDomain(net.AnyIP)
	} else {
		return net.NewIPOrDomain(net.LocalHostIP)
	}
}

func NewSinglePortRange(port uint16) *net.PortRange {
	return net.SinglePortRange(net.Port(port))
}

func NewProtoStreamSettings(streamConfig *conf.StreamConfig) (*internet.StreamConfig, error) {
	return streamConfig.Build()
}

func NewProxySettings(inboundConfig *protocol.InboundDetourConfig) (*serial.TypedMessage, error) {
	switch strings.ToLower(inboundConfig.Protocol) {
	case "vless":
		vlessConfig := new(conf.VLessInboundConfig)
		err := json.Unmarshal([]byte(*(inboundConfig.Settings)), vlessConfig)

		if err != nil {
			return nil, err
		}
		p, err := vlessConfig.Build()
		if err != nil {
			return nil, err
		}

		return serial.ToTypedMessage(p), nil
	case "vmess":
		vmessConfig := new(conf.VMessInboundConfig)

		err := json.Unmarshal([]byte(*(inboundConfig.Settings)), vmessConfig)
		if err != nil {
			return nil, err
		}
		p, err := vmessConfig.Build()
		if err != nil {
			return nil, err
		}
		return serial.ToTypedMessage(p), nil
	}
	return nil, errors.New(fmt.Sprintf("Unsupport proxy protocol: %v", strings.ToLower(inboundConfig.Protocol)))
}
