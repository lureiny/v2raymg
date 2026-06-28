package certmgmtservice

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	certmgmtlego "github.com/lureiny/v2raymg/pkg/certmgmt/lego"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// Config holds all configuration for the Manager.
type Config struct {
	Email string `yaml:"email" json:"email"`
	Path  string `yaml:"path"  json:"path"`
	// RenewBeforeDays is the legacy renewal lead time in DAYS. Kept for backward
	// compatibility and still propagated into IssueRequest; RenewBeforeHours takes
	// precedence when set. See renewBeforeDuration.
	RenewBeforeDays int `yaml:"renew_before_days" json:"renew_before_days"`
	// RenewBeforeHours is how long before expiry auto-renew starts attempting,
	// expressed in HOURS. Takes precedence over RenewBeforeDays. Defaults to 24h
	// when neither field is set (see renewBeforeDuration).
	RenewBeforeHours int             `yaml:"renew_before_hours" json:"renew_before_hours"`
	Challenge        ChallengeConfig `yaml:"challenge"          json:"challenge"`
}

// defaultRenewBefore is the renewal lead time used when neither RenewBeforeHours
// nor RenewBeforeDays is configured.
const defaultRenewBefore = 24 * time.Hour

// renewBeforeDuration returns the effective "renew this long before expiry" window.
// Precedence: RenewBeforeHours (>0) wins; else RenewBeforeDays*24h (>0); else 24h.
func (c Config) renewBeforeDuration() time.Duration {
	if c.RenewBeforeHours > 0 {
		return time.Duration(c.RenewBeforeHours) * time.Hour
	}
	if c.RenewBeforeDays > 0 {
		return time.Duration(c.RenewBeforeDays) * 24 * time.Hour
	}
	return defaultRenewBefore
}

// ChallengeConfig mirrors domain.ChallengeConfig but uses plain strings for easier unmarshalling.
type ChallengeConfig struct {
	Type string    `yaml:"type" json:"type"`
	DNS  DNSConfig `yaml:"dns"  json:"dns"`
	HTTP HTTPConfig `yaml:"http" json:"http"`
}

// DNSConfig mirrors domain.DNSChallengeConfig.
type DNSConfig struct {
	ProviderName               string            `yaml:"provider_name"                json:"provider_name"`
	Credentials                map[string]string `yaml:"credentials"                  json:"credentials"`
	Resolvers                  []string          `yaml:"resolvers"                    json:"resolvers"`
	DisableCompletePropagation bool              `yaml:"disable_complete_propagation" json:"disable_complete_propagation"`
	TimeoutSec                 int               `yaml:"timeout_sec"                  json:"timeout_sec"`
}

// HTTPConfig mirrors domain.HTTPChallengeConfig.
type HTTPConfig struct {
	Mode           string   `yaml:"mode"            json:"mode"`
	ListenAddr     string   `yaml:"listen_addr"     json:"listen_addr"`
	WebRoot        string   `yaml:"web_root"        json:"web_root"`
	MemcachedHosts []string `yaml:"memcached_hosts" json:"memcached_hosts"`
	ProxyHeader    string   `yaml:"proxy_header"    json:"proxy_header"`
}

// Manager orchestrates certificate issuance, renewal and retrieval.
type Manager struct {
	cfg    Config
	issuer domain.Issuer
	// domainMu holds a *sync.Mutex per domain to prevent concurrent operations on the same domain.
	domainMu sync.Map
}

// NewManager creates a Manager backed by a LegoIssuer.
func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:    cfg,
		issuer: certmgmtlego.NewLegoIssuer(cfg.Path),
	}
}

// NewManagerWithIssuer creates a Manager with an injected domain.Issuer (for testing).
func NewManagerWithIssuer(cfg Config, issuer domain.Issuer) *Manager {
	return &Manager{
		cfg:    cfg,
		issuer: issuer,
	}
}

// Path returns the filesystem root under which certmgmt stores all
// certificate material (ACME-issued, imported, etc). Exposed so callers
// that need to whitelist the directory — e.g. the mihomo container, whose
// SAFE_PATHS env var must include every directory a cert path might point
// into — can wire it up at container-start time without hardcoding the
// layout.
func (m *Manager) Path() string { return m.cfg.Path }

