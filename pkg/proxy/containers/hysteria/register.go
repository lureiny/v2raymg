package hysteria

import (
	"context"
	"fmt"

	certmgmtdomain "github.com/lureiny/v2raymg/pkg/certmgmt/domain"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// certIssuer is the subset of *service.Manager that hysteria needs.
// It is extracted from BuildOptions.CertManager via type assertion.
type certIssuer interface {
	GetCertFiles(domain string) (certFile, keyFile string, ok bool)
	Issue(ctx context.Context, domains []string) (*certmgmtdomain.CertificateRecord, error)
}

func init() {
	container.RegisterFactory(contracts.ContainerHysteria, &hysteriaFactory{})
}

type hysteriaFactory struct{}

func (f *hysteriaFactory) NewConfigObj() container.ContainerConfig {
	return &HysteriaConfig{}
}

func (f *hysteriaFactory) New(opts container.BuildOptions) (container.Container, error) {
	cfg, ok := opts.Config.(*HysteriaConfig)
	if !ok || cfg == nil {
		return nil, fmt.Errorf("hysteria: invalid config type, expected *HysteriaConfig")
	}

	// Final fallback chain for the TLS/ACME domain:
	//   cfg.Domain → cfg.Host → opts.ProxyHost (NodeConfig.ProxyHost)
	// This lets existing deployments upgrade without touching their hysteria
	// config: the node-level ProxyHost (already used for subscription links
	// and metric labels) becomes the ACME domain automatically.
	if cfg.Domain == "" && opts.ProxyHost != "" {
		cfg.Domain = opts.ProxyHost
	}
	// Without a domain and without a preinstalled cert pair, the process
	// would sit in waitForCertAndStart forever logging `waiting for
	// certificate domain=""`. Fail fast so the misconfiguration is visible
	// at startup.
	if cfg.Domain == "" && (cfg.CertFile == "" || cfg.KeyFile == "") {
		return nil, fmt.Errorf("hysteria: no TLS domain source — configure NodeConfig.ProxyHost, hysteria.host, hysteria.domain, or both hysteria.cert_file and hysteria.key_file")
	}

	var hopts []HysteriaOption
	if opts.StoreMgr != nil {
		hopts = append(hopts, WithStoreMgr(opts.StoreMgr))
	}
	if opts.UserManager != nil {
		hopts = append(hopts, WithUserManager(opts.UserManager))
	}
	if opts.CertReader != nil {
		hopts = append(hopts, WithCertReader(opts.CertReader))
	}
	if opts.HTTPPort != 0 {
		hopts = append(hopts, WithHTTPPort(opts.HTTPPort))
	}
	if ce, ok := opts.CertManager.(certIssuer); ok {
		hopts = append(hopts, WithCertManager(ce))
	}
	return NewHysteriaContainer(*cfg, hopts...)
}
