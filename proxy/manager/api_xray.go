//go:build !v2ray

package manager

import (
	"encoding/json"

	"github.com/lureiny/v2raymg/proxy/protocol"
	"github.com/xtls/xray-core/infra/conf"
)

func configAllApiInfo(config *protocol.V2rayConfig) {
	config.Stats = &conf.StatsConfig{}
	configApi(config)
	configRoute(config)
	configPolicy(config)
}

func configApi(config *protocol.V2rayConfig) {
	config.API = &conf.APIConfig{
		Tag: apiTag,
		Services: []string{
			"HandlerService",
			"LoggerService",
			"StatsService",
		},
	}
}

func configRoute(config *protocol.V2rayConfig) {
	config.RouterConfig.RuleList = append(
		config.RouterConfig.RuleList,
		json.RawMessage(`{
			"inboundTag": [
				"api"
			],
			"outboundTag": "api",
			"type": "field"
		}`),
	)
}

func configPolicy(config *protocol.V2rayConfig) {
	config.Policy = &conf.PolicyConfig{
		Levels: map[uint32]*conf.Policy{
			0: {
				StatsUserUplink:   true,
				StatsUserDownlink: true,
			},
		},
		System: &conf.SystemPolicy{
			StatsInboundUplink:    true,
			StatsInboundDownlink:  true,
			StatsOutboundUplink:   true,
			StatsOutboundDownlink: true,
		},
	}
}
