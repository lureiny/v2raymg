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

// PortClaimer is the process-wide port authority as a container sees it.
//
// Every port this process causes to be bound goes through one allocation
// table — user-facing forward ports via forward.AddRule, container-internal
// inbound ports via the methods below. Two tables means neither can promise
// uniqueness, so there is exactly one implementation in production:
// *forward.DefaultForwardManager, created once in cmd/server.go and injected
// from there. It satisfies this interface structurally; the narrow shape here
// keeps container packages from importing pkg/proxy/forward for three methods.
//
// A container claiming a port here is claiming it for its own internal
// 127.0.0.1 listener. The public relay in front of that listener is a separate
// concern created via UserManager.GetBindPort -> forward.AddRule.
type PortClaimer interface {
	AllocatePort() (uint32, error)
	AllocateSpecificPort(port uint32) error
	ReleasePort(port uint32)
}

// BuildOptions carries common dependencies injected when creating a Container.
type BuildOptions struct {
	UserManager *usermanager.UserManager
	StoreMgr    *store.StoreManager

	// PortClaimer is the shared port authority. Containers must route every
	// inbound port through it — see ClaimInboundPort. nil disables claiming
	// entirely, which is only correct for unit tests that construct a
	// container in isolation; production always injects it.
	PortClaimer PortClaimer
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

	// ProxyHost is the node's public hostname/IP (NodeConfig.ProxyHost).
	// Containers that need a default TLS domain (e.g., Hysteria when its
	// own config omits both `host` and `domain`) fall back to this.
	ProxyHost string
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
