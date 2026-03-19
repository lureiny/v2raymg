package subscription

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// RemoteFetcher 从远程 URL 列表拉取订阅并合并为 SubscriptionSpec 列表。
type RemoteFetcher struct {
	urls []string
}

// NewRemoteFetcher 创建一个 RemoteFetcher。
// urls 是远程订阅地址列表，每个 URL 应返回 base64 编码的 URI 列表（common 格式）。
func NewRemoteFetcher(urls []string) *RemoteFetcher {
	return &RemoteFetcher{urls: urls}
}

// Fetch 从所有配置的远程 URL 拉取订阅，合并返回 SubscriptionSpec 列表。
// 单个 URL 失败不影响其他 URL 的拉取，错误会被累积后一并返回。
// 返回的 spec 仅填充 URI 字段，协议字段由调用方根据 URI 前缀判断。
func (f *RemoteFetcher) Fetch() ([]contracts.SubscriptionSpec, error) {
	var specs []contracts.SubscriptionSpec
	var errs []string

	for _, url := range f.urls {
		uris, err := fetchURIsFromURL(url)
		if err != nil {
			errs = append(errs, fmt.Sprintf("[%s]: %v", url, err))
			continue
		}
		for _, uri := range uris {
			uri = strings.TrimSpace(uri)
			if uri == "" {
				continue
			}
			specs = append(specs, contracts.SubscriptionSpec{
				URI: uri,
			})
		}
	}

	if len(errs) > 0 && len(specs) == 0 {
		return nil, fmt.Errorf("all remote subscription sources failed: %s", strings.Join(errs, "; "))
	}
	return specs, nil
}

// fetchURIsFromURL 从单个 URL 拉取订阅内容，base64 解码后返回 URI 列表。
// 如果内容不是合法的 base64，则原样按行分割返回（兼容未编码格式）。
func fetchURIsFromURL(rawURL string) ([]string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "v2raymg/1.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// 尝试 base64 解码（standard encoding，与旧版保持一致）
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		// 不是 base64，原样按行返回
		return strings.Split(string(data), "\n"), nil
	}
	return strings.Split(string(decoded), "\n"), nil
}
