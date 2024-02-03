//go:build !v2ray

package sub

import (
	"strings"

	"github.com/xtls/xray-core/infra/conf"
	"golang.org/x/crypto/curve25519"
)

const (
	defaultFingerPrint = "chrome"
)

type VlessTLSConfig struct {
	SNI  string
	ALPN string
	FP   string
	PBK  string
}

func (c *VlessTLSConfig) Build() string {
	params := []string{}
	if len(c.SNI) > 0 {
		params = append(params, "sni="+c.SNI)
	}
	if len(c.ALPN) > 0 {
		params = append(params, "alpn="+c.ALPN)
	}
	if len(c.FP) > 0 {
		params = append(params, "fp="+c.FP)
	}
	if len(c.PBK) > 0 {
		params = append(params, "pbk="+c.PBK)
	}
	return strings.Join(params, "&")
}

func newTLSOrRealityConfig(s *conf.StreamConfig) *VlessTLSConfig {
	tlsConfig := &VlessTLSConfig{}
	switch strings.ToLower(s.Security) {
	case "tls":
		if s.TLSSettings != nil {
			if s.TLSSettings.ALPN != nil {
				tlsConfig.ALPN = strings.Join(*s.TLSSettings.ALPN, ",")
			}
			tlsConfig.SNI = s.TLSSettings.ServerName
		}
	case "reality":
		tlsConfig.FP = defaultFingerPrint
		pbk, _ := curve25519.X25519([]byte(s.REALITYSettings.PrivateKey), curve25519.Basepoint)
		tlsConfig.PBK = string(pbk)
	}
	return tlsConfig
}
