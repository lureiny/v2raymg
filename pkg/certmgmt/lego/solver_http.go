package certmgmtlego

import (
	"fmt"
	"net"
	"strings"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/providers/http/memcached"
	"github.com/go-acme/lego/v4/providers/http/webroot"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// NewHTTP01Provider creates a challenge.Provider based on the HTTP challenge configuration.
//
//   - Mode "server" (default): starts a built-in HTTP server on ListenAddr.
//   - Mode "webroot": writes token files into WebRoot directory.
//   - Mode "memcached": distributes token via memcached.
func NewHTTP01Provider(cfg *domain.HTTPChallengeConfig) (challenge.Provider, error) {
	if cfg == nil {
		cfg = &domain.HTTPChallengeConfig{}
	}

	mode := strings.ToLower(cfg.Mode)
	if mode == "" {
		mode = "server"
	}

	switch mode {
	case "server":
		iface, port, err := splitListenAddr(cfg.ListenAddr)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid ListenAddr %q: %v", domain.ErrChallengeSetup, cfg.ListenAddr, err)
		}
		srv := http01.NewProviderServer(iface, port)
		if cfg.ProxyHeader != "" {
			srv.SetProxyHeader(cfg.ProxyHeader)
		}
		return srv, nil

	case "webroot":
		p, err := webroot.NewHTTPProvider(cfg.WebRoot)
		if err != nil {
			return nil, fmt.Errorf("%w: webroot: %v", domain.ErrChallengeSetup, err)
		}
		return p, nil

	case "memcached":
		p, err := memcached.NewMemcachedProvider(cfg.MemcachedHosts)
		if err != nil {
			return nil, fmt.Errorf("%w: memcached: %v", domain.ErrChallengeSetup, err)
		}
		return p, nil

	default:
		return nil, fmt.Errorf("%w: unknown HTTP challenge mode %q", domain.ErrChallengeSetup, mode)
	}
}

// splitListenAddr splits a "host:port" or ":port" address into iface and port strings
// suitable for http01.NewProviderServer. An empty addr defaults to ":80".
func splitListenAddr(addr string) (iface, port string, err error) {
	if addr == "" {
		return "", "80", nil
	}
	iface, port, err = net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	return iface, port, nil
}
