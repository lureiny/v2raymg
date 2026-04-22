package mihomo

import (
	"context"
	"fmt"
	"sync"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	"github.com/lureiny/v2raymg/pkg/proxy/tools/process"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// MihomoContainer implements the Container interface for mihomo.
type MihomoContainer struct {
	*container.BaseContainer
	cfg MihomoConfig

	// mu guards runner, restClient and cachedVersion. Version() can be called
	// from any goroutine while startProcess/stopProcess mutate these fields,
	// so all reads and writes go through this lock. The lock is only held
	// around assignments — never across I/O (download / runner.Start /
	// runner.Stop / REST calls) — so lifecycle transitions don't serialise
	// on unrelated readers.
	mu            sync.RWMutex
	runner        *process.Runner
	restClient    *RESTClient
	cachedVersion string
}

type mihomoHooks struct {
	c *MihomoContainer
}

// GetRunFunc returns run/stop hooks for BaseContainer. run is never nil —
// BaseContainer panics if it is.
func (h *mihomoHooks) GetRunFunc() (func() error, func() error) {
	return h.c.startProcess, h.c.stopProcess
}

// NewMihomoContainer builds a MihomoContainer from the given config.
func NewMihomoContainer(cfg MihomoConfig) (*MihomoContainer, error) {
	c := &MihomoContainer{cfg: cfg}
	c.BaseContainer = container.NewBaseContainer(contracts.ContainerMihomo, &mihomoHooks{c: c})
	return c, nil
}

func (c *MihomoContainer) Init(_ any) error   { return nil }
func (c *MihomoContainer) Reload() error      { return nil }
func (c *MihomoContainer) ConfigFile() string { return c.cfg.ConfigFilePath }

func (c *MihomoContainer) Version() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cachedVersion
}

func (c *MihomoContainer) Update(_ context.Context, _ container.UpdateRequest) (*container.UpdateResult, error) {
	return nil, container.ErrNotSupported
}

func (c *MihomoContainer) FastAddInbound(_ string, _ map[string]any) error {
	return fmt.Errorf("mihomo: FastAddInbound not implemented")
}

func (c *MihomoContainer) RemoveInboundConfig(_ string) error {
	return fmt.Errorf("mihomo: RemoveInboundConfig not implemented")
}

func (c *MihomoContainer) GetInboundConfig(_ string) (inbound.Inbound, error) {
	return nil, fmt.Errorf("mihomo: GetInboundConfig not implemented")
}

func (c *MihomoContainer) ListInboundConfigs() []inbound.Inbound { return nil }

func (c *MihomoContainer) UserEventChannel() <-chan usermanager.UserEvent { return nil }

func (c *MihomoContainer) GetUserSubscriptions(_ contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	return nil, nil
}
