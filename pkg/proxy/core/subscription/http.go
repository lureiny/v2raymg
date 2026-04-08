package subscription

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lureiny/v2raymg/pkg/log"
)

const (
	// httpTimeout 是外部 HTTP 请求的超时时间。
	httpTimeout = 10 * time.Second
	// maxResponseBody 是外部 HTTP 响应体的最大读取字节数（4MB）。
	maxResponseBody = 4 << 20
)

// httpGet 发起 HTTP GET 请求并返回响应体。
// 设置 Host 和 Accept 头，与旧版 proxy/sub/converter 行为一致。
func httpGet(reqURL string) ([]byte, error) {
	parsedURL, err := url.Parse(reqURL)
	if err != nil {
		return nil, fmt.Errorf("httpGet: parse url: %w", err)
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("httpGet: new request: %w", err)
	}
	req.Header.Add("Host", parsedURL.Host)
	req.Header.Add("Accept", "*/*")

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpGet: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("httpGet: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("httpGet: read body: %w", err)
	}
	return data, nil
}

// tryBase64Decode 依次尝试 StdEncoding、RawStdEncoding、URLEncoding、RawURLEncoding
// 解码 base64 字符串。解码成功后还会做启发式校验（UTF-8 合法 + 包含 "://"），
// 防止明文 URI 被 Raw 变体误判为合法 base64 后产生乱码。
// 全部失败时返回 error。
func tryBase64Decode(s string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		decoded, err := enc.DecodeString(s)
		if err == nil && looksLikeURIList(decoded) {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("not valid base64")
}

// looksLikeURIList 检查 base64 解码结果是否看起来像代理 URI 列表。
// 必须是合法 UTF-8 且包含 "://"（所有代理 URI 都含此模式）。
func looksLikeURIList(data []byte) bool {
	return utf8.Valid(data) && strings.Contains(string(data), "://")
}

// FetchExternalSub 拉取外部订阅链接并解码为 URI 列表。
// 编码格式与 CommonConverter 一致：base64(uri1\nuri2\n...)。
// 若 base64 解码失败，则按明文处理（兼容非 base64 订阅源）。
func FetchExternalSub(subURL string) ([]string, error) {
	data, err := httpGet(subURL)
	if err != nil {
		return nil, fmt.Errorf("fetch external sub: %w", err)
	}

	body := strings.TrimSpace(string(data))
	if body == "" {
		return nil, nil
	}

	// 尝试多种 base64 变体解码（订阅源常见 StdEncoding / RawStdEncoding / URLEncoding）
	decoded, err := tryBase64Decode(body)
	if err != nil {
		// 所有 base64 变体均失败，视为明文
		decoded = []byte(body)
	}

	lines := strings.Split(string(decoded), "\n")
	var uris []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			uris = append(uris, line)
		}
	}
	return uris, nil
}

// MaxExtSubs 是 ext_sub 参数允许的最大数量。
const MaxExtSubs = 10

// SanitizeURL 返回 URL 的 scheme://host 部分，隐藏 path 和 query string 中可能包含的凭据。
func SanitizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "<invalid-url>"
	}
	return u.Scheme + "://" + u.Host
}

// FetchAndMergeExtSubs 并发拉取多个外部订阅链接，返回按传入顺序合并的 URI 列表。
// 超过 MaxExtSubs 的部分会被截断。单个链接失败不影响其他。
func FetchAndMergeExtSubs(extSubs []string) (uris []string, truncated bool) {
	if len(extSubs) == 0 {
		return nil, false
	}
	if len(extSubs) > MaxExtSubs {
		extSubs = extSubs[:MaxExtSubs]
		truncated = true
	}

	results := make([][]string, len(extSubs))
	var wg sync.WaitGroup
	for i, u := range extSubs {
		wg.Add(1)
		go func(idx int, subURL string) {
			defer wg.Done()
			fetched, err := FetchExternalSub(subURL)
			if err != nil {
				log.Warn("fetch external sub failed", "host", SanitizeURL(subURL), "err", err)
				return
			}
			log.Info("[FetchAndMergeExtSubs] external sub URIs", "host", SanitizeURL(subURL), "count", len(fetched))
			results[idx] = fetched
		}(i, u)
	}
	wg.Wait()

	for _, r := range results {
		uris = append(uris, r...)
	}
	return uris, truncated
}

// --- URL 配置解析函数 ---

// FetchProxyGroupsFromURL 从 URL 获取 proxy-groups 配置
// 每行格式同 proxy_group 参数
func FetchProxyGroupsFromURL(url string) ([]ProxyGroupConfig, error) {
	data, err := httpGet(url)
	if err != nil {
		return nil, fmt.Errorf("fetch proxy_groups_url: %w", err)
	}

	var configs []ProxyGroupConfig
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		config, err := ParseProxyGroupParam(line)
		if err != nil {
			continue // 忽略解析错误的行
		}
		configs = append(configs, *config)
	}
	return configs, nil
}

// FetchRuleProvidersFromURL 从 URL 获取 rule-providers 配置
// 每行格式同 rule_provider 参数
func FetchRuleProvidersFromURL(url string) ([]RuleProviderConfig, error) {
	data, err := httpGet(url)
	if err != nil {
		return nil, fmt.Errorf("fetch rule_providers_url: %w", err)
	}

	var configs []RuleProviderConfig
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		config, err := ParseRuleProviderParam(line)
		if err != nil {
			continue
		}
		configs = append(configs, *config)
	}
	return configs, nil
}

// FetchRulesFromURL 从 URL 获取 rules 配置
// 每行格式同 rule 参数
func FetchRulesFromURL(url string) ([]RuleConfig, error) {
	data, err := httpGet(url)
	if err != nil {
		return nil, fmt.Errorf("fetch rules_url: %w", err)
	}

	var configs []RuleConfig
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		config, err := ParseRuleParam(line)
		if err != nil {
			continue
		}
		configs = append(configs, *config)
	}
	return configs, nil
}
