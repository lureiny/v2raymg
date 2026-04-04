package version_test

import (
	"testing"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
	"github.com/lureiny/v2raymg/pkg/cluster_user/version"
)

func user(updatedAtUs int64, originNode string) *clusteruser.ClusterUser {
	return &clusteruser.ClusterUser{
		Username:    "alice",
		UpdatedAtUs: updatedAtUs,
		OriginNode:  originNode,
	}
}

func TestIsNewer_HigherTimestamp(t *testing.T) {
	a := user(2000, "node-a")
	b := user(1000, "node-b")
	if !version.IsNewer(a, b) {
		t.Error("a has higher timestamp, expected IsNewer(a,b)=true")
	}
}

func TestIsNewer_LowerTimestamp(t *testing.T) {
	a := user(1000, "node-a")
	b := user(2000, "node-b")
	if version.IsNewer(a, b) {
		t.Error("a has lower timestamp, expected IsNewer(a,b)=false")
	}
}

func TestIsNewer_EqualTimestamp_LexHigherNode(t *testing.T) {
	// "node-z" > "node-a" lexicographically, so a is newer
	a := user(1000, "node-z")
	b := user(1000, "node-a")
	if !version.IsNewer(a, b) {
		t.Error("equal timestamp, a has higher OriginNode lex, expected IsNewer(a,b)=true")
	}
}

func TestIsNewer_EqualTimestamp_LexLowerNode(t *testing.T) {
	// "node-a" < "node-z" lexicographically, so a is NOT newer
	a := user(1000, "node-a")
	b := user(1000, "node-z")
	if version.IsNewer(a, b) {
		t.Error("equal timestamp, a has lower OriginNode lex, expected IsNewer(a,b)=false")
	}
}

func TestIsNewer_EqualTimestamp_EqualNode(t *testing.T) {
	// Identical versions: neither is newer
	a := user(1000, "node-a")
	b := user(1000, "node-a")
	if version.IsNewer(a, b) {
		t.Error("identical versions, expected IsNewer(a,b)=false")
	}
}

func TestIsNewer_ZeroTimestamp(t *testing.T) {
	a := user(0, "node-b")
	b := user(0, "node-a")
	if !version.IsNewer(a, b) {
		t.Error("zero timestamp tie-break: 'node-b' > 'node-a', expected IsNewer(a,b)=true")
	}
}
