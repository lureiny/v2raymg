package forward

import (
	"fmt"
	"sync"
)

// --- Global singleton ForwardManager ---

var (
	globalMu      sync.Mutex
	globalManager *DefaultForwardManager
	globalConfig  = PortAllocatorConfig{
		MinPort: 10000,
		MaxPort: 65535,
	}
)

// SetGlobalConfig sets the port allocator config used by the global singleton.
// Must be called before the first call to GlobalForwardManager().
// Not goroutine-safe with respect to GlobalForwardManager(); call it at init time.
func SetGlobalConfig(cfg PortAllocatorConfig) {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalManager != nil {
		panic("forward: SetGlobalConfig called after global ForwardManager already initialised")
	}
	globalConfig = cfg
}

// GlobalForwardManager returns the process-wide singleton ForwardManager.
// It is lazily initialised on first call.
func GlobalForwardManager() (ForwardManager, error) {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalManager != nil {
		return globalManager, nil
	}

	m, err := NewDefaultForwardManager(globalConfig)
	if err != nil {
		return nil, fmt.Errorf("forward: init global manager: %w", err)
	}
	globalManager = m
	return globalManager, nil
}

// ResetGlobalForwardManager closes and removes the global singleton.
// Intended for testing only.
func ResetGlobalForwardManager() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalManager != nil {
		globalManager.Close()
		globalManager = nil
	}
}
