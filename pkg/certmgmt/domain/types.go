package domain

import (
	"context"
	"time"

	"github.com/go-acme/lego/v4/registration"
)

// ChallengeType identifies the ACME challenge mechanism.
type ChallengeType string

const (
	ChallengeHTTP01 ChallengeType = "http01"
	ChallengeDNS01  ChallengeType = "dns01"
)

// HTTPChallengeConfig configures the HTTP-01 challenge.
type HTTPChallengeConfig struct {
	// Mode: "server" (default) | "webroot" | "memcached"
	Mode           string
	ListenAddr     string   // for "server" mode, default ":80"
	WebRoot        string   // for "webroot" mode
	MemcachedHosts []string // for "memcached" mode
	ProxyHeader    string   // reverse-proxy header, default "Host"
}

// DNSChallengeConfig configures the DNS-01 challenge.
type DNSChallengeConfig struct {
	ProviderName               string
	Credentials                map[string]string // env_var_name → value
	Resolvers                  []string
	DisableCompletePropagation bool
	TimeoutSec                 int // default 10
}

// ChallengeConfig selects and configures the challenge type.
type ChallengeConfig struct {
	Type ChallengeType
	HTTP *HTTPChallengeConfig
	DNS  *DNSChallengeConfig
}

// IssueRequest carries all parameters for issuing or renewing a certificate.
type IssueRequest struct {
	Domains        []string
	Email          string
	Challenge      ChallengeConfig
	KeyType        string // "ec256" (default) | "rsa2048" | "rsa4096" | "rsa8192" | "ec384"
	PreferredChain string
	Bundle          bool          // default true
	RenewBeforeDays int           // legacy lead time in days, default 30; superseded by RenewBefore when set
	RenewBefore     time.Duration // authoritative renewal lead time; falls back to RenewBeforeDays*24h then 30d when zero
	CAURL           string        // default lego.LEDirectoryProduction
}

// CertificateRecord is the public certificate metadata stored and returned to callers.
type CertificateRecord struct {
	Domain       string
	CertFile     string
	KeyFile      string
	ResourceFile string    // lego resource JSON, required for renewal
	NotAfter     time.Time
	ObtainedBy   string    // "http01" | "dns01" | "imported"
	ObtainedAt   time.Time
}

// AccountData is the persisted form of an ACME account.
type AccountData struct {
	Email         string                 `json:"email"`
	Registration  *registration.Resource `json:"registration"`
	PrivateKeyPEM []byte                 `json:"private_key_pem"`
}

// LegoResource is the persisted form of a lego certificate.Resource.
// certificate.Resource has json:"-" tags on its PEM fields, so we copy them here.
type LegoResource struct {
	Domain         string `json:"domain"`
	CertURL        string `json:"certUrl"`
	CertStableURL  string `json:"certStableUrl"`
	PrivateKeyPEM  []byte `json:"private_key"`
	CertificatePEM []byte `json:"certificate"`
	IssuerCertPEM  []byte `json:"issuer_certificate,omitempty"`
	CSRPEM         []byte `json:"csr,omitempty"`
}

// Issuer handles certificate lifecycle operations.
type Issuer interface {
	Issue(ctx context.Context, req IssueRequest) (*CertificateRecord, error)
	Renew(ctx context.Context, record *CertificateRecord, req IssueRequest) (*CertificateRecord, error)
	Revoke(ctx context.Context, record *CertificateRecord) error
}

// Store persists accounts and certificates.
type Store interface {
	SaveAccount(caURL, email string, data *AccountData) error
	LoadAccount(caURL, email string) (*AccountData, error)
	SaveCert(record *CertificateRecord, resource *LegoResource) error
	LoadCert(domain string) (*CertificateRecord, *LegoResource, error)
	ListCerts() ([]*CertificateRecord, error)
}
