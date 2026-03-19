// Package container defines the Container interface and core types.
package container

import (
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/store"
)

// ContainerConfig is the interface for container-specific configuration objects.
// Each container package implements this to allow decoding from a generic map.
type ContainerConfig interface {
	Decode(cfg map[string]any) error
}

// BuildOptions carries common dependencies injected when creating a Container.
type BuildOptions struct {
	UserManager *usermanager.UserManager
	StoreMgr    *store.StoreManager
	// CertManager holds the certificate manager (concrete type: *certmgmtservice.Manager).
	// Typed as any to avoid import cycles; containers type-assert to the interface they need.
	CertManager any
	Config      any // container-specific config object (implements ContainerConfig)

	// CertReader provides certificate file path lookup.
	// Used by containers that require TLS (e.g., Hysteria).
	CertReader interface {
		GetCertFiles(domain string) (certFile, keyFile string, ok bool)
	}

	// HTTPPort is the v2raymg HTTP server port, used for auth callbacks.
	HTTPPort int
}

// Factory creates Container instances for a specific container type.
// Implementations register themselves via RegisterFactory (typically in init()).
type Factory interface {
	// NewConfigObj returns a fresh, empty container-specific config object.
	// Callers decode into it, then pass it as BuildOptions.Config.
	NewConfigObj() ContainerConfig

	// New creates a new Container using the provided BuildOptions.
	New(opts BuildOptions) (Container, error)
}
