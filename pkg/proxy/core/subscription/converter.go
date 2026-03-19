package subscription

import (
	"sync"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// Converter 定义订阅格式转换器接口。
// 不同客户端（Clash、Surge、Qv2ray 等）实现此接口。
type Converter interface {
	// Format 返回转换器支持的客户端格式。
	Format() ClientFormat

	// Convert 将 SubscriptionSpec 列表转换为客户端特定的订阅格式字符串。
	Convert(specs []contracts.SubscriptionSpec) (string, error)
}

// registry 存储已注册的 Converter 实例
var (
	registryMu sync.RWMutex
	registry   = make(map[ClientFormat]Converter)
)

// RegisterConverter 注册一个 Converter。
// 如果同名格式已存在，将覆盖。
func RegisterConverter(c Converter) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[c.Format()] = c
}

// GetConverter 根据格式获取对应的 Converter。
// 如果未找到，返回 nil 和 false。
func GetConverter(format ClientFormat) (Converter, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	c, ok := registry[format]
	return c, ok
}

// GetConverterOrDefault 根据格式获取 Converter，未找到时返回 common 格式。
func GetConverterOrDefault(format ClientFormat) Converter {
	if c, ok := GetConverter(format); ok {
		return c
	}
	// 回退到 common 格式
	if c, ok := GetConverter(FormatCommon); ok {
		return c
	}
	return nil
}
