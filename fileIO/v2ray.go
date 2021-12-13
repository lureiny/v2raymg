package fileIO

import (
	"encoding/json"

	"github.com/v2fly/v2ray-core/v4/infra/conf"
	"github.com/v2fly/v2ray-core/v4/infra/conf/cfgcommon"
)

type V2rayConfig struct {
	LogConfig        *conf.LogConfig              `json:"log"`
	RouterConfig     *conf.RouterConfig           `json:"routing"`
	DNSConfig        *conf.DNSConfig              `json:"dns"`
	InboundConfigs   []InboundDetourConfig        `json:"inbounds"`
	OutboundConfigs  []conf.OutboundDetourConfig  `json:"outbounds"`
	Transport        *conf.TransportConfig        `json:"transport"`
	Policy           *conf.PolicyConfig           `json:"policy"`
	API              *conf.APIConfig              `json:"api"`
	Stats            *conf.StatsConfig            `json:"stats"`
	Reverse          *conf.ReverseConfig          `json:"reverse"`
	FakeDNS          *conf.FakeDNSConfig          `json:"fakeDns"`
	BrowserForwarder *conf.BrowserForwarderConfig `json:"browserForwarder"`
	Observatory      *conf.ObservatoryConfig      `json:"observatory"`

	Services map[string]*json.RawMessage `json:"services"`
}

// TODO: 支持范围端口
// type PortRangeString string

type InboundDetourConfig struct {
	Protocol       string                              `json:"protocol"`
	PortRange      uint32                              `json:"port"`
	ListenOn       string                              `json:"listen"`
	Settings       *json.RawMessage                    `json:"settings"`
	Tag            string                              `json:"tag"`
	Allocation     *conf.InboundDetourAllocationConfig `json:"allocate"`
	StreamSetting  *conf.StreamConfig                  `json:"streamSettings"`
	DomainOverride *cfgcommon.StringList               `json:"domainOverride"`
	SniffingConfig *conf.SniffingConfig                `json:"sniffing"`
}

type V2rayInboundUser struct {
	Email   string `json:"email"`
	ID      string `json:"id"`
	Level   uint32 `json:"level,omitempty"`
	AlterID uint32 `json:"alterId,omitempty"`
}
