package container

import (
	"context"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// testContainerHooks implements Hooks for testContainer
type testContainerHooks struct{}

func (t *testContainerHooks) GetRunFunc() (func() error, func() error) {
	return func() error { return nil }, func() error { return nil }
}

// testContainer is a minimal container implementation that embeds BaseContainer for testing.
type testContainer struct {
	*BaseContainer
}

func newTestContainer() *testContainer {
	return &testContainer{
		BaseContainer: NewBaseContainer(contracts.ContainerType("test-type"), &testContainerHooks{}),
	}
}

// Implement remaining Container methods for testContainer

func (c *testContainer) Version() string {
	return "test-version"
}

func (c *testContainer) ConfigFile() string {
	return "/test/config.json"
}

func (c *testContainer) Reload() error {
	return nil
}

func (c *testContainer) AddInboundConfig(inbound Inbound) error {
	return nil
}

func (c *testContainer) RemoveInboundConfig(tag string) error {
	return nil
}

func (c *testContainer) GetInboundConfig(tag string) (Inbound, error) {
	return nil, nil
}

func (c *testContainer) ListInboundConfigs() []Inbound {
	return nil
}

func (c *testContainer) UserEventChannel() <-chan usermanager.UserEvent {
	return nil
}

func (c *testContainer) StartUserSync(interval time.Duration) error {
	// no-op for test
	return nil
}

func (c *testContainer) StopUserSync() {
	// no-op for test
}

func (c *testContainer) FastAddInbound(tag string, params map[string]any) error {
	// no-op for test
	return nil
}

func (c *testContainer) Update(ctx context.Context, req UpdateRequest) (*UpdateResult, error) {
	return nil, ErrNotSupported
}

// Init implements container.Container.Init.
func (c *testContainer) Init(config any) error {
	return nil
}

func (c *testContainer) GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	return nil, nil
}

func TestRegistry_Register(t *testing.T) {
	// Unregister first to ensure clean state
	UnregisterContainer("test-type-reg-1")

	err := RegisterContainer("test-type-reg-1", func() Container {
		return newTestContainer()
	})
	if err != nil {
		t.Fatalf("RegisterContainer() returned error: %v", err)
	}

	// Verify registered
	if !IsRegistered("test-type-reg-1") {
		t.Error("expected container type to be registered")
	}

	// Clean up
	UnregisterContainer("test-type-reg-1")
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	// Unregister first
	UnregisterContainer("test-type-dup")

	// Register once
	RegisterContainer("test-type-dup", func() Container {
		return newTestContainer()
	})

	// Register again should fail
	err := RegisterContainer("test-type-dup", func() Container {
		return newTestContainer()
	})
	if err == nil {
		t.Error("expected error when registering duplicate container type")
	}

	// Clean up
	UnregisterContainer("test-type-dup")
}

func TestRegistry_NewContainer(t *testing.T) {
	// Unregister first
	UnregisterContainer("test-type-reg-2")

	// Register
	RegisterContainer("test-type-reg-2", func() Container {
		return newTestContainer()
	})

	// Create via registry
	c, err := NewContainer("test-type-reg-2")
	if err != nil {
		t.Fatalf("NewContainer() returned error: %v", err)
	}
	if c == nil {
		t.Fatal("NewContainer() returned nil")
	}

	// Clean up
	UnregisterContainer("test-type-reg-2")
}

