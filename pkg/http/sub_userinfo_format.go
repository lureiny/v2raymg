package http

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
)

// DefaultSubUserInfoFormat is the built-in fallback Subscription-Userinfo
// header schema. Used when neither the API ?sub_userinfo_format= query nor
// the node config subscription.userinfo_header_format provides one.
const DefaultSubUserInfoFormat = "upload=${upload}; download=${download}; total=${total}; expire=${expire}"

const (
	bytesPerKB int64 = 1024
	bytesPerMB int64 = 1024 * 1024
	bytesPerGB int64 = 1024 * 1024 * 1024
)

// formatFloat2dp formats a float with up to 2 decimal places, stripping
// trailing zeros and a trailing decimal point so 1.50 → "1.5", 1.00 → "1".
func formatFloat2dp(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

// formatUnit renders v as float(v/divisor) using formatFloat2dp.
func formatUnit(v int64, divisor int64) string {
	return formatFloat2dp(float64(v) / float64(divisor))
}

// autoUnit picks the largest unit such that the displayed number is ≥1
// (or "B" when v < 1KB). Bytes are rendered as integers; KB/MB/GB use
// formatFloat2dp. Boundaries are inclusive on the lower side.
func autoUnit(v int64) string {
	switch {
	case v < bytesPerKB:
		return fmt.Sprintf("%d B", v)
	case v < bytesPerMB:
		return formatUnit(v, bytesPerKB) + " KB"
	case v < bytesPerGB:
		return formatUnit(v, bytesPerMB) + " MB"
	default:
		return formatUnit(v, bytesPerGB) + " GB"
	}
}

// subUserInfoVars is the data input to the format renderer.
type subUserInfoVars struct {
	Username   string
	Upload     int64
	Download   int64
	Total      int64
	ExpireUnix int64
	// ExpiryTime is the raw user expiry; when IsZero() expire_string = "never".
	// Otherwise it is rendered in the access node's local timezone.
	ExpiryTime time.Time
}

// buildSubUserInfoVarMap turns subUserInfoVars into the ${var} substitution
// table for os.Expand.
func buildSubUserInfoVarMap(in subUserInfoVars) map[string]string {
	m := map[string]string{
		"username": in.Username,

		"upload":      strconv.FormatInt(in.Upload, 10),
		"upload_kb":   formatUnit(in.Upload, bytesPerKB),
		"upload_mb":   formatUnit(in.Upload, bytesPerMB),
		"upload_gb":   formatUnit(in.Upload, bytesPerGB),
		"upload_auto": autoUnit(in.Upload),

		"download":      strconv.FormatInt(in.Download, 10),
		"download_kb":   formatUnit(in.Download, bytesPerKB),
		"download_mb":   formatUnit(in.Download, bytesPerMB),
		"download_gb":   formatUnit(in.Download, bytesPerGB),
		"download_auto": autoUnit(in.Download),

		"total_kb":   formatUnit(in.Total, bytesPerKB),
		"total_mb":   formatUnit(in.Total, bytesPerMB),
		"total_gb":   formatUnit(in.Total, bytesPerGB),
		"total_auto": autoUnit(in.Total),
	}
	// total: non-positive values (e.g. user has no traffic limit) collapse
	// to the sentinel "-1", matching the subscription-userinfo convention
	// used by Clash/sing-box for "unlimited". Only the raw `total` variable
	// is sentineled — the unit variants (total_kb/_mb/_gb/_auto) keep their
	// calculated values so unit suffixes stay meaningful.
	if in.Total <= 0 {
		m["total"] = "-1"
	} else {
		m["total"] = strconv.FormatInt(in.Total, 10)
	}
	// expire: same convention. Only `expire` is sentineled; expire_string
	// uses the human-readable "never" instead.
	if in.ExpireUnix <= 0 {
		m["expire"] = "-1"
	} else {
		m["expire"] = strconv.FormatInt(in.ExpireUnix, 10)
	}
	if in.ExpiryTime.IsZero() {
		m["expire_string"] = "never"
	} else {
		m["expire_string"] = in.ExpiryTime.Local().Format("2006-01-02 15:04:05")
	}
	return m
}

// renderSubUserInfoFormat expands ${var} references in format using vars.
// Unknown variables become empty strings; each unknown is logged once via
// log.Warn so misconfiguration is visible without breaking the response.
func renderSubUserInfoFormat(format string, vars map[string]string) string {
	return os.Expand(format, func(key string) string {
		if v, ok := vars[key]; ok {
			return v
		}
		log.Warn("sub_userinfo_format: unknown variable", "var", key)
		return ""
	})
}

// resolveSubUserInfoFormat picks the effective format string per the
// override chain: explicit query param > node config > built-in default.
// queryFormat and configFormat are both trimmed of surrounding whitespace
// before checking for emptiness.
func resolveSubUserInfoFormat(queryFormat, configFormat string) string {
	if s := strings.TrimSpace(queryFormat); s != "" {
		return s
	}
	if s := strings.TrimSpace(configFormat); s != "" {
		return s
	}
	return DefaultSubUserInfoFormat
}

// isClashClient returns true when the request's format hint (client query
// param or User-Agent) names a Clash-family client. Matched case-insensitive
// on "clash" as a substring, which covers Clash, ClashX, Clash.Meta, mihomo
// (mihomo's UA usually still contains "Clash"), etc.
func isClashClient(formatHint string) bool {
	return strings.Contains(strings.ToLower(formatHint), "clash")
}

// stripClashEmptyFields removes the standardized subscription-userinfo
// `total=...` and/or `expire=...` segments (delimited by `;`) from a
// rendered header. Used only for Clash-family clients, which prefer the
// fields absent over a `-1` sentinel.
//
// Only segments whose key (case-sensitive, trimmed) equals exactly
// "total" or "expire" are stripped. Custom-key schemas like
// `quota=${total}` are intentionally left untouched — the rule only
// applies to the conventional field names.
func stripClashEmptyFields(header string, dropTotal, dropExpire bool) string {
	if !dropTotal && !dropExpire {
		return header
	}
	parts := strings.Split(header, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		eq := strings.IndexByte(s, '=')
		if eq > 0 {
			key := strings.TrimSpace(s[:eq])
			if (dropTotal && key == "total") || (dropExpire && key == "expire") {
				continue
			}
		}
		out = append(out, s)
	}
	return strings.Join(out, "; ")
}
