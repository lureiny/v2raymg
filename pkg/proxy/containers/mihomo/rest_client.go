package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultRESTTimeout = 10 * time.Second
	// maxErrorBodyBytes caps how much of a non-2xx response body we read
	// when extracting the mihomo error message. The response is advisory
	// diagnostic text — a misbehaving server should not OOM us.
	maxErrorBodyBytes = 1024
	// maxConfigsBodyBytes caps the GET /configs response we buffer. mihomo's
	// General payload is normally well under 1KB; 64KB leaves headroom for
	// future schema additions while keeping a bounded upper limit.
	maxConfigsBodyBytes = 64 * 1024
)

// RESTClient talks to the mihomo external-controller REST API.
//
// The wire contract was verified against mihomo Alpha branch (see
// memory reference_mihomo_protocol_facts.md):
//   - Bearer <secret> when Secret != "" (skipped entirely when empty —
//     mihomo disables auth middleware in that mode).
//   - Non-2xx responses carry a JSON body with shape {"message":"..."}
//     (hub/route/errors.go HTTPError).
//   - Mutating endpoints return 204 No Content on success.
type RESTClient struct {
	baseURL    string
	secret     string
	httpClient *http.Client
}

// NewRESTClient builds a RESTClient targeting the given base URL.
// baseURL must include scheme, e.g. "http://127.0.0.1:9090". Any trailing
// slash is stripped so "http://x/" and "http://x" behave identically — mihomo's
// chi router doesn't normalise doubled separators reliably.
func NewRESTClient(baseURL, secret string) *RESTClient {
	return &RESTClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		secret:     secret,
		httpClient: &http.Client{Timeout: defaultRESTTimeout},
	}
}

type versionResponse struct {
	Version string `json:"version"`
	Meta    bool   `json:"meta"`
}

type mihomoErrorBody struct {
	Message string `json:"message"`
}

// do sends a request and returns the response on 2xx. On non-2xx it parses
// mihomo's HTTPError body for diagnostics, closes the response, and returns
// a formatted error. On 2xx the caller owns resp.Body and must close it.
//
// body: if non-nil, JSON-marshaled and sent with Content-Type: application/json.
// op:   short tag embedded in error messages, e.g. "GET /version".
func (c *RESTClient) do(ctx context.Context, method, url, op string, body any) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("mihomo rest: %s: marshal body: %w", op, err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("mihomo rest: %s: build request: %w", op, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mihomo rest: %s: %w", op, err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}

	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("mihomo rest: %s: unauthorized (check secret)", op)
	}
	if msg := parseErrorBody(resp.Body); msg != "" {
		return nil, fmt.Errorf("mihomo rest: %s: HTTP %d: %s", op, resp.StatusCode, msg)
	}
	return nil, fmt.Errorf("mihomo rest: %s: HTTP %d", op, resp.StatusCode)
}

// parseErrorBody extracts mihomo's {"message":"..."} field from an error
// response. Falls back to the raw (trimmed, truncated) text when the body
// isn't JSON or has no message field. Returns "" when body is empty.
func parseErrorBody(body io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(body, maxErrorBodyBytes))
	if err != nil || len(data) == 0 {
		return ""
	}
	var e mihomoErrorBody
	if json.Unmarshal(data, &e) == nil && e.Message != "" {
		return e.Message
	}
	s := string(bytes.TrimSpace(data))
	if len(s) > 256 {
		s = s[:256]
	}
	return s
}

// GetVersion fetches the running mihomo version via GET /version.
func (c *RESTClient) GetVersion(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, c.baseURL+"/version", "GET /version", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var v versionResponse
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", fmt.Errorf("mihomo rest: GET /version: decode body: %w", err)
	}
	return v.Version, nil
}

// GetConfigs fetches mihomo's current general configuration as raw JSON.
// Named listeners are not included in this payload — use it for log-level,
// mode, and other singleton fields, or for diagnostics.
//
// The response is capped at maxConfigsBodyBytes; an oversize body returns an
// error instead of being silently truncated, so callers never get undecodable
// JSON from this path.
func (c *RESTClient) GetConfigs(ctx context.Context) (json.RawMessage, error) {
	resp, err := c.do(ctx, http.MethodGet, c.baseURL+"/configs", "GET /configs", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read one byte past the cap so we can detect overflow without buffering
	// the entire oversize response.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxConfigsBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("mihomo rest: GET /configs: read body: %w", err)
	}
	if len(data) > maxConfigsBodyBytes {
		return nil, fmt.Errorf("mihomo rest: GET /configs: response exceeds %d bytes", maxConfigsBodyBytes)
	}
	return json.RawMessage(data), nil
}

// PutConfigs replaces mihomo's configuration with the given yaml payload.
//
// force=false leverages PatchInboundListeners' name-keyed diff: unchanged
// named listeners keep running and their active TCP connections stay up.
// force=true additionally recreates the legacy singleton listeners
// (HTTP/SOCKS/Mixed/Redir/TProxy/ShadowSocks/Vmess/Tuic); v2raymg never
// needs force=true since it relies solely on the named listeners array.
func (c *RESTClient) PutConfigs(ctx context.Context, yamlPayload []byte, force bool) error {
	url := fmt.Sprintf("%s/configs?force=%t", c.baseURL, force)
	body := map[string]string{"payload": string(yamlPayload)}
	resp, err := c.do(ctx, http.MethodPut, url, "PUT /configs", body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// PatchConfigs applies a partial config update to singleton fields
// (port, log-level, mode, tun, ...). Not usable for named listeners —
// those must go through PutConfigs.
func (c *RESTClient) PatchConfigs(ctx context.Context, fields map[string]any) error {
	resp, err := c.do(ctx, http.MethodPatch, c.baseURL+"/configs", "PATCH /configs", fields)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// PostConfigsGeo triggers mihomo to refresh its GeoIP/GeoSite databases.
// v2raymg does not route via mihomo's geo data; the method exists so the
// /configs surface is complete and future admin flows can surface it.
func (c *RESTClient) PostConfigsGeo(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodPost, c.baseURL+"/configs/geo", "POST /configs/geo", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
