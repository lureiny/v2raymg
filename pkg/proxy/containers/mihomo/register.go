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

func (f *mihomoFactory) New(opts container.BuildOptions) (container.Container, error) {
	cfg, ok := opts.Config.(*MihomoConfig)
	if !ok || cfg == nil {
		return nil, fmt.Errorf("mihomo: invalid config type, expected *MihomoConfig")
	}
	var options []MihomoOption
	if opts.StoreMgr != nil {
		options = append(options, WithStoreMgr(opts.StoreMgr))
	}
	return NewMihomoContainer(*cfg, options...)
}
