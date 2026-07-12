package converter

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
)

const (
	// templateTimeout bounds a Clash template fetch (connect + body read).
	templateTimeout = 15 * time.Second
	// maxTemplateBody caps a template response body (8MB).
	maxTemplateBody = 8 << 20
)

// httpGet 发起 HTTP GET 请求并返回响应体。
// 设置 Host 和 Accept 头，与旧版 proxy/sub/converter 行为一致。
func httpGet(reqURL string) ([]byte, error) {
	if err := subscription.ValidateOutboundURL(reqURL); err != nil {
		return nil, fmt.Errorf("httpGet: %w", err)
	}
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

	// SafeHTTPClient rejects connections to non-public IPs at dial time; adds a
	// timeout and body cap the previous default client lacked.
	client := subscription.SafeHTTPClient(templateTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpGet: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("httpGet: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTemplateBody))
	if err != nil {
		return nil, fmt.Errorf("httpGet: read body: %w", err)
	}
	return data, nil
}
