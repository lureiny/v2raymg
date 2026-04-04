package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type HttpCallback func(*http.Response, error) error

// setAuthHeader sets the appropriate auth header.
// If token starts with "Bearer " it is used as-is via Authorization header;
// otherwise it is treated as a static X-Token.
func setAuthHeader(req *http.Request, token string) {
	if strings.HasPrefix(token, "Bearer ") {
		req.Header.Set("Authorization", token)
	} else if token != "" {
		req.Header.Set("X-Token", token)
	}
}

func DoGetRequest(reqUrl string, token string, params map[string]interface{}, cb HttpCallback) error {
	client := http.Client{
		Timeout: 30 * time.Second,
	}
	p := url.Values{}
	for k, v := range params {
		p.Add(k, fmt.Sprintf("%v", v))
	}
	rawUrl := fmt.Sprintf("%s?%s", reqUrl, p.Encode())
	parsedUrl, err := url.Parse(rawUrl)
	if err != nil {
		return err
	}
	parsedUrl.Path = filepath.Clean(parsedUrl.Path)
	req, err := http.NewRequest("GET", parsedUrl.String(), nil)
	if err != nil {
		return err
	}
	setAuthHeader(req, token)
	return cb(client.Do(req))
}

func DoPostRequest(reqUrl string, token string, body interface{}, cb HttpCallback) error {
	return doJSONRequest("POST", reqUrl, token, body, cb)
}

func DoPutRequest(reqUrl string, token string, body interface{}, cb HttpCallback) error {
	return doJSONRequest("PUT", reqUrl, token, body, cb)
}

func DoDeleteRequest(reqUrl string, token string, body interface{}, cb HttpCallback) error {
	return doJSONRequest("DELETE", reqUrl, token, body, cb)
}

func doJSONRequest(method, reqUrl string, token string, body interface{}, cb HttpCallback) error {
	client := http.Client{
		Timeout: 30 * time.Second,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	parsedUrl, err := url.Parse(reqUrl)
	if err != nil {
		return err
	}
	parsedUrl.Path = filepath.Clean(parsedUrl.Path)
	req, err := http.NewRequest(method, parsedUrl.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthHeader(req, token)
	return cb(client.Do(req))
}
