package converter

import (
	"encoding/base64"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
)

// CommonConverter 实现通用订阅格式转换器。
// 输出格式：所有 URI 用换行符连接后 base64 编码。
type CommonConverter struct{}

// Format 返回 FormatCommon。
func (c *CommonConverter) Format() subscription.ClientFormat {
	return subscription.FormatCommon
}

// Convert 将所有 spec.URI 统一命名并去重后，用换行符连接并 base64 编码。
func (c *CommonConverter) Convert(specs []contracts.SubscriptionSpec) (string, error) {
	uris := renderNamedURIs(specs)
	joined := strings.Join(uris, "\n")
	return base64.StdEncoding.EncodeToString([]byte(joined)), nil
}

func init() {
	Register(&CommonConverter{})
}
