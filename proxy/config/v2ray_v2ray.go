//go:build v2ray

package config

import (
	"encoding/json"

	"github.com/v2fly/v2ray-core/v5/infra/conf/cfgcommon"
	"github.com/v2fly/v2ray-core/v5/infra/conf/cfgcommon/sniffer"
	"github.com/v2fly/v2ray-core/v5/infra/conf/cfgcommon/tlscfg"
	"github.com/v2fly/v2ray-core/v5/infra/conf/synthetic/dns"
	"github.com/v2fly/v2ray-core/v5/infra/conf/synthetic/log"
	"github.com/v2fly/v2ray-core/v5/infra/conf/synthetic/router"
	"github.com/v2fly/v2ray-core/v5/infra/conf/v4"
)

type V2rayConfig struct {
	LogConfig        *log.LogConfig             `json:"log"`
	RouterConfig     *router.RouterConfig       `json:"routing"`
	DNSConfig        *dns.DNSConfig             `json:"dns"`
	InboundConfigs   []InboundDetourConfig      `json:"inbounds"`
	OutboundConfigs  []v4.OutboundDetourConfig  `json:"outbounds"`
	Transport        *v4.TransportConfig        `json:"transport"`
	Policy           *v4.PolicyConfig           `json:"policy"`
	API              *v4.APIConfig              `json:"api"`
	Stats            *v4.StatsConfig            `json:"stats"`
	Reverse          *v4.ReverseConfig          `json:"reverse"`
	FakeDNS          *v4.FakeDNSConfig          `json:"fakeDns"`
	BrowserForwarder *v4.BrowserForwarderConfig `json:"browserForwarder"`
	Observatory      *v4.ObservatoryConfig      `json:"observatory"`

	Services map[string]*json.RawMessage `json:"services"`
}

// TODO: 支持范围端口
// type PortRangeString string

type InboundDetourConfig struct {
	Protocol       string                            `json:"protocol"`
	PortRange      uint32                            `json:"port"`
	ListenOn       string                            `json:"listen"`
	Settings       *json.RawMessage                  `json:"settings"`
	Tag            string                            `json:"tag"`
	Allocation     *v4.InboundDetourAllocationConfig `json:"allocate"`
	StreamSetting  *v4.StreamConfig                  `json:"streamSettings"`
	DomainOverride *cfgcommon.StringList             `json:"domainOverride"`
	SniffingConfig *sniffer.SniffingConfig           `json:"sniffing"`
}

type V2rayInboundUser struct {
	Email   string `json:"email"`
	ID      string `json:"id"`
	Level   uint32 `json:"level,omitempty"`
	AlterID uint32 `json:"alterId,omitempty"`
}

func NewStreamSetting(network, tls, keyFile, certFile string) (*v4.StreamConfig, error) {
	t := v4.TransportProtocol(network)
	streamConfig := &v4.StreamConfig{
		Network:  &t,
		Security: tls,
		TLSSettings: &tlscfg.TLSConfig{
			Certs: []*tlscfg.TLSCertConfig{
				&tlscfg.TLSCertConfig{
					CertFile: certFile,
					KeyFile:  keyFile,
				},
			},
		},
	}
	return streamConfig, nil
}

func NewInboundDetourConfig(protocol, listenon, tag string, port uint32) (*InboundDetourConfig, error) {
	inboundConfig := &InboundDetourConfig{
		Protocol:  protocol,
		ListenOn:  listenon,
		Tag:       tag,
		PortRange: port,
	}

	return inboundConfig, nil
}

type VLessInboundConfig struct {
	Clients    []json.RawMessage          `json:"clients"`
	Decryption string                     `json:"decryption"`
	Fallbacks  []*v4.VLessInboundFallback `json:"fallbacks"`
}
