package ping

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	"gopkg.in/yaml.v3"
)

// RemoteLoader loads ping nodes from a remote URL via HTTP.
type RemoteLoader struct {
	url     string
	client  *http.Client
	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
}

// NewRemoteLoader creates a new RemoteLoader.
func NewRemoteLoader(url string) *RemoteLoader {
	return &RemoteLoader{
		url: url,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		stopCh: make(chan struct{}),
	}
}

// Load fetches and parses the YAML from the remote URL.
func (l *RemoteLoader) Load() ([]*PingNodeInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %q: %w", l.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %q: status %d", l.url, resp.StatusCode)
	}

	var nf nodesFile
	if err := yaml.NewDecoder(resp.Body).Decode(&nf); err != nil {
		return nil, fmt.Errorf("parse yaml from %q: %w", l.url, err)
	}

	// Set defaults for nodes
	for _, node := range nf.Nodes {
		if len(node.Usage) == 0 {
			node.Usage = []string{"icmp", "tcp"}
		}
		if node.Port == 0 {
			node.Port = 80 // default TCP port for probing
		}
	}

	return nf.Nodes, nil
}

// Name returns the loader name.
func (l *RemoteLoader) Name() string {
	return "remote"
}

// StartReload starts a goroutine that periodically reloads nodes.
func (l *RemoteLoader) StartReload(interval time.Duration, onChange func([]*PingNodeInfo)) (stop func()) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.running {
		return func() {}
	}

	l.running = true
	l.stopCh = make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-l.stopCh:
				log.Debug("remote loader stopped", "url", l.url)
				return
			case <-ticker.C:
				nodes, err := l.Load()
				if err != nil {
					log.Error("reload ping nodes failed", "url", l.url, "err", err)
					continue
				}
				log.Info("reloaded ping nodes from remote", "url", l.url, "count", len(nodes))
				if onChange != nil {
					onChange(nodes)
				}
			}
		}
	}()

	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.running {
			close(l.stopCh)
			l.running = false
		}
	}
}
