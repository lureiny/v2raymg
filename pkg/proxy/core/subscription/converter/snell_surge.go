package converter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// convertSnell 转换 Snell 为 Surge 格式。
// 格式: name = snell, host, port, psk=password, version=5
// TODO: 当 obfs 支持实现后，在此添加 obfs=http 参数
// 格式：ProxyName = snell, host, port, psk=xxx, version=5, obfs=http
func (c *SurgeConverter) convertSnell(spec contracts.SubscriptionSpec) string {
	name := c.nodeName(spec, "SNELL")
	parts := []string{
		fmt.Sprintf("%s = snell", name),
		spec.Host,
		strconv.FormatUint(uint64(spec.Port), 10),
		fmt.Sprintf("psk=%s", spec.Password),
		"version=5",
	}

	return strings.Join(parts, ", ")
}
