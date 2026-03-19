package appconfig

import "sync"

var (
	globalCfg *AppConfig
	globalMu  sync.RWMutex
)

// Init loads the global configuration from the given file path.
// It calls LoadFromFile and stores the result globally.
// Returns an error if loading fails.
func Init(path string) error {
	cfg, err := LoadFromFile(path)
	if err != nil {
		return err
	}
	globalMu.Lock()
	globalCfg = cfg
	globalMu.Unlock()
	return nil
}

// InitWithValidate loads and validates the global configuration.
// It calls LoadFromFileWithValidate and stores the result globally.
// Returns an error if loading or validation fails.
func InitWithValidate(path string) error {
	cfg, err := LoadFromFileWithValidate(path)
	if err != nil {
		return err
	}
	globalMu.Lock()
	globalCfg = cfg
	globalMu.Unlock()
	return nil
}

// Get returns the global AppConfig, or nil if not initialized.
func Get() *AppConfig {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalCfg
}

// MustGet returns the global AppConfig, panics if not initialized.
func MustGet() *AppConfig {
	cfg := Get()
	if cfg == nil {
		panic("appconfig: not initialized, call Init first")
	}
	return cfg
}

// Set sets the global configuration directly (for advanced use).
// Useful when the caller constructs AppConfig manually.
func Set(cfg *AppConfig) {
	globalMu.Lock()
	globalCfg = cfg
	globalMu.Unlock()
}
