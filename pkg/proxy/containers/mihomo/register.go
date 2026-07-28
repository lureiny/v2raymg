package mihomo

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func init() {
	container.RegisterFactory(contracts.ContainerMihomo, &mihomoFactory{})
}

type mihomoFactory struct{}

func (f *mihomoFactory) NewConfigObj() container.ContainerConfig {
	return &MihomoConfig{}
}

// certPathProvider is the minimum surface we need from BuildOptions.CertManager
// to whitelist the certmgr storage directory in SAFE_PATHS. Kept as a local
// anonymous-ish interface so this package stays free of a certmgmt import —
// pkg/certmgmt/service.Manager satisfies it via its exported Path() method.
type certPathProvider interface {
	Path() string
}

func (f *mihomoFactory) New(opts container.BuildOptions) (container.Container, error) {
	cfg, ok := opts.Config.(*MihomoConfig)
	if !ok || cfg == nil {
		return nil, fmt.Errorf("mihomo: invalid config type, expected *MihomoConfig")
	}
	var options []MihomoOption
	if opts.StoreMgr != nil {
		options = append(options, WithStoreMgr(opts.StoreMgr))
	}
	if opts.UserManager != nil {
		options = append(options, WithUserManager(opts.UserManager))
	}
	if opts.PortClaimer != nil {
		options = append(options, WithPortClaimer(opts.PortClaimer))
	}
	// If the caller provided a cert manager with a discoverable storage
	// root, whitelist that root in SAFE_PATHS so mihomo accepts
	// certmgr-managed cert paths referenced from inbound configs. When
	// the cert manager doesn't expose Path() (non-production test doubles,
	// or a future alternative impl), just skip — DataDir is always trusted
	// implicitly, so the self-signed / PEM-content paths still work.
	if p, ok := opts.CertManager.(certPathProvider); ok {
		if root := p.Path(); root != "" {
			options = append(options, WithSafePathRoots(root))
		}
	}
	return NewMihomoContainer(*cfg, options...)
}
