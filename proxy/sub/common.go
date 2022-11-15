package sub

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// for vless and trojan
type BaseConfig struct {
	UUID       string // trojan: passwd vless: uuid
	RemoteHost string
	RemotePort uint32
	Flow       string // for xtls
}

func (c *BaseConfig) Build() string {
	return fmt.Sprintf("%s@%s:%d", url.QueryEscape(c.UUID), c.RemoteHost, c.RemotePort)
}

// 修复生成uri, 例如存在&&的问题
func fixUri(uri string) string {
	return strings.ReplaceAll(uri, "&&", "&")
}

func parseHost(configHost, sni string) string {
	if ip := net.ParseIP(configHost); sni == "" || ip != nil {
		return configHost
	}
	// 解析域名
	ips, err := net.LookupHost(configHost)
	if err != nil {
		return configHost
	}
	return ips[0]
}
