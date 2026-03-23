package ping

import "time"

// NodeLoader defines how ping nodes are loaded from a source.
type NodeLoader interface {
	// Load returns the list of ping nodes from this source.
	Load() ([]*PingNodeInfo, error)
	// Name returns the loader type name for logging.
	Name() string
}

// ReloadableLoader is an optional interface for loaders that support periodic reload.
type ReloadableLoader interface {
	NodeLoader
	// StartReload starts a goroutine that periodically reloads nodes.
	// The onChange callback is called with the new node list after each reload.
	// Returns a stop function to cancel the reload goroutine.
	StartReload(interval time.Duration, onChange func([]*PingNodeInfo)) (stop func())
}
