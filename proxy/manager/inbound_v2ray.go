//go:build v2ray

package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/proxy/protocol"
	core "github.com/v2fly/v2ray-core/v5"
	"github.com/v2fly/v2ray-core/v5/app/proxyman/command"
	conf "github.com/v2fly/v2ray-core/v5/infra/conf/v4"
	"google.golang.org/grpc"
)

func AddInbound(con command.HandlerServiceClient, inboundConfigString []byte) error {
	inboundHandlerConfig, err := NewInboundHandlerConfig(inboundConfigString)
	if err != nil {
		return err
	}

	_, err = con.AddInbound(context.Background(), &command.AddInboundRequest{
		Inbound: inboundHandlerConfig,
	})
	return err
}

func AddInboundToRuntime(runtimeConfig *RuntimeConfig, inboundConfigByte []byte) error {
	// 创建grpc client
	cmdConn, err := grpc.Dial(fmt.Sprintf("%s:%d", runtimeConfig.Host, runtimeConfig.Port), grpc.WithInsecure())
	if err != nil {
		return err
	}

	handlerClient := command.NewHandlerServiceClient(cmdConn)
	return AddInbound(handlerClient, inboundConfigByte)
}

func NewInboundHandlerConfig(rawConfig []byte) (*core.InboundHandlerConfig, error) {
	inboundDetourConfig := &conf.InboundDetourConfig{}
	err := json.Unmarshal(rawConfig, inboundDetourConfig)
	if err != nil {
		return nil, err
	}
	if inboundDetourConfig.Tag == "" {
		return nil, fmt.Errorf("tag can not be empty")
	}

	portRange := inboundDetourConfig.PortRange
	if portRange.From != portRange.To {
		return nil, fmt.Errorf("unsupport port range")
	}
	return inboundDetourConfig.Build()
}

func RemoveInbound(con command.HandlerServiceClient, tag string) error {
	_, err := con.RemoveInbound(context.Background(), &command.RemoveInboundRequest{
		Tag: tag,
	})
	return err
}

func RemoveInboundFromRuntime(runtimeConfig *RuntimeConfig, tag string) error {
	// 创建grpc client
	cmdConn, err := grpc.Dial(fmt.Sprintf("%s:%d", runtimeConfig.Host, runtimeConfig.Port), grpc.WithInsecure())
	if err != nil {
		return err
	}

	handlerClient := command.NewHandlerServiceClient(cmdConn)
	return RemoveInbound(handlerClient, tag)
}

func resetInboundUser(inboundSetting *json.RawMessage, p string) {
	switch strings.ToLower(p) {
	case VlessProtocolName:
		vlessConfig := new(protocol.VLessInboundConfig)
		json.Unmarshal([]byte(*(inboundSetting)), vlessConfig)
		vlessConfig.Clients = []json.RawMessage{}
		vlessConfigBytes, _ := json.MarshalIndent(vlessConfig, "", "    ")
		*inboundSetting = vlessConfigBytes
	case VmessProtocolName:
		vmessConfig := new(conf.VMessInboundConfig)
		json.Unmarshal([]byte(*(inboundSetting)), vmessConfig)
		vmessConfig.Users = []json.RawMessage{}
		vmessConfigBytes, _ := json.MarshalIndent(vmessConfig, "", "    ")
		*inboundSetting = vmessConfigBytes
	case TrojanProtocolName:
		trojanConfig := new(conf.TrojanServerConfig)
		json.Unmarshal([]byte(*(inboundSetting)), trojanConfig)
		trojanConfig.Clients = []*conf.TrojanUserConfig{}
		trojanConfigBytes, _ := json.MarshalIndent(trojanConfig, "", "    ")
		*inboundSetting = trojanConfigBytes
	}
}

func NewProtocolSetting(p string) *json.RawMessage {
	switch strings.ToLower(p) {
	case VmessProtocolName:
		vmessConfig := new(conf.VMessInboundConfig)
		vmessConfigBytes, _ := json.MarshalIndent(vmessConfig, "", "    ")
		return (*json.RawMessage)(&vmessConfigBytes)
	case TrojanProtocolName:
		trojanConfig := new(conf.TrojanServerConfig)
		trojanConfigBytes, _ := json.MarshalIndent(trojanConfig, "", "    ")
		return (*json.RawMessage)(&trojanConfigBytes)
	// 默认返回vless的配置
	default:
		vlessConfig := new(protocol.VLessInboundConfig)
		vlessConfigBytes, _ := json.MarshalIndent(vlessConfig, "", "    ")
		return (*json.RawMessage)(&vlessConfigBytes)
	}
}
