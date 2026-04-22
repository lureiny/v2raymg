package mihomo

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// TestMihomoFactoryLoadable verifies the stage-1 acceptance criterion:
// ContainerMgr loads an enabled mihomo entry without error and the returned
// Container reports the expected ContainerType. Start() is intentionally not
// invoked — the skeleton runner returns "not implemented" by design.
func TestMihomoFactoryLoadable(t *testing.T) {
	cfg := container.ContainerMgrConfig{
		Containers: []container.ContainerEntry{
			{
				Type:    contracts.ContainerMihomo,
				Enabled: true,
				Config:  map[string]any{},
			},
		},
	}

	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	if err := mgr.LoadFromConfig(cfg); err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}

	c, ok := mgr.Get(contracts.ContainerMihomo)
	if !ok {
		t.Fatal("mihomo container not loaded")
	}
	if c.Type() != contracts.ContainerMihomo {
		t.Errorf("Type() = %q, want %q", c.Type(), contracts.ContainerMihomo)
	}
}
