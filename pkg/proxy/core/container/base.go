// Package container defines the Container interface and core types.
package container

import (
	"fmt"
	"sync"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/errors"
)

// ContainerState represents the lifecycle state of a container.
type ContainerState int

const (
	// ContainerStateStopped indicates the container is not running.
	ContainerStateStopped ContainerState = iota
	// ContainerStateStarting indicates the container is starting.
	ContainerStateStarting
	// ContainerStateRunning indicates the container is running.
	ContainerStateRunning
	// ContainerStateStopping indicates the container is stopping.
	ContainerStateStopping
)

// String returns the string representation of ContainerState.
func (s ContainerState) String() string {
	switch s {
	case ContainerStateStopped:
		return "stopped"
	case ContainerStateStarting:
		return "starting"
	case ContainerStateRunning:
		return "running"
	case ContainerStateStopping:
		return "stopping"
	default:
		return "unknown"
	}
}

// Hooks defines lifecycle callback functions for BaseContainer.
// Implementers MUST provide run function - nil is not allowed.
type Hooks interface {
	// GetRunFunc returns the run and stop functions for the container.
	// - run: executed when Start() is called, performs actual start logic.
	//        MUST NOT be nil - BaseContainer will error if run is nil.
	// - stop: executed when Stop() is called, performs actual stop logic.
	//        Can be nil - BaseContainer will be idempotent.
	// The runFunc should NOT call MarkRunning() - that's handled by BaseContainer.
	// The stopFunc should NOT call MarkStopped() - that's handled by BaseContainer.
	GetRunFunc() (run func() error, stop func() error)
}

// BaseContainer provides common lifecycle management for container implementations.
// Embed this struct to get basic start/stop/state management functionality.
//
// IMPORTANT: Embedders MUST provide non-nil Hooks when creating BaseContainer.
// Without hooks, Start() will fail with explicit error.
//
// All fields are private to enforce usage through methods.
type BaseContainer struct {
	// opMu serializes the ENTIRE Start/Stop operation, including hook
	// execution, so a Start and a Stop can never run concurrently. It is the
	// only lock held across a hook; mu below is taken only for brief state
	// reads/writes, so State()/IsRunning() readers are never blocked by a slow
	// hook. Lock order: opMu is outermost — hooks may take container-level
	// locks (e.g. a container's own mu / process lock) strictly inside it,
	// never the reverse. Serializing Start against Stop removes the
	// Start/Stop interleavings that otherwise left orphan processes and a
	// lying state (Stopped while the process is still running).
	opMu          sync.Mutex
	mu            sync.RWMutex
	state         ContainerState
	containerType contracts.ContainerType
	stopChan      chan struct{}
	hooks         Hooks
	stopFunc      func() error // cached stop function from hooks
}

// NewBaseContainer creates a new BaseContainer with the given type and hooks.
// The hooks parameter MUST NOT be nil - BaseContainer requires hooks to function.
// Without hooks, Start() will return an error.
func NewBaseContainer(ct contracts.ContainerType, hooks Hooks) *BaseContainer {
	if hooks == nil {
		panic("BaseContainer: hooks must not be nil")
	}
	return &BaseContainer{
		containerType: ct,
		state:         ContainerStateStopped,
		stopChan:      make(chan struct{}),
		hooks:         hooks,
	}
}

// Type returns the container type.
func (b *BaseContainer) Type() contracts.ContainerType {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.containerType
}

// State returns the current container state.
func (b *BaseContainer) State() ContainerState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// IsRunning returns true if the container is in running state.
func (b *BaseContainer) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state == ContainerStateRunning
}

// GetRunFunc returns the run and stop functions from hooks.
// Panics if hooks is nil (should not happen if created via NewBaseContainer).
func (b *BaseContainer) GetRunFunc() (func() error, func() error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	// Should never be nil if created via NewBaseContainer
	if b.hooks == nil {
		panic("BaseContainer: hooks is nil - did you use NewBaseContainer?")
	}
	return b.hooks.GetRunFunc()
}

