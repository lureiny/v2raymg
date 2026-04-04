package subscription

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpGet: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("httpGet: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("httpGet: read body: %w", err)
	}
	return data, nil
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
