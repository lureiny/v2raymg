package container

import (
	"context"
	"errors"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// ===== Mock types for ContainerMgr tests =====

// mockMgrRunnable implements Hooks for mockMgrContainer.
type mockMgrRunnable struct {
	startErr error
	stopErr  error
}

func (r *mockMgrRunnable) GetRunFunc() (func() error, func() error) {
	run := func() error { return r.startErr }
	stop := func() error { return r.stopErr }
	return run, stop
}

// mockMgrContainer is a minimal Container for ContainerMgr tests.
type mockMgrContainer struct {
	*BaseContainer
	restoreErr error
}

func newMockMgrContainer(kind ContainerType) *mockMgrContainer {
	c := &mockMgrContainer{}
	c.BaseContainer = NewBaseContainer(kind, &mockMgrRunnable{})
	return c
}

func newMockMgrContainerWithErrors(kind ContainerType, startErr, stopErr, restoreErr error) *mockMgrContainer {
	c := &mockMgrContainer{restoreErr: restoreErr}
	c.BaseContainer = NewBaseContainer(kind, &mockMgrRunnable{startErr: startErr, stopErr: stopErr})
	return c
}

func (c *mockMgrContainer) Init(config any) error                                               { return nil }
func (c *mockMgrContainer) Reload() error                                                       { return nil }
func (c *mockMgrContainer) Version() string                                                     { return "mock" }
func (c *mockMgrContainer) ConfigFile() string                                                  { return "" }
func (c *mockMgrContainer) Update(_ context.Context, _ UpdateRequest) (*UpdateResult, error)   { return nil, ErrNotSupported }
func (c *mockMgrContainer) RemoveInboundConfig(_ string) error                                  { return nil }
func (c *mockMgrContainer) GetInboundConfig(_ string) (Inbound, error)                         { return nil, nil }
func (c *mockMgrContainer) ListInboundConfigs() []Inbound                                       { return nil }
func (c *mockMgrContainer) FastAddInbound(_ string, _ map[string]any) error                    { return nil }
func (c *mockMgrContainer) UserEventChannel() <-chan usermanager.UserEvent                      { return nil }
func (c *mockMgrContainer) GetUserSubscriptions(_ contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	return nil, nil
}

// Restore makes mockMgrContainer implement Restorable.
func (c *mockMgrContainer) Restore(_ context.Context) error { return c.restoreErr }

// mockMgrConfig implements ContainerConfig.
type mockMgrConfig struct {
	decodeErr error
}

func (c *mockMgrConfig) Decode(_ map[string]any) error { return c.decodeErr }

// mockMgrFactory implements Factory.
type mockMgrFactory struct {
	cfgObj    *mockMgrConfig
	container *mockMgrContainer
	newErr    error
}

func (f *mockMgrFactory) NewConfigObj() ContainerConfig { return f.cfgObj }

func (f *mockMgrFactory) New(_ BuildOptions) (Container, error) {
	if f.newErr != nil {
		return nil, f.newErr
	}
	return f.container, nil
}

// registerMgrFactory registers a factory under the given type and returns a cleanup func.
func registerMgrFactory(kind ContainerType, f Factory) func() {
	RegisterFactory(kind, f)
	return func() {
		factoryMapMu.Lock()
		delete(factoryMap, kind)
		factoryMapMu.Unlock()
	}
}

// ===== Tests =====

func TestContainerMgr_LoadFromConfig_disabled(t *testing.T) {
	const kind ContainerType = "mgr-test-disabled"

	// Do NOT register a factory — if a disabled entry were processed, it would error.
	cfg := ContainerMgrConfig{
		Containers: []ContainerEntry{
			{Type: kind, Enabled: false, Config: map[string]any{}},
		},
	}

	mgr := NewContainerMgr(nil, BuildOptions{})
	if err := mgr.LoadFromConfig(cfg); err != nil {
		t.Fatalf("LoadFromConfig() unexpected error: %v", err)
	}

	if _, ok := mgr.Get(kind); ok {
		t.Error("disabled entry should not be instantiated")
	}

	if len(mgr.Types()) != 0 {
		t.Errorf("expected 0 types, got %d", len(mgr.Types()))
	}
}

func TestContainerMgr_LoadFromConfig_unknownType(t *testing.T) {
	const kind ContainerType = "mgr-test-unknown-xyz"

	// Ensure not registered.
	factoryMapMu.RLock()
	_, registered := factoryMap[kind]
	factoryMapMu.RUnlock()
	if registered {
		t.Skipf("type %q is already registered; skipping", kind)
	}

	cfg := ContainerMgrConfig{
		Containers: []ContainerEntry{
			{Type: kind, Enabled: true, Config: map[string]any{}},
		},
	}

	mgr := NewContainerMgr(nil, BuildOptions{})
	err := mgr.LoadFromConfig(cfg)
	if err == nil {
		t.Fatal("LoadFromConfig() expected error for unregistered type, got nil")
	}
}

func TestContainerMgr_LoadFromConfig_decodeError(t *testing.T) {
	const kind ContainerType = "mgr-test-decode-err"
	decodeErr := errors.New("bad config")

	cleanup := registerMgrFactory(kind, &mockMgrFactory{
		cfgObj:    &mockMgrConfig{decodeErr: decodeErr},
		container: newMockMgrContainer(kind),
	})
	defer cleanup()

	cfg := ContainerMgrConfig{
		Containers: []ContainerEntry{
			{Type: kind, Enabled: true, Config: map[string]any{"bad": "value"}},
		},
	}

	mgr := NewContainerMgr(nil, BuildOptions{})
	err := mgr.LoadFromConfig(cfg)
	if err == nil {
		t.Fatal("LoadFromConfig() expected error for Decode failure, got nil")
	}
	if !errors.Is(err, decodeErr) {
		t.Errorf("expected wrapped decodeErr, got: %v", err)
	}
}

func TestContainerMgr_LoadFromConfig_enabled(t *testing.T) {
	const kind ContainerType = "mgr-test-enabled"

	c := newMockMgrContainer(kind)
	cleanup := registerMgrFactory(kind, &mockMgrFactory{
		cfgObj:    &mockMgrConfig{},
		container: c,
	})
	defer cleanup()

	cfg := ContainerMgrConfig{
		Containers: []ContainerEntry{
			{Type: kind, Enabled: true, Config: map[string]any{}},
		},
	}

	mgr := NewContainerMgr(nil, BuildOptions{})
	if err := mgr.LoadFromConfig(cfg); err != nil {
		t.Fatalf("LoadFromConfig() unexpected error: %v", err)
	}

	got, ok := mgr.Get(kind)
	if !ok {
		t.Fatal("expected container to be loaded")
	}
	if got != c {
		t.Error("Get() returned wrong instance")
	}

	types := mgr.Types()
	if len(types) != 1 || types[0] != kind {
		t.Errorf("Types() = %v, want [%q]", types, kind)
	}
}

func TestContainerMgr_StartAll_StopAll(t *testing.T) {
	const kind ContainerType = "mgr-test-start-stop"

	c := newMockMgrContainer(kind)
	cleanup := registerMgrFactory(kind, &mockMgrFactory{
		cfgObj:    &mockMgrConfig{},
		container: c,
	})
	defer cleanup()

	mgr := NewContainerMgr(nil, BuildOptions{})
	cfg := ContainerMgrConfig{
		Containers: []ContainerEntry{
			{Type: kind, Enabled: true, Config: map[string]any{}},
		},
	}
	if err := mgr.LoadFromConfig(cfg); err != nil {
		t.Fatalf("LoadFromConfig() unexpected error: %v", err)
	}

	if err := mgr.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll() unexpected error: %v", err)
	}
	if !c.IsRunning() {
		t.Error("container should be running after StartAll")
	}

	if err := mgr.StopAll(); err != nil {
		t.Fatalf("StopAll() unexpected error: %v", err)
	}
	if c.IsRunning() {
		t.Error("container should not be running after StopAll")
	}
}

