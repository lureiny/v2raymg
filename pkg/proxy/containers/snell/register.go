package snell

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func init() {
	container.RegisterFactory(contracts.ContainerSnell, &snellFactory{})
}

type snellFactory struct{}

func (f *snellFactory) NewConfigObj() container.ContainerConfig {
	return &SnellConfig{}
}

func (f *snellFactory) New(opts container.BuildOptions) (container.Container, error) {
	cfg, ok := opts.Config.(*SnellConfig)
	if !ok || cfg == nil {
		return nil, fmt.Errorf("snell: invalid config type, expected *SnellConfig")
	}
	var sopts []SnellOption
	if opts.StoreMgr != nil {
		sopts = append(sopts, WithStoreMgr(opts.StoreMgr))
	}
	if opts.UserManager != nil {
		sopts = append(sopts, WithUserManager(opts.UserManager))
	}
	return NewSnellContainer(*cfg, sopts...)
}