// domainLock returns the per-domain mutex, creating it if necessary.
func (m *Manager) domainLock(d string) *sync.Mutex {
	v, _ := m.domainMu.LoadOrStore(d, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Issue obtains a new certificate for the given domains.
func (m *Manager) Issue(ctx context.Context, domains []string) (*domain.CertificateRecord, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("%w: domains required", domain.ErrConfigInvalid)
	}
	mu := m.domainLock(domains[0])
	mu.Lock()
	defer mu.Unlock()

	req := m.buildIssueRequest(domains)
	return m.issuer.Issue(ctx, req)
}

// RenewDomain renews the certificate for a single domain if it is close to expiry.
// Returns nil, nil if the certificate does not yet need renewal.
func (m *Manager) RenewDomain(ctx context.Context, d string) (*domain.CertificateRecord, error) {
	mu := m.domainLock(d)
	mu.Lock()
	defer mu.Unlock()

	record, _, err := certmgmtlego.LoadCert(m.cfg.Path, d)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("%w: no certificate for domain %q", domain.ErrCertResourceMissing, d)
	}

	// Check renewal threshold before delegating to the issuer.
	if time.Until(record.NotAfter) > m.cfg.renewBeforeDuration() {
		return nil, nil // not yet due
	}

	req := m.buildIssueRequest([]string{d})
	return m.issuer.Renew(ctx, record, req)
}

// GetCert returns the stored certificate record for a domain, or nil if not found.
func (m *Manager) GetCert(d string) *domain.CertificateRecord {
	record, _, err := certmgmtlego.LoadCert(m.cfg.Path, d)
	if err != nil {
		log.Printf("certmgmt: GetCert(%q): %v", d, err)
		return nil
	}
	return record
}

// ListCerts returns all stored certificate records.
func (m *Manager) ListCerts() []*domain.CertificateRecord {
	records, err := certmgmtlego.ListCerts(m.cfg.Path)
	if err != nil {
		log.Printf("certmgmt: ListCerts: %v", err)
		return nil
	}
	return records
}

// TODO: Phase B — add Facade methods that return *legopkg.Certificate for compatibility
// with the old lego.CertManager interface. These require importing the old package types
// which is currently forbidden (init() side effects). Deferred to Phase B.

// buildIssueRequest converts the Manager's config into a domain.IssueRequest.
func (m *Manager) buildIssueRequest(domains []string) domain.IssueRequest {
	req := domain.IssueRequest{
		Domains:         domains,
		Email:           m.cfg.Email,
		Bundle:          true,
		RenewBeforeDays: m.cfg.RenewBeforeDays,
		// RenewBefore is the authoritative renewal lead time (honors hours);
		// the issuer gate uses it so a window > 30 days is not silently capped.
		RenewBefore: m.cfg.renewBeforeDuration(),
		Challenge: domain.ChallengeConfig{
			Type: domain.ChallengeType(m.cfg.Challenge.Type),
		},
	}
	if req.RenewBeforeDays <= 0 {
		req.RenewBeforeDays = 30
	}

	switch m.cfg.Challenge.Type {
	case string(domain.ChallengeDNS01), "dns": // accept both "dns01" and "dns"
		req.Challenge.DNS = &domain.DNSChallengeConfig{
			ProviderName:               m.cfg.Challenge.DNS.ProviderName,
			Credentials:                m.cfg.Challenge.DNS.Credentials,
			Resolvers:                  m.cfg.Challenge.DNS.Resolvers,
			DisableCompletePropagation: m.cfg.Challenge.DNS.DisableCompletePropagation,
			TimeoutSec:                 m.cfg.Challenge.DNS.TimeoutSec,
		}
	default:
		req.Challenge.HTTP = &domain.HTTPChallengeConfig{
			Mode:           m.cfg.Challenge.HTTP.Mode,
			ListenAddr:     m.cfg.Challenge.HTTP.ListenAddr,
			WebRoot:        m.cfg.Challenge.HTTP.WebRoot,
			MemcachedHosts: m.cfg.Challenge.HTTP.MemcachedHosts,
			ProxyHeader:    m.cfg.Challenge.HTTP.ProxyHeader,
		}
	}
	return req
}