func TestContainerMgr_RestoreAll_restorable(t *testing.T) {
	const kind ContainerType = "mgr-test-restore"

	c := newMockMgrContainer(kind)
	cleanup := registerMgrFactory(kind, &mockMgrFactory{
		cfgObj:    &mockMgrConfig{},
		container: c,
	})
	defer cleanup()

	mgr := NewContainerMgr(nil, BuildOptions{})
	cfg := ContainerMgrConfig{
		Containers: []ContainerEntry{
			{Type: kind, Enabled: true, Config: map[string]any{}},
		},
	}
	if err := mgr.LoadFromConfig(cfg); err != nil {
		t.Fatalf("LoadFromConfig() unexpected error: %v", err)
	}

	// mockMgrContainer implements Restorable; Restore should succeed.
	if err := mgr.RestoreAll(context.Background()); err != nil {
		t.Fatalf("RestoreAll() unexpected error: %v", err)
	}
}

func TestContainerMgr_RestoreAll_errorAccumulated(t *testing.T) {
	const kind ContainerType = "mgr-test-restore-err"
	restoreErr := errors.New("restore failed")

	c := newMockMgrContainerWithErrors(kind, nil, nil, restoreErr)
	cleanup := registerMgrFactory(kind, &mockMgrFactory{
		cfgObj:    &mockMgrConfig{},
		container: c,
	})
	defer cleanup()

	mgr := NewContainerMgr(nil, BuildOptions{})
	cfg := ContainerMgrConfig{
		Containers: []ContainerEntry{
			{Type: kind, Enabled: true, Config: map[string]any{}},
		},
	}
	if err := mgr.LoadFromConfig(cfg); err != nil {
		t.Fatalf("LoadFromConfig() unexpected error: %v", err)
	}

	err := mgr.RestoreAll(context.Background())
	if err == nil {
		t.Fatal("RestoreAll() expected error, got nil")
	}
	if !errors.Is(err, restoreErr) {
		t.Errorf("expected wrapped restoreErr, got: %v", err)
	}
}
