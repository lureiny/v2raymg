//go:build v2ray

package sub

import (
	"strings"

	"github.com/v2fly/v2ray-core/v5/infra/conf/v4"
)

type VlessTLSConfig struct {
	SNI  string
	ALPN string
}

func (c *VlessTLSConfig) Build() string {
	params := []string{}
	if len(c.SNI) > 0 {
		params = append(params, "sni="+c.SNI)
	}
	if len(c.ALPN) > 0 {
		params = append(params, "alpn="+c.ALPN)
	}
	return strings.Join(params, "&")
}

func newTLSOrXTLSConfig(s *v4.StreamConfig) *VlessTLSConfig {
	tlsConfig := &VlessTLSConfig{}
	if s.TLSSettings != nil {
		tlsConfig.ALPN = strings.Join(*s.TLSSettings.ALPN, ",")
		tlsConfig.SNI = s.TLSSettings.ServerName
	}
	return tlsConfig
}
