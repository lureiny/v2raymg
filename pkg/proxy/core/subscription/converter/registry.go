package converter

import (
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
)

// Register 注册一个 Converter 到全局 registry。
// 如果同名格式已存在，将覆盖。
func Register(c subscription.Converter) {
	subscription.RegisterConverter(c)
}

// Get 根据格式获取对应的 Converter。
// 如果未找到，返回 nil 和 false。
func Get(format subscription.ClientFormat) (subscription.Converter, bool) {
	return subscription.GetConverter(format)
}

// GetOrDefault 根据格式获取 Converter，未找到时返回 common 格式。
func GetOrDefault(format subscription.ClientFormat) subscription.Converter {
	return subscription.GetConverterOrDefault(format)
}
