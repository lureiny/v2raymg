// Package container defines the Container interface and core types.
package container

import (
	"fmt"
	"sync"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// ContainerFactory is a function that creates a Container instance.
type ContainerFactory func() Container

// containerRegistry stores registered container types and singleton instances.
type containerRegistry struct {
	mu         sync.RWMutex
	factories  map[contracts.ContainerType]ContainerFactory
	instances  map[contracts.ContainerType]Container // singleton instances
	singletonM sync.Mutex
}

var (
	// globalRegistry is the singleton container registry.
	globalRegistry = &containerRegistry{
		factories: make(map[contracts.ContainerType]ContainerFactory),
		instances: make(map[contracts.ContainerType]Container),
	}
)

// RegisterContainer registers a container factory for a given container type.
// Returns error if already registered.
// This is for non-singleton use case - each call to NewContainer creates a new instance.
func RegisterContainer(kind contracts.ContainerType, factory ContainerFactory) error {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	if _, exists := globalRegistry.factories[kind]; exists {
		return fmt.Errorf("container type %q already registered", kind)
	}
	globalRegistry.factories[kind] = factory
	return nil
}

// RegisterContainerFunc is a convenience function that wraps a factory function.
func RegisterContainerFunc(kind contracts.ContainerType, fn func() Container) error {
	return RegisterContainer(kind, fn)
}

// RegisterSingleton registers a container singleton instance for a given container type.
// The instance will be returned by GetContainer() - same instance for all callers.
// Returns error if already registered (as singleton or factory).
func RegisterSingleton(kind contracts.ContainerType, instance Container) error {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	// Check both factories and instances
	if _, exists := globalRegistry.factories[kind]; exists {
		return fmt.Errorf("container type %q already registered as factory", kind)
	}
	if _, exists := globalRegistry.instances[kind]; exists {
		return fmt.Errorf("container type %q already registered as singleton", kind)
	}
	globalRegistry.instances[kind] = instance
	return nil
}

// UnregisterContainer removes a container type from the registry.
// Returns error if not registered.
func UnregisterContainer(kind contracts.ContainerType) error {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	// Check both factories and instances
	if _, exists := globalRegistry.factories[kind]; !exists {
		if _, exists := globalRegistry.instances[kind]; !exists {
			return fmt.Errorf("container type %q not registered", kind)
		}
		delete(globalRegistry.instances, kind)
		return nil
	}
	delete(globalRegistry.factories, kind)
	return nil
}

// NewContainer creates a new container instance from the global registry.
// Returns error if the container type is not registered.
// Note: This creates a NEW instance each time - for singleton use GetContainer.
func NewContainer(kind contracts.ContainerType) (Container, error) {
	globalRegistry.mu.RLock()
	factory, exists := globalRegistry.factories[kind]
	globalRegistry.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown container type: %q", kind)
	}

	return factory(), nil
}

// GetContainer returns the singleton container instance for the given type.
// If registered as singleton, returns the same instance.
// If registered as factory, creates a new instance (lazy creation).
// Returns error if the container type is not registered.
func GetContainer(kind contracts.ContainerType) (Container, error) {
	// First check if singleton exists
	globalRegistry.singletonM.Lock()
	instance, exists := globalRegistry.instances[kind]
	globalRegistry.singletonM.Unlock()

	if exists {
		return instance, nil
	}

	// If not a singleton, create via factory
	return NewContainer(kind)
}

// SetContainer sets a singleton container instance directly.
// This is useful for testing or when the instance is created externally.
func SetContainer(kind contracts.ContainerType, instance Container) error {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	// Check if factory already registered
	if _, exists := globalRegistry.factories[kind]; exists {
		return fmt.Errorf("container type %q is registered as factory, cannot set singleton", kind)
	}

	globalRegistry.singletonM.Lock()
	globalRegistry.instances[kind] = instance
	globalRegistry.singletonM.Unlock()
	return nil
}

// GetRegisteredTypes returns all registered container types (both factories and singletons).
func GetRegisteredTypes() []contracts.ContainerType {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	types := make([]contracts.ContainerType, 0, len(globalRegistry.factories)+len(globalRegistry.instances))
	for kind := range globalRegistry.factories {
		types = append(types, kind)
	}
	for kind := range globalRegistry.instances {
		types = append(types, kind)
	}
	return types
}

// IsRegistered returns true if the container type is registered.
func IsRegistered(kind contracts.ContainerType) bool {
	globalRegistry.mu.RLock()
	_, factoryExists := globalRegistry.factories[kind]
	globalRegistry.mu.RUnlock()

	if factoryExists {
		return true
	}

	globalRegistry.singletonM.Lock()
	_, instanceExists := globalRegistry.instances[kind]
	globalRegistry.singletonM.Unlock()

	return instanceExists
}

// IsSingleton returns true if the container type is registered as a singleton.
func IsSingleton(kind contracts.ContainerType) bool {
	globalRegistry.singletonM.Lock()
	_, exists := globalRegistry.instances[kind]
	globalRegistry.singletonM.Unlock()
	return exists
}

// --- Factory-based registry (new pattern) ---

var (
	factoryMap   = map[contracts.ContainerType]Factory{}
	factoryMapMu sync.RWMutex
)

// RegisterFactory registers a container Factory for the given type.
// Calling this from init() is the preferred way to register container implementations.
// Overwrites any previously registered factory for the same type.
func RegisterFactory(kind contracts.ContainerType, f Factory) {
	factoryMapMu.Lock()
	defer factoryMapMu.Unlock()
	factoryMap[kind] = f
}

// GetFactory returns the Factory registered for the given container type.
func GetFactory(kind contracts.ContainerType) (Factory, bool) {
	factoryMapMu.RLock()
	defer factoryMapMu.RUnlock()
	f, ok := factoryMap[kind]
	return f, ok
}

// Create creates a new Container using the registered Factory for the given type.
func Create(kind contracts.ContainerType, opts BuildOptions) (Container, error) {
	f, ok := GetFactory(kind)
	if !ok {
		return nil, fmt.Errorf("no factory registered for container type %q", kind)
	}
	return f.New(opts)
}

// RegisteredTypes returns all container types registered via RegisterFactory.
func RegisteredTypes() []contracts.ContainerType {
	factoryMapMu.RLock()
	defer factoryMapMu.RUnlock()
	types := make([]contracts.ContainerType, 0, len(factoryMap))
	for k := range factoryMap {
		types = append(types, k)
	}
	return types
}
