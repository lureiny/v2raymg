// Package container defines the Container interface and core types.
package container

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/store"
)

// ContainerEntry describes a single container's enable configuration.
type ContainerEntry struct {
	Type    contracts.ContainerType `yaml:"type"   json:"type"`
	Enabled bool                    `yaml:"enabled" json:"enabled"`
	// Config holds container-specific configuration as key-value pairs.
	// It is decoded via Factory.NewConfigObj().Decode(Config).
	Config map[string]any `yaml:"config" json:"config"`
}

// ContainerMgrConfig holds the full set of containers to manage.
type ContainerMgrConfig struct {
	Containers []ContainerEntry `yaml:"containers" json:"containers"`
}

// Restorable is an optional capability interface for containers that support
// state restoration after restart.
type Restorable interface {
	Restore(ctx context.Context) error
}

// ContainerMgr manages the lifecycle of multiple Container instances.
// It is agnostic to any specific container type — all creation is delegated
// to registered Factory implementations.
type ContainerMgr struct {
	mu        sync.RWMutex
	instances map[contracts.ContainerType]Container
	buildOpts BuildOptions // common dependencies provided at construction time
}

// NewContainerMgr creates a new ContainerMgr with the given store manager and build options.
func NewContainerMgr(storeMgr *store.StoreManager, opts BuildOptions) *ContainerMgr {
	opts.StoreMgr = storeMgr
	return &ContainerMgr{
		instances: make(map[contracts.ContainerType]Container),
		buildOpts: opts,
	}
}

// LoadFromConfig instantiates all enabled containers described by cfg.
// Disabled entries are skipped. Returns the first error encountered (fail-fast).
func (m *ContainerMgr) LoadFromConfig(cfg ContainerMgrConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range cfg.Containers {
		if !entry.Enabled {
			continue
		}

		factory, ok := GetFactory(entry.Type)
		if !ok {
			return fmt.Errorf("no factory registered for container type %q", entry.Type)
		}

		cfgObj := factory.NewConfigObj()
		if err := cfgObj.Decode(entry.Config); err != nil {
			return fmt.Errorf("decode config for container type %q: %w", entry.Type, err)
		}

		opts := m.buildOpts
		opts.Config = cfgObj

		instance, err := factory.New(opts)
		if err != nil {
			return fmt.Errorf("create container type %q: %w", entry.Type, err)
		}

		m.instances[entry.Type] = instance
	}

	return nil
}

// SetForTest injects a container instance for testing purposes.
func (m *ContainerMgr) SetForTest(kind contracts.ContainerType, c Container) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.instances[kind] = c
}

// Get returns the container instance for the given type.
// Returns nil, false if the type is not loaded.
func (m *ContainerMgr) Get(kind contracts.ContainerType) (Container, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.instances[kind]
	return c, ok
}

// StartAll starts all loaded containers, then automatically restores state
// (forward rules, inbound records, etc.) for containers that implement Restorable.
// All containers are attempted; errors are accumulated and returned as a combined error.
func (m *ContainerMgr) StartAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errs []error
	for kind, c := range m.instances {
		if err := c.Start(); err != nil {
			errs = append(errs, fmt.Errorf("start container %q: %w", kind, err))
			continue
		}
		// Auto-restore after each successful start
		if r, ok := c.(Restorable); ok {
			if err := r.Restore(ctx); err != nil {
				errs = append(errs, fmt.Errorf("restore container %q: %w", kind, err))
			}
		}
	}
	return errors.Join(errs...)
}

// RestoreAll calls Restore on every loaded container that implements Restorable.
// Non-Restorable containers are silently skipped. All Restorable containers are
// attempted; errors are accumulated and returned as a combined error.
func (m *ContainerMgr) RestoreAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errs []error
	for kind, c := range m.instances {
		r, ok := c.(Restorable)
		if !ok {
			continue
		}
		if err := r.Restore(ctx); err != nil {
			errs = append(errs, fmt.Errorf("restore container %q: %w", kind, err))
		}
	}
	return errors.Join(errs...)
}

// StopAll stops all loaded containers. All containers are attempted; errors
// are accumulated and returned as a combined error.
func (m *ContainerMgr) StopAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errs []error
	for kind, c := range m.instances {
		if err := c.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("stop container %q: %w", kind, err))
		}
	}
	return errors.Join(errs...)
}

// Types returns the types of all currently loaded containers.
func (m *ContainerMgr) Types() []contracts.ContainerType {
	m.mu.RLock()
	defer m.mu.RUnlock()
	types := make([]contracts.ContainerType, 0, len(m.instances))
	for k := range m.instances {
		types = append(types, k)
	}
	return types
}

// TypeStrings returns the types of all currently loaded containers as plain strings.
func (m *ContainerMgr) TypeStrings() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.instances))
	for k := range m.instances {
		out = append(out, string(k))
	}
	return out
}
