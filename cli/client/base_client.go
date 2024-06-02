package client

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"time"
)

type HttpCallback func(*http.Response, error) error

func DoGetRequest(reqUrl string, params, headers map[string]interface{}, cb HttpCallback) error {
	client := http.Client{
		Timeout: 30 * time.Second,
	}
	p := url.Values{}
	for k, v := range params {
		p.Add(k, fmt.Sprintf("%v", v))
	}
	rawUrl := fmt.Sprintf("%s?%s", reqUrl, p.Encode())
	// 标准化url
	parsedUrl, err := url.Parse(rawUrl)
	if err != nil {
		return err
	}
	parsedUrl.Path = filepath.Clean(parsedUrl.Path)
	req, err := http.NewRequest("GET", parsedUrl.String(), nil)
	if err != nil {
		return err
	}
	return cb(client.Do(req))
}
