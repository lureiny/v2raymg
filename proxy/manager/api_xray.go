//go:build !v2ray

package manager

import (
	"encoding/json"

	"github.com/lureiny/v2raymg/proxy/config"
	"github.com/xtls/xray-core/infra/conf"
)

func configAllApiInfo(c *config.V2rayConfig) {
	c.Stats = &conf.StatsConfig{}
	configApi(c)
	configRoute(c)
	configPolicy(c)
}

func configApi(c *config.V2rayConfig) {
	c.API = &conf.APIConfig{
		Tag: apiTag,
		Services: []string{
			"HandlerService",
			"LoggerService",
			"StatsService",
		},
	}
}

func configRoute(c *config.V2rayConfig) {
	c.RouterConfig.RuleList = append(
		c.RouterConfig.RuleList,
		json.RawMessage(`{
			"inboundTag": [
				"api"
			],
			"outboundTag": "api",
			"type": "field"
		}`),
	)
}

func configPolicy(c *config.V2rayConfig) {
	c.Policy = &conf.PolicyConfig{
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
