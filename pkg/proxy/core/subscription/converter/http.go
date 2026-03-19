package converter

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
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
