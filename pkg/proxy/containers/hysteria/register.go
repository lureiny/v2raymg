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
