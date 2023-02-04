//go:build v2ray

package manager

import (
	"encoding/json"

	"github.com/lureiny/v2raymg/proxy/protocol"
	conf "github.com/v2fly/v2ray-core/v5/infra/conf/v4"
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
