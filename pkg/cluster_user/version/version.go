package version

import (
	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
)

// IsNewer reports whether a's version is strictly newer than b's.
// UpdatedAtUs is compared first. When equal, OriginNode is compared
// lexicographically as a stable tie-break (higher string wins).
func IsNewer(a, b *clusteruser.ClusterUser) bool {
	if a.UpdatedAtUs != b.UpdatedAtUs {
		return a.UpdatedAtUs > b.UpdatedAtUs
	}
	return a.OriginNode > b.OriginNode
}
