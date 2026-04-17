package converter

import (
	"fmt"
	"strings"
)

// Helpers for reading values out of SubscriptionSpec.Extensions (map[string]any).
// Shared by all converters (clash, surge, ...).

func extString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func extInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case uint32:
			return int(val)
		case float64:
			return int(val)
		}
	}
	return 0
}

// extStringOrJoinSlice returns the value for key as a string: either the raw
// string value, or a comma-joined form if the underlying value is []string /
// []any. The []any branch covers values that arrive via YAML unmarshal into a
// generic map (YAML does not produce []string directly).
func extStringOrJoinSlice(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []string:
		return strings.Join(val, ",")
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, ",")
	}
	return ""
}
