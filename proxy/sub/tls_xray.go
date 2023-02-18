//go:build !v2ray

package sub

import (
	"strings"

	"github.com/xtls/xray-core/infra/conf"
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

func newTLSOrXTLSConfig(s *conf.StreamConfig) *VlessTLSConfig {
	tlsConfig := &VlessTLSConfig{}
	switch strings.ToLower(s.Security) {
	case "tls":
		if s.TLSSettings != nil {
			if s.TLSSettings.ALPN != nil {
				tlsConfig.ALPN = strings.Join(*s.TLSSettings.ALPN, ",")
			}
			tlsConfig.SNI = s.TLSSettings.ServerName
		}
	case "xtls":
		if s.XTLSSettings != nil {
			if s.XTLSSettings.ALPN != nil {
				tlsConfig.ALPN = strings.Join(*s.XTLSSettings.ALPN, ",")
			}
			tlsConfig.SNI = s.XTLSSettings.ServerName
		}
	}
	return tlsConfig
}
