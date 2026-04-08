package subscription

import (
	"fmt"
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
		uris, err := FetchExternalSub(url)
		if err != nil {
			errs = append(errs, fmt.Sprintf("[%s]: %v", url, err))
			continue
		}
		for _, uri := range uris {
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
