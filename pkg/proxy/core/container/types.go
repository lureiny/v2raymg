// Package container defines the Container interface and core types.
package container

import "github.com/lureiny/v2raymg/pkg/proxy/core/contracts"

// ContainerType is an alias for contracts.ContainerType.
// It represents the proxy software flavour.
type ContainerType = contracts.ContainerType

// Convenience constants for common container types
const (
	ContainerXray     ContainerType = contracts.ContainerXray
	ContainerV2ray    ContainerType = contracts.ContainerV2ray
	ContainerHysteria ContainerType = contracts.ContainerHysteria
	ContainerSnell    ContainerType = contracts.ContainerSnell
)
