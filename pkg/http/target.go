package http

import "github.com/gin-gonic/gin"

type TargetOption func(*targetConfig)

type targetConfig struct {
	defaultValue string
}

func WithDefault(val string) TargetOption {
	return func(c *targetConfig) { c.defaultValue = val }
}

// getTargetFromQuery 从 query 参数获取 target，不存在或为空时返回默认值（默认 "all"）
func getTargetFromQuery(c *gin.Context, opts ...TargetOption) string {
	cfg := targetConfig{defaultValue: "all"}
	for _, o := range opts {
		o(&cfg)
	}
	target, exists := c.GetQuery("target")
	if !exists || target == "" {
		return cfg.defaultValue
	}
	return target
}

// resolveTarget 检查 target 是否为空，为空时返回当前节点名（用于写操作）
func resolveTarget(target string, nodeName string) string {
	if target == "" {
		return nodeName
	}
	return target
}
