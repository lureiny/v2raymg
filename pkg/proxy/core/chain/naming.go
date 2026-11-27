package chain

import (
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// ResolveSubscriptionNodeName resolves the base NodeName placed into a container's
// subscription URI/spec.
//
// For chain inbounds, it intentionally omits the node prefix and uses:
//  1. Explicit/persisted meta.Remark
//  2. Backend proxy name parsed from meta.ChainOutboundURI (#fragment / VMess ps)
//  3. Inbound tag fallback
//
// For direct inbounds, it uses "<reqNodeName>-<remark>" when remark is set
// (or "<remark>" when reqNodeName is empty), otherwise reqNodeName, or tag if empty.
func ResolveSubscriptionNodeName(reqNodeName, tag string, meta contracts.InboundMeta) string {
	nodePrefix := strings.TrimSpace(reqNodeName)
	remark := strings.TrimSpace(meta.Remark)

	if meta.InboundType == contracts.InboundTypeChain {
		if remark != "" {
			return remark
		}
		if meta.ChainOutboundURI != "" {
			if spec, err := ParseURI(meta.ChainOutboundURI); err == nil {
				if backendName := strings.TrimSpace(spec.Remark); backendName != "" {
					return backendName
				}
			}
		}
		return tag
	}

	if remark != "" {
		if nodePrefix == "" {
			return remark
		}
		return nodePrefix + "-" + remark
	}
	if nodePrefix != "" {
		return nodePrefix
	}
	return tag
}
