// Package container defines the Container interface and core types.
package container

import (
	"context"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// Container represents a managed proxy process.
type Container interface {
	// Init initializes the container with the given configuration.
	// This is called once after the container is created, before Start.
	// The config parameter is implementation-specific:
	// - xray: *xray.ExecutorConfig
	// - Other implementations: their own config types
	// Returns error if initialization fails.
	// If no config is needed, implementations should accept nil and return nil.
	Init(config any) error

	Start() error
	Stop() error
	Restart() error
	Reload() error
	IsRunning() bool
	Version() string
	Type() ContainerType
	ConfigFile() string
	Update(ctx context.Context, req UpdateRequest) (*UpdateResult, error)

	// GetRunFunc returns the run and stop functions for the container.
	// - run: executed when Start() is called
	// - stop: executed when Stop() is called
	// Returns nil, nil if not implemented.
	GetRunFunc() (func() error, func() error)

	// Inbound management - CRUD operations for inbounds
	RemoveInboundConfig(tag string) error
	GetInboundConfig(tag string) (inbound.Inbound, error)
	ListInboundConfigs() []inbound.Inbound

	// FastAddInbound quickly adds an inbound with minimal parameters.
	// - tag: required, unique identifier for the inbound
	// - params: optional parameters that include:
	//   - "protocol": required, proxy protocol (vmess/vless/trojan/shadowsocks)
	//   - "port": optional, defaults to random available port
	//   - "listen": optional, defaults to "0.0.0.0"
	//   - "security": optional, defaults to "tls" for non-ss protocols
	//   - "transport": optional, defaults to "tcp"
	//   - Protocol-specific required params:
	//     - shadowsocks: "method", "password"
	// Returns error if tag is empty or protocol is missing.
	FastAddInbound(tag string, params map[string]any) error

	// UserEventChannel returns the container's channel for receiving user events.
	// The container should distribute events to appropriate inbounds.
	// Returns nil if UserManager is not set.
	UserEventChannel() <-chan usermanager.UserEvent

	// GetUserSubscriptions returns subscription specs for the given user.
	// Each inbound that the user has valid credentials for produces one SubscriptionSpec.
	// Returns nil, nil if the container has no subscriptions for the user.
	GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error)
}

// RuntimeAPI provides gRPC-based runtime operations.
type RuntimeAPI interface {
	AddInboundNative(nativeInboundJSON []byte) error
	RemoveInboundNative(tag string) error
	AddUser(tag string, user contracts.UserSpec) error
	RemoveUser(tag string, email string) error
	QueryStats(pattern string, reset bool) (map[string]*contracts.Stats, error)
}

// UpdateRequest holds the parameters for a container update.
type UpdateRequest struct {
	TargetTag     string
	RestartPolicy RestartPolicy
	DryRun        bool
	SourceConfig  *ReleaseSourceConfig
}

// UpdateResult holds the result of a container update.
type UpdateResult struct {
	FromVersion      string
	ToVersion        string
	Tag              string
	ChecksumVerified bool
	Restarted        bool
	Error            error
}

// RestartPolicy defines when to restart after update.
type RestartPolicy string

const (
	RestartPolicyAlways    RestartPolicy = "always"
	RestartPolicyOnFailure RestartPolicy = "on-failure"
	RestartPolicyNever     RestartPolicy = "never"
)

// ReleaseSourceConfig holds the source configuration for updates.
type ReleaseSourceConfig struct {
	Provider          string
	Owner             string
	Repo              string
	AssetName         string
	ChecksumAssetName string
}
