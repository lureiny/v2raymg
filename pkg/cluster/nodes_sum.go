package cluster

import (
	"crypto/sha256"
	"sort"
	"strconv"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// ComputeNodesSum returns a 32-byte SHA-256 digest over a node set, plus the
// number of nodes folded in.
//
// The digest is the cheap "do we agree?" probe exchanged on every heartbeat in
// place of the full node directory. Two nodes that hold the same advertised set
// MUST produce the same sum, so the encoding is fully canonical:
//
//   - The input is sorted first. Callers derive the set from NodeManager, whose
//     map iteration order is random; folding unsorted would make two ALREADY
//     CONVERGED nodes compute different sums — a permanent false mismatch that
//     turns this optimisation into a pessimisation. The comparator is total
//     (name, then host, port, cluster) so even a hypothetical duplicate name
//     cannot reintroduce nondeterminism.
//   - Each field is written with a 4-byte big-endian length prefix (same scheme
//     as usermanager/sync.ComputeHash) so no concatenation of field values can
//     collide with a different split.
//   - Only the three identity fields are folded: name, host, port. These are
//     exactly the "meta info" that Node.Compare treats as uniquely identifying a
//     node. Per-node runtime state (in/out token, heartbeat timestamps,
//     CreateTime, isLocal) MUST NOT be folded in — it is local to each observer
//     and would make the sums differ forever.
//
// An empty set yields the SHA-256 of the empty input (a real 32-byte value),
// never a nil/empty slice: an empty sum on the wire is the sentinel for
// "peer did not provide one" (a legacy node).
func ComputeNodesSum(nodes []*proto.Node) (sum []byte, count int) {
	sorted := make([]*proto.Node, 0, len(nodes))
	for _, n := range nodes {
		if n == nil {
			continue
		}
		sorted = append(sorted, n)
	}
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.GetName() != b.GetName() {
			return a.GetName() < b.GetName()
		}
		if a.GetHost() != b.GetHost() {
			return a.GetHost() < b.GetHost()
		}
		return a.GetPort() < b.GetPort()
	})

	h := sha256.New()
	writeField := func(s string) {
		b := []byte(s)
		l := uint32(len(b))
		h.Write([]byte{byte(l >> 24), byte(l >> 16), byte(l >> 8), byte(l)})
		h.Write(b)
	}
	for _, n := range sorted {
		writeField(n.GetName())
		writeField(n.GetHost())
		writeField(strconv.FormatInt(int64(n.GetPort()), 10))
	}
	return h.Sum(nil), len(sorted)
}
