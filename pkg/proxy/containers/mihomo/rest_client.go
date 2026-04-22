package mihomo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultRESTTimeout = 10 * time.Second

// RESTClient talks to the mihomo external-controller REST API.
//
// Stage 2 only exposes GetVersion (readiness probe + version cache seed).
// Stage 3 will add GetConfigs / PutConfigs / PatchConfigs / PostConfigsGeo.
type RESTClient struct {
	baseURL    string
	secret     string
	httpClient *http.Client
}

// NewRESTClient builds a RESTClient targeting the given base URL.
// baseURL must be an absolute URL including scheme, e.g. "http://127.0.0.1:9090".
// Pass empty secret to skip Authorization headers (mihomo disables auth in
// that mode — see hub/route/server.go authentication middleware).
func NewRESTClient(baseURL, secret string) *RESTClient {
	return &RESTClient{
		baseURL:    baseURL,
		secret:     secret,
		httpClient: &http.Client{Timeout: defaultRESTTimeout},
	}
}

type versionResponse struct {
	Version string `json:"version"`
	Meta    bool   `json:"meta"`
}

// GetVersion fetches the running mihomo version via GET /version.
//
// Error signals are distinguished explicitly so callers can react to the
// common "wrong secret" case separately from generic transport failures.
func (c *RESTClient) GetVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/version", nil)
	if err != nil {
		return "", fmt.Errorf("mihomo rest: build request: %w", err)
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("mihomo rest: GET /version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("mihomo rest: GET /version: unauthorized (check secret)")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mihomo rest: GET /version: HTTP %d", resp.StatusCode)
	}

	var body versionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("mihomo rest: decode /version: %w", err)
	}
	return body.Version, nil
}
