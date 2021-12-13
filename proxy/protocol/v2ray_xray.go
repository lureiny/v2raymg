//go:build !v2ray

package protocol

import (
	"encoding/json"

	"github.com/xtls/xray-core/infra/conf"
)

type V2rayConfig struct {
	LogConfig       *conf.LogConfig             `json:"log"`
	RouterConfig    *conf.RouterConfig          `json:"routing"`
	DNSConfig       *conf.DNSConfig             `json:"dns"`
	InboundConfigs  []InboundDetourConfig       `json:"inbounds"`
	OutboundConfigs []conf.OutboundDetourConfig `json:"outbounds"`
	Transport       *conf.TransportConfig       `json:"transport"`
	Policy          *conf.PolicyConfig          `json:"policy"`
	API             *conf.APIConfig             `json:"api"`
	Stats           *conf.StatsConfig           `json:"stats"`
	Reverse         *conf.ReverseConfig         `json:"reverse"`
	FakeDNS         *conf.FakeDNSConfig         `json:"fakeDns"`
	Observatory     *conf.ObservatoryConfig     `json:"observatory"`

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
	DomainOverride *conf.StringList                    `json:"domainOverride"`
	SniffingConfig *conf.SniffingConfig                `json:"sniffing"`
}

type V2rayInboundUser struct {
	Email   string `json:"email"`
	ID      string `json:"id"`
	Level   uint32 `json:"level,omitempty"`
	AlterID uint32 `json:"alterId,omitempty"`
}

func NewStreamSetting(network, tls, keyFile, certFile string) (*conf.StreamConfig, error) {
	t := conf.TransportProtocol(network)
	streamConfig := &conf.StreamConfig{
		Network:  &t,
		Security: tls,
		TLSSettings: &conf.TLSConfig{
			Certs: []*conf.TLSCertConfig{
				&conf.TLSCertConfig{
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
	Clients    []json.RawMessage            `json:"clients"`
	Decryption string                       `json:"decryption"`
	Fallbacks  []*conf.VLessInboundFallback `json:"fallbacks"`
}
