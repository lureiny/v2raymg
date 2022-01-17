package sub

import (
	"strings"

	"github.com/v2fly/v2ray-core/v4/infra/conf"
)

type VlessTLSConfig struct {
	SNI  string
	ALPN string
	Flow string // xtls的流控方式
}

func (c *VlessTLSConfig) Build() string {
	params := []string{}
	if len(c.SNI) > 0 {
		params = append(params, "sni="+c.SNI)
	}
	if len(c.ALPN) > 0 {
		params = append(params, "alpn="+c.ALPN)
	}
	if len(c.Flow) > 0 {
		params = append(params, "flow="+c.Flow)
	}
	return strings.Join(params, "&")
}

func newTLSConfig(t *conf.TLSConfig) *VlessTLSConfig {
	tlsConfig := &VlessTLSConfig{
		Flow: "",
	}
	if t != nil {
		tlsConfig.ALPN = strings.Join(*t.ALPN, ",")
		tlsConfig.SNI = t.ServerName
	}
	return tlsConfig
}
