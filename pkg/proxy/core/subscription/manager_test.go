package subscription

import (
	"context"
	"errors"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// ============================================================
// Mock implementations
// ============================================================

// mockUserGetter implements userGetter.
type mockUserGetter struct {
	user *contracts.UserSpec
	err  error
}

func (m *mockUserGetter) GetUser(username string) (*contracts.UserSpec, error) {
	return m.user, m.err
}

// mockContainer implements container.Container for testing.
type mockContainer struct {
	kind contracts.ContainerType
	subs []contracts.SubscriptionSpec
	err  error
}

func (c *mockContainer) Init(_ any) error                                          { return nil }
func (c *mockContainer) Start() error                                              { return nil }
func (c *mockContainer) Stop() error                                               { return nil }
func (c *mockContainer) Restart() error                                            { return nil }
func (c *mockContainer) Reload() error                                             { return nil }
func (c *mockContainer) IsRunning() bool                                           { return true }
func (c *mockContainer) Version() string                                           { return "" }
func (c *mockContainer) Type() contracts.ContainerType                             { return c.kind }
func (c *mockContainer) ConfigFile() string                                        { return "" }
func (c *mockContainer) GetRunFunc() (func() error, func() error)                  { return nil, nil }
func (c *mockContainer) RemoveInboundConfig(_ string) error                        { return nil }
func (c *mockContainer) GetInboundConfig(_ string) (inbound.Inbound, error)        { return nil, nil }
func (c *mockContainer) ListInboundConfigs() []inbound.Inbound                     { return nil }
func (c *mockContainer) FastAddInbound(_ string, _ map[string]any) error           { return nil }
func (c *mockContainer) UserEventChannel() <-chan usermanager.UserEvent             { return nil }
func (c *mockContainer) Update(_ context.Context, _ container.UpdateRequest) (*container.UpdateResult, error) {
	return nil, nil
}
func (c *mockContainer) GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	return c.subs, c.err
}

// mockContainerSource implements containerSource.
type mockContainerSource struct {
	containers map[contracts.ContainerType]container.Container
}

func newMockContainerSource() *mockContainerSource {
	return &mockContainerSource{containers: make(map[contracts.ContainerType]container.Container)}
}

func (s *mockContainerSource) Types() []contracts.ContainerType {
	types := make([]contracts.ContainerType, 0, len(s.containers))
	for t := range s.containers {
		types = append(types, t)
	}
	return types
}

func (s *mockContainerSource) Get(kind contracts.ContainerType) (container.Container, bool) {
	c, ok := s.containers[kind]
	return c, ok
}

// newTestManager builds a Manager backed by mock dependencies.
func newTestManager(cs containerSource, ug userGetter) *Manager {
	return &Manager{containers: cs, users: ug}
}

// ============================================================
// Tests
// ============================================================

func TestGetSubscription_userNotFound(t *testing.T) {
	cs := newMockContainerSource()
	ug := &mockUserGetter{err: errors.New("user not found")}
	m := newTestManager(cs, ug)

	req := contracts.SubscriptionRequest{User: contracts.UserSpec{Username: "ghost"}}
	_, err := m.GetSubscription(req)
	if err == nil {
		t.Fatal("expected error for unknown user, got nil")
	}
}

func TestGetSubscription_userDisabled(t *testing.T) {
	cs := newMockContainerSource()
	ug := &mockUserGetter{user: &contracts.UserSpec{Username: "alice", DeletionState: "deleting"}}
	m := newTestManager(cs, ug)

	req := contracts.SubscriptionRequest{User: contracts.UserSpec{Username: "alice"}}
	_, err := m.GetSubscription(req)
	if err == nil {
		t.Fatal("expected error for disabled user, got nil")
	}
}

func TestGetSubscription_aggregatesAllContainers(t *testing.T) {
	spec1 := contracts.SubscriptionSpec{InboundTag: "in1", Protocol: contracts.ProtocolVLess}
	spec2 := contracts.SubscriptionSpec{InboundTag: "in2", Protocol: contracts.ProtocolVMess}

	cs := newMockContainerSource()
	cs.containers[contracts.ContainerXray] = &mockContainer{
		kind: contracts.ContainerXray,
		subs: []contracts.SubscriptionSpec{spec1},
	}
	cs.containers[contracts.ContainerType("other")] = &mockContainer{
		kind: contracts.ContainerType("other"),
		subs: []contracts.SubscriptionSpec{spec2},
	}

	ug := &mockUserGetter{user: &contracts.UserSpec{Username: "bob"}}
	m := newTestManager(cs, ug)

	req := contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "bob"},
		Host: "example.com",
	}
	specs, err := m.GetSubscription(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 2 {
		t.Errorf("expected 2 specs, got %d", len(specs))
	}

	tags := map[string]bool{}
	for _, s := range specs {
		tags[s.InboundTag] = true
	}
	if !tags["in1"] || !tags["in2"] {
		t.Errorf("expected specs with tags in1 and in2, got %v", tags)
	}
}

func TestGetSubscription_toleratesContainerError(t *testing.T) {
	spec1 := contracts.SubscriptionSpec{InboundTag: "in1"}

	cs := newMockContainerSource()
	cs.containers[contracts.ContainerXray] = &mockContainer{
		kind: contracts.ContainerXray,
		subs: []contracts.SubscriptionSpec{spec1},
	}
	cs.containers[contracts.ContainerType("failing")] = &mockContainer{
		kind: contracts.ContainerType("failing"),
		err:  errors.New("container unavailable"),
	}

	ug := &mockUserGetter{user: &contracts.UserSpec{Username: "carol"}}
	m := newTestManager(cs, ug)

	req := contracts.SubscriptionRequest{User: contracts.UserSpec{Username: "carol"}, Host: "h"}
	specs, err := m.GetSubscription(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the successful container's spec should appear.
	if len(specs) != 1 || specs[0].InboundTag != "in1" {
		t.Errorf("expected 1 spec from healthy container, got %v", specs)
	}
}
