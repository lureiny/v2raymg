//go:build full_dns

package certmgmtlego

// Full DNS provider registry — all lego providers are compiled in.
// Build with: make build-full

import (
	"fmt"
	"time"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/providers/dns"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// NewDNS01Provider creates a challenge.Provider and associated options for a DNS-01 challenge.
// Full build: delegates to lego's built-in provider registry (all 50+ providers).
func NewDNS01Provider(cfg *domain.DNSChallengeConfig) (challenge.Provider, []dns01.ChallengeOption, error) {
	if cfg == nil || cfg.ProviderName == "" {
		return nil, nil, fmt.Errorf("%w: DNS provider name is required", domain.ErrConfigInvalid)
	}

	provider, err := dns.NewDNSChallengeProviderByName(cfg.ProviderName)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", domain.ErrDNSProviderNotFound, err)
	}

	var opts []dns01.ChallengeOption

	if len(cfg.Resolvers) > 0 {
		opts = append(opts, dns01.AddRecursiveNameservers(dns01.ParseNameservers(cfg.Resolvers)))
	}

	if cfg.DisableCompletePropagation {
		opts = append(opts, dns01.DisableCompletePropagationRequirement())
	}

	if cfg.TimeoutSec > 0 {
		opts = append(opts, dns01.AddDNSTimeout(time.Duration(cfg.TimeoutSec)*time.Second))
	}

	return provider, opts, nil
}
