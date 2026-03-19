package subscription

// ClientFormat 表示订阅输出的客户端格式类型
type ClientFormat string

const (
	// FormatCommon 通用格式（默认）
	FormatCommon ClientFormat = "common"
	// FormatClash Clash 客户端格式
	FormatClash ClientFormat = "clash"
	// FormatSurge Surge 客户端格式
	FormatSurge ClientFormat = "surge"
	// FormatQv2ray Qv2ray 客户端格式
	FormatQv2ray ClientFormat = "qv2ray"
)