// Start starts the container if not already running.
// This method is idempotent - calling multiple times has the same effect as calling once.
//
// Returns error if:
// - hooks is nil (programming error)
// - run function from hooks is nil (programming error)
// - run function returns error
//
// The Start flow:
// 1. Verify hooks and run function are provided (panic if not)
// 2. Transition state from Stopped to Starting
// 3. Execute the run function
// 4. Save the stop function for later use
// 5. On success, transition state to Running (via MarkRunning)
// 6. On failure, transition back to Stopped with error
func (b *BaseContainer) Start() error {
	// Serialize the whole Start against any concurrent Stop (see opMu doc).
	b.opMu.Lock()
	defer b.opMu.Unlock()

	b.mu.Lock()

	// Verify hooks is set - this is a programming error if not
	if b.hooks == nil {
		b.mu.Unlock()
		panic("BaseContainer: Start() called with nil hooks - use NewBaseContainer with valid hooks")
	}

	// Check current state
	switch b.state {
	case ContainerStateRunning:
		// Already running, idempotent success
		b.mu.Unlock()
		return nil
	case ContainerStateStarting:
		// Already starting, wait for it
		b.mu.Unlock()
		return nil
	case ContainerStateStopping:
		// Wait for stop to complete, then start fresh
		// Create new stop channel
		b.stopChan = make(chan struct{})
		b.state = ContainerStateStarting
	case ContainerStateStopped:
		// Normal start
		b.state = ContainerStateStarting
		// Reset stop channel
		b.stopChan = make(chan struct{})
	default:
		b.mu.Unlock()
		return fmt.Errorf("unknown container state: %v", b.state)
	}

	// Get run func from hooks
	runFunc, stopFunc := b.hooks.GetRunFunc()

	// Verify run function is provided - this is a programming error
	if runFunc == nil {
		b.state = ContainerStateStopped
		b.mu.Unlock()
		panic("BaseContainer: GetRunFunc() returned nil run function - hooks must provide run function")
	}

	// Unlock before executing run func to avoid deadlock
	b.mu.Unlock()

	// Execute the run function
	if err := runFunc(); err != nil {
		// Failed to start, mark as stopped
		b.mu.Lock()
		b.state = ContainerStateStopped
		b.mu.Unlock()
		return errors.Wrap(errors.ErrContainerStartFailed, "run function failed", err)
	}

	// Success - save stop function and mark as running
	b.mu.Lock()
	b.stopFunc = stopFunc
	b.mu.Unlock()
	b.MarkRunning()
	return nil
}

// MarkRunning marks the container as running after successful start.
// Call this after actual start logic completes.
func (b *BaseContainer) MarkRunning() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == ContainerStateStarting {
		b.state = ContainerStateRunning
	}
}

// MarkStopped marks the container as stopped after successful stop.
// Call this after actual stop logic completes.
func (b *BaseContainer) MarkStopped() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = ContainerStateStopped
}

// Stop stops the container if running.
// This method is idempotent - calling multiple times has the same effect as calling once.
//
// The Stop flow:
// 1. Transition state from Running to Stopping
// 2. Close stop channel to signal stop
// 3. Execute the stop function (if provided)
// 4. Transition state to Stopped (via MarkStopped)
func (b *BaseContainer) Stop() error {
	// Serialize the whole Stop against any concurrent Start (see opMu doc).
	// Restart() must NOT hold opMu: it calls Stop() then Start(), which each
	// take opMu themselves; holding it in Restart would self-deadlock.
	b.opMu.Lock()
	defer b.opMu.Unlock()

	b.mu.Lock()

	// Save stop function reference before changing state
	stopFunc := b.stopFunc

	switch b.state {
	case ContainerStateStopped:
		// Already stopped, idempotent success
		b.mu.Unlock()
		return nil
	case ContainerStateStopping:
		// Already stopping, idempotent success
		b.mu.Unlock()
		return nil
	case ContainerStateStarting:
		// Was starting, now stop it
		b.state = ContainerStateStopping
		b.stopFunc = nil // clear cached stop function
		if b.stopChan != nil {
			close(b.stopChan)
		}
		b.mu.Unlock()
		b.MarkStopped()
		return nil
	case ContainerStateRunning:
		// Normal stop
		b.state = ContainerStateStopping
		b.stopFunc = nil // clear cached stop function
		if b.stopChan != nil {
			close(b.stopChan)
		}
		b.mu.Unlock()

		// Execute the stop function (if provided)
		if stopFunc != nil {
			if err := stopFunc(); err != nil {
				// Even if stop function fails, we still mark as stopped
				b.mu.Lock()
				b.state = ContainerStateStopped
				b.mu.Unlock()
				return errors.Wrap(errors.ErrContainerStopFailed, "stop function failed", err)
			}
		}

		// Mark as stopped
		b.MarkStopped()
		return nil
	default:
		b.mu.Unlock()
		return fmt.Errorf("unknown container state: %v", b.state)
	}
}

// StopChan returns the stop channel for coordination.
// The channel is closed when Stop() is called.
func (b *BaseContainer) StopChan() <-chan struct{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.stopChan
}

// Restart is a default implementation that stops then starts.
// Override to provide more efficient implementation.
func (b *BaseContainer) Restart() error {
	if err := b.Stop(); err != nil {
		return err
	}
	// Wait for stop to complete or handle idempotently
	if err := b.Start(); err != nil {
		return err
	}
	// Mark as running after restart
	b.MarkRunning()
	return nil
}

// Required methods that MUST be implemented by Container (not provided by BaseContainer):
// - Reload() error
// - Update(ctx context.Context, req UpdateRequest) (*UpdateResult, error)
// - RemoveInboundConfig(tag string) error
// - GetInboundConfig(tag string) (Inbound, error)
// - ListInboundConfigs() []Inbound
// - UserEventChannel() <-chan usermanager.UserEvent
// - Version() string
// - ConfigFile() string
// - FastAddInbound(tag string, params map[string]any) error

// Common errors
var (
	ErrNotSupported = fmt.Errorf("operation not supported")
)
