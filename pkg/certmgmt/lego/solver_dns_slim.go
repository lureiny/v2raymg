//go:build !full_dns

package certmgmtlego

// Slim DNS provider registry — only common providers are compiled in.
// Use `make build-full` to compile with all lego providers.
//
// Supported providers:
//   alidns, cloudflare, dnspod, route53, tencentcloud, namecheap, godaddy

import (
	"fmt"
	"time"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/providers/dns/alidns"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/dnspod"
	"github.com/go-acme/lego/v4/providers/dns/godaddy"
	"github.com/go-acme/lego/v4/providers/dns/namecheap"
	"github.com/go-acme/lego/v4/providers/dns/route53"
	"github.com/go-acme/lego/v4/providers/dns/tencentcloud"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// newSlimDNSProvider creates a DNS challenge provider by name.
// Only providers compiled in this slim build are supported.
func newSlimDNSProvider(name string) (challenge.Provider, error) {
	switch name {
	case "alidns":
		return alidns.NewDNSProvider()
	case "cloudflare":
		return cloudflare.NewDNSProvider()
	case "dnspod":
		return dnspod.NewDNSProvider()
	case "route53":
		return route53.NewDNSProvider()
	case "tencentcloud":
		return tencentcloud.NewDNSProvider()
	case "namecheap":
		return namecheap.NewDNSProvider()
	case "godaddy":
		return godaddy.NewDNSProvider()
	default:
		return nil, fmt.Errorf("DNS provider %q is not supported in slim build; use full build or choose from: alidns, cloudflare, dnspod, route53, tencentcloud, namecheap, godaddy", name)
	}
}

// NewDNS01Provider creates a challenge.Provider and associated options for a DNS-01 challenge.
// This function must be called inside a WithDNSCredentials callback so that the
// required environment variables are already set when the provider is constructed.
func NewDNS01Provider(cfg *domain.DNSChallengeConfig) (challenge.Provider, []dns01.ChallengeOption, error) {
	if cfg == nil || cfg.ProviderName == "" {
		return nil, nil, fmt.Errorf("%w: DNS provider name is required", domain.ErrConfigInvalid)
	}

	provider, err := newSlimDNSProvider(cfg.ProviderName)
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
