package hysteria

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TrafficStat holds per-user traffic statistics.
type TrafficStat struct {
	Upload   int64 // rx = server received = client upload
	Download int64 // tx = server sent = client download
}

// QueryTraffic queries the hysteria traffic stats API.
// If clear is true, stats are reset after reading.
func QueryTraffic(cfg HysteriaConfig, clear bool) (map[string]TrafficStat, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/traffic", cfg.TrafficStatsPort)
	if clear {
		url += "?clear=1"
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("hysteria: create traffic request: %w", err)
	}
	req.Header.Set("Authorization", cfg.TrafficStatsSecret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hysteria: query traffic: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hysteria: query traffic: HTTP %d", resp.StatusCode)
	}

	// Response format: {"username": {"tx": N, "rx": N}}
	var raw map[string]struct {
		TX int64 `json:"tx"`
		RX int64 `json:"rx"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("hysteria: decode traffic response: %w", err)
	}

	result := make(map[string]TrafficStat, len(raw))
	for user, stat := range raw {
		result[user] = TrafficStat{
			Upload:   stat.RX, // rx = server received = client upload
			Download: stat.TX, // tx = server sent = client download
		}
	}

	return result, nil
}

// KickUser kicks the specified users from the hysteria server.
func KickUser(cfg HysteriaConfig, usernames []string) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/kick", cfg.TrafficStatsPort)

	body, err := json.Marshal(usernames)
	if err != nil {
		return fmt.Errorf("hysteria: marshal kick request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("hysteria: create kick request: %w", err)
	}
	req.Header.Set("Authorization", cfg.TrafficStatsSecret)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("hysteria: kick user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hysteria: kick user: HTTP %d", resp.StatusCode)
	}

	return nil
}
