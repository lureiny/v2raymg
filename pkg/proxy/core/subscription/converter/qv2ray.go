package converter

import (
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
)

// Qv2rayConverter 实现 Qv2ray 订阅格式转换器。
// 输出格式：所有 URI 用换行符连接，不做额外编码。
type Qv2rayConverter struct{}

// Format 返回 FormatQv2ray。
func (c *Qv2rayConverter) Format() subscription.ClientFormat {
	return subscription.FormatQv2ray
}

// Convert 将所有 spec.URI 用换行符连接，不做额外编码。
func (c *Qv2rayConverter) Convert(specs []contracts.SubscriptionSpec) (string, error) {
	var uris []string
	for _, spec := range specs {
		if spec.URI != "" {
			uris = append(uris, spec.URI)
		}
	}
	return strings.Join(uris, "\n"), nil
}

func init() {
	Register(&Qv2rayConverter{})
}
