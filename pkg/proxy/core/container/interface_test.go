package container

import (
	"context"
	"errors"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// ErrContainerAlreadyRunning is a test error
var ErrContainerAlreadyRunning = errors.New("container already running")

// Is checks if error matches target
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// mockContainer implements Container for testing
type mockContainer struct {
	running bool
	version string
}

func (m *mockContainer) Start() error {
	if m.running {
		return ErrContainerAlreadyRunning
	}
	m.running = true
	return nil
}

func (m *mockContainer) Stop() error {
	m.running = false
	return nil
}

func (m *mockContainer) Restart() error {
	m.running = false
	m.running = true
	return nil
}

func (m *mockContainer) Reload() error {
	return nil
}

func (m *mockContainer) IsRunning() bool {
	return m.running
}

func (m *mockContainer) Version() string {
	return m.version
}

func (m *mockContainer) Type() contracts.ContainerType {
	return contracts.ContainerXray
}

func (m *mockContainer) ConfigFile() string {
	return "/etc/xray/config.json"
}

func (m *mockContainer) Update(ctx context.Context, req UpdateRequest) (*UpdateResult, error) {
	return &UpdateResult{
		FromVersion: m.version,
		ToVersion:   req.TargetTag,
		Tag:         req.TargetTag,
	}, nil
}

func (m *mockContainer) GetRunFunc() (func() error, func() error)      { return nil, nil }
func (m *mockContainer) RemoveInboundConfig(tag string) error           { return nil }
func (m *mockContainer) GetInboundConfig(tag string) (Inbound, error)   { return nil, nil }
func (m *mockContainer) ListInboundConfigs() []Inbound                  { return nil }
func (m *mockContainer) FastAddInbound(string, map[string]any) error    { return nil }
func (m *mockContainer) UserEventChannel() <-chan usermanager.UserEvent  { return nil }
func (m *mockContainer) Init(config any) error                          { return nil }
func (m *mockContainer) GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	return nil, nil
}

func TestRestartPolicy_Constants(t *testing.T) {
	if RestartPolicyAlways != "always" {
		t.Errorf("RestartPolicyAlways = %q, want %q", RestartPolicyAlways, "always")
	}
	if RestartPolicyOnFailure != "on-failure" {
		t.Errorf("RestartPolicyOnFailure = %q, want %q", RestartPolicyOnFailure, "on-failure")
	}
	if RestartPolicyNever != "never" {
		t.Errorf("RestartPolicyNever = %q, want %q", RestartPolicyNever, "never")
	}
}

func TestUpdateRequest_Fields(t *testing.T) {
	req := UpdateRequest{
		TargetTag:     "v1.8.4",
		RestartPolicy: RestartPolicyAlways,
		DryRun:        false,
	}

	if req.TargetTag != "v1.8.4" {
		t.Errorf("TargetTag = %q, want %q", req.TargetTag, "v1.8.4")
	}
	if req.RestartPolicy != RestartPolicyAlways {
		t.Errorf("RestartPolicy = %q, want %q", req.RestartPolicy, RestartPolicyAlways)
	}
}

func TestUpdateResult_Fields(t *testing.T) {
	result := UpdateResult{
		FromVersion:      "1.8.3",
		ToVersion:        "1.8.4",
		Tag:              "v1.8.4",
		ChecksumVerified: true,
		Restarted:        true,
	}

	if result.FromVersion != "1.8.3" {
		t.Errorf("FromVersion = %q, want %q", result.FromVersion, "1.8.3")
	}
	if result.ToVersion != "1.8.4" {
		t.Errorf("ToVersion = %q, want %q", result.ToVersion, "1.8.4")
	}
	if !result.ChecksumVerified {
		t.Error("expected ChecksumVerified = true")
	}
	if !result.Restarted {
		t.Error("expected Restarted = true")
	}
}

func TestContainer_Start_AlreadyRunning(t *testing.T) {
	container := &mockContainer{running: true, version: "1.8.4"}

	err := container.Start()
	if err == nil {
		t.Fatal("expected error for already running")
	}
	if !Is(err, ErrContainerAlreadyRunning) {
		t.Errorf("expected ErrContainerAlreadyRunning, got %v", err)
	}
}

func TestContainer_Update(t *testing.T) {
	container := &mockContainer{running: true, version: "1.8.4"}

	req := UpdateRequest{
		TargetTag:     "v1.8.5",
		RestartPolicy: RestartPolicyAlways,
	}

	result, err := container.Update(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FromVersion != "1.8.4" {
		t.Errorf("FromVersion = %q, want %q", result.FromVersion, "1.8.4")
	}
	if result.ToVersion != "v1.8.5" {
		t.Errorf("ToVersion = %q, want %q", result.ToVersion, "v1.8.5")
	}
}