func TestRegistry_NewContainer_Unknown(t *testing.T) {
	c, err := NewContainer("unknown-type-xyz")
	if err == nil {
		t.Error("expected error for unknown container type")
	}
	if c != nil {
		t.Error("expected nil container for unknown type")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	// Unregister first
	UnregisterContainer("test-type-reg-3")

	// Register
	RegisterContainer("test-type-reg-3", func() Container {
		return newTestContainer()
	})

	// Unregister
	err := UnregisterContainer("test-type-reg-3")
	if err != nil {
		t.Fatalf("UnregisterContainer() returned error: %v", err)
	}

	// Verify unregistered
	if IsRegistered("test-type-reg-3") {
		t.Error("expected container type to be unregistered")
	}
}

func TestRegistry_Unregister_NotRegistered(t *testing.T) {
	err := UnregisterContainer("never-registered-type")
	if err == nil {
		t.Error("expected error when unregistering non-existent type")
	}
}

func TestRegistry_GetRegisteredTypes(t *testing.T) {
	// Unregister first
	UnregisterContainer("test-type-reg-a")
	UnregisterContainer("test-type-reg-b")

	// Register two types
	RegisterContainer("test-type-reg-a", func() Container { return newTestContainer() })
	RegisterContainer("test-type-reg-b", func() Container { return newTestContainer() })

	types := GetRegisteredTypes()

	// Check we have both types
	foundA, foundB := false, false
	for _, tt := range types {
		if tt == "test-type-reg-a" {
			foundA = true
		}
		if tt == "test-type-reg-b" {
			foundB = true
		}
	}

	if !foundA || !foundB {
		t.Errorf("expected to find both registered types, foundA=%v foundB=%v", foundA, foundB)
	}

	// Clean up
	UnregisterContainer("test-type-reg-a")
	UnregisterContainer("test-type-reg-b")
}

func TestRegistry_IsRegistered(t *testing.T) {
	// Unregister first
	UnregisterContainer("test-type-check")

	if IsRegistered("test-type-check") {
		t.Error("expected false for unregistered type")
	}

	RegisterContainer("test-type-check", func() Container { return newTestContainer() })

	if !IsRegistered("test-type-check") {
		t.Error("expected true for registered type")
	}

	// Clean up
	UnregisterContainer("test-type-check")
}

func TestRegistry_RegisterSingleton(t *testing.T) {
	// Unregister first
	UnregisterContainer("test-type-singleton")

	// Create a singleton instance
	instance := newTestContainer()

	// Register as singleton
	err := RegisterSingleton("test-type-singleton", instance)
	if err != nil {
		t.Fatalf("RegisterSingleton() returned error: %v", err)
	}

	// Verify registered as singleton
	if !IsSingleton("test-type-singleton") {
		t.Error("expected container type to be registered as singleton")
	}

	// Verify GetContainer returns the same instance
	c1, err := GetContainer("test-type-singleton")
	if err != nil {
		t.Fatalf("GetContainer() returned error: %v", err)
	}
	c2, err := GetContainer("test-type-singleton")
	if err != nil {
		t.Fatalf("GetContainer() returned error: %v", err)
	}

	// Same instance should be returned
	if c1 != c2 {
		t.Error("expected GetContainer to return the same singleton instance")
	}

	// Clean up
	UnregisterContainer("test-type-singleton")
}

func TestRegistry_RegisterSingleton_Duplicate(t *testing.T) {
	// Unregister first
	UnregisterContainer("test-type-singleton-dup")

	instance1 := newTestContainer()
	instance2 := newTestContainer()

	// Register first singleton
	err := RegisterSingleton("test-type-singleton-dup", instance1)
	if err != nil {
		t.Fatalf("RegisterSingleton() returned error: %v", err)
	}

	// Register again should fail
	err = RegisterSingleton("test-type-singleton-dup", instance2)
	if err == nil {
		t.Error("expected error when registering duplicate singleton")
	}

	// Clean up
	UnregisterContainer("test-type-singleton-dup")
}

func TestRegistry_SetContainer(t *testing.T) {
	// Unregister first
	UnregisterContainer("test-type-set")

	instance := newTestContainer()

	// Set singleton instance directly
	err := SetContainer("test-type-set", instance)
	if err != nil {
		t.Fatalf("SetContainer() returned error: %v", err)
	}

	// Verify GetContainer returns the instance
	c, err := GetContainer("test-type-set")
	if err != nil {
		t.Fatalf("GetContainer() returned error: %v", err)
	}
	if c != instance {
		t.Error("expected GetContainer to return the set instance")
	}

	// Clean up
	UnregisterContainer("test-type-set")
}

func TestRegistry_Init(t *testing.T) {
	// Unregister first
	UnregisterContainer("test-type-init")

	// Register a factory
	RegisterContainer("test-type-init", func() Container {
		return newTestContainer()
	})

	// Create container
	c, err := NewContainer("test-type-init")
	if err != nil {
		t.Fatalf("NewContainer() returned error: %v", err)
	}

	// Call Init
	err = c.Init(nil)
	if err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}

	// Clean up
	UnregisterContainer("test-type-init")
}
