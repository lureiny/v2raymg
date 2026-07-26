package cluster_test

import (
	"bytes"
	"crypto/sha256"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

func protoNode(name, host string, port int32) *proto.Node {
	return &proto.Node{Name: name, Host: host, Port: port}
}

func sampleNodes() []*proto.Node {
	return []*proto.Node{
		protoNode("node-a", "10.0.0.1", 2000),
		protoNode("node-b", "10.0.0.2", 2001),
		protoNode("node-c", "10.0.0.3", 2002),
		protoNode("node-d", "10.0.0.4", 2003),
		protoNode("node-e", "10.0.0.5", 2004),
	}
}

// TestComputeNodesSum_OrderIndependent is the load-bearing regression for this
// whole optimisation: callers derive the node set from NodeManager's map, whose
// iteration order is random. If ComputeNodesSum did not sort internally, two
// already-converged nodes would compute different sums, mismatch on every
// heartbeat, and exchange the full directory forever — strictly worse than the
// behaviour being replaced. Removing the sort must fail this test.
func TestComputeNodesSum_OrderIndependent(t *testing.T) {
	base := sampleNodes()
	want, wantCount := cluster.ComputeNodesSum(base)
	if wantCount != len(base) {
		t.Fatalf("count = %d, want %d", wantCount, len(base))
	}

	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		shuffled := sampleNodes()
		rng.Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})
		got, gotCount := cluster.ComputeNodesSum(shuffled)
		if !bytes.Equal(got, want) {
			t.Fatalf("shuffle %d produced a different sum:\n got %x\nwant %x", i, got, want)
		}
		if gotCount != wantCount {
			t.Fatalf("shuffle %d count = %d, want %d", i, gotCount, wantCount)
		}
	}
}

// TestComputeNodesSum_EveryIdentityFieldMatters asserts the digest distinguishes
// a change in any of the three fields that uniquely identify a node (the same
// "meta info" Node.Compare enforces). A field that did not flip the sum would be
// a silent divergence the heartbeat could never detect.
func TestComputeNodesSum_EveryIdentityFieldMatters(t *testing.T) {
	base, _ := cluster.ComputeNodesSum(sampleNodes())

	mutations := map[string]func(*proto.Node){
		"name": func(n *proto.Node) { n.Name = "node-z" },
		"host": func(n *proto.Node) { n.Host = "10.9.9.9" },
		"port": func(n *proto.Node) { n.Port = 9999 },
	}
	for field, mutate := range mutations {
		nodes := sampleNodes()
		mutate(nodes[2])
		got, _ := cluster.ComputeNodesSum(nodes)
		if bytes.Equal(got, base) {
			t.Errorf("changing %s did not change the sum", field)
		}
	}
}

// TestComputeNodesSum_MembershipMatters covers add/remove of a whole node.
func TestComputeNodesSum_MembershipMatters(t *testing.T) {
	base, baseCount := cluster.ComputeNodesSum(sampleNodes())

	added := append(sampleNodes(), protoNode("node-f", "10.0.0.6", 2005))
	gotAdd, addCount := cluster.ComputeNodesSum(added)
	if bytes.Equal(gotAdd, base) {
		t.Error("adding a node did not change the sum")
	}
	if addCount != baseCount+1 {
		t.Errorf("count after add = %d, want %d", addCount, baseCount+1)
	}

	removed := sampleNodes()[:len(sampleNodes())-1]
	gotRemove, removeCount := cluster.ComputeNodesSum(removed)
	if bytes.Equal(gotRemove, base) {
		t.Error("removing a node did not change the sum")
	}
	if removeCount != baseCount-1 {
		t.Errorf("count after remove = %d, want %d", removeCount, baseCount-1)
	}
}

// TestComputeNodesSum_EmptySetIsNotTheWireSentinel guards the protocol contract:
// an empty nodes_sum on the wire means "peer did not provide one" (legacy node),
// so a genuinely empty node set must still hash to a real 32-byte value.
func TestComputeNodesSum_EmptySetIsNotTheWireSentinel(t *testing.T) {
	sum, count := cluster.ComputeNodesSum(nil)
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if len(sum) != sha256.Size {
		t.Fatalf("len(sum) = %d, want %d", len(sum), sha256.Size)
	}
	empty := sha256.Sum256(nil)
	if !bytes.Equal(sum, empty[:]) {
		t.Errorf("empty set sum = %x, want sha256 of empty input %x", sum, empty)
	}
}

// TestComputeNodesSum_SkipsNilEntries keeps a nil element from panicking or
// perturbing the digest.
func TestComputeNodesSum_SkipsNilEntries(t *testing.T) {
	want, wantCount := cluster.ComputeNodesSum(sampleNodes())

	withNil := sampleNodes()
	withNil = append(withNil[:2], append([]*proto.Node{nil}, withNil[2:]...)...)
	got, gotCount := cluster.ComputeNodesSum(withNil)
	if !bytes.Equal(got, want) {
		t.Errorf("nil entry changed the sum:\n got %x\nwant %x", got, want)
	}
	if gotCount != wantCount {
		t.Errorf("count = %d, want %d", gotCount, wantCount)
	}
}

// --- Cluster.GetAdvertisedNodes ---

// completeNode builds a node that satisfies IsCompleteRegister().
func completeNode(name, host string, port int32) *cluster.Node {
	n := &cluster.Node{Node: protoNode(name, host, port)}
	n.SetRecvHeartBeatTime(time.Now().Unix())
	n.SetReportHeartBeatTime(time.Now().Unix())
	return n
}

// TestGetAdvertisedNodes_IncludesSelf is the second load-bearing invariant: the
// advertised set must contain the advertising node itself. If it did not, node A
// would fold "everything except A" while B folds "everything except B", so two
// nodes with an identical cluster view would never agree on the sum and the
// mismatch path would run on every heartbeat forever.
func TestGetAdvertisedNodes_IncludesSelf(t *testing.T) {
	// Two peers that have completed mutual registration, each holding the same
	// three-node view — modelled the way InitEndNodeCluster does it, with the
	// local node in its own NodeManager.
	names := []string{"node-a", "node-b", "node-c"}

	sums := map[string][]byte{}
	for _, self := range names {
		c := newTestCluster("c1", "token")
		for i, name := range names {
			c.Add(completeNode(name, "10.0.0.1", int32(2000+i)))
		}
		nodes, sum := c.GetAdvertisedNodes()
		if _, ok := nodes[self]; !ok {
			t.Fatalf("advertised set of %q does not contain itself: %v", self, nodes)
		}
		if len(nodes) != len(names) {
			t.Fatalf("advertised set of %q has %d nodes, want %d", self, len(nodes), len(names))
		}
		sums[self] = sum
	}

	// Converged peers must agree.
	for _, self := range names[1:] {
		if !bytes.Equal(sums[self], sums[names[0]]) {
			t.Errorf("converged peers disagree on the sum: %s=%x %s=%x",
				names[0], sums[names[0]], self, sums[self])
		}
	}
}

// TestGetAdvertisedNodes_ExcludesIncompleteRegistration asserts a peer that has
// not completed mutual registration stays out of the advertised set, matching
// the filter the heartbeat response used before this change.
func TestGetAdvertisedNodes_ExcludesIncompleteRegistration(t *testing.T) {
	c := newTestCluster("c1", "token")
	c.Add(completeNode("node-a", "10.0.0.1", 2000))

	// Only one direction of the registration completed.
	halfway := &cluster.Node{Node: protoNode("node-b", "10.0.0.2", 2001)}
	halfway.SetRecvHeartBeatTime(time.Now().Unix())
	c.Add(halfway)

	// Never registered at all.
	c.Add(&cluster.Node{Node: protoNode("node-c", "10.0.0.3", 2002)})

	nodes, _ := c.GetAdvertisedNodes()
	if len(nodes) != 1 {
		t.Fatalf("advertised %d nodes, want 1: %v", len(nodes), nodes)
	}
	if _, ok := nodes["node-a"]; !ok {
		t.Errorf("advertised set missing the fully-registered node: %v", nodes)
	}
}

// TestGetAdvertisedNodes_RuntimeStateDoesNotAffectSum asserts per-observer
// runtime state is not folded into the digest. Tokens and heartbeat timestamps
// differ on every node by construction; folding any of them in would make the
// sums differ forever.
func TestGetAdvertisedNodes_RuntimeStateDoesNotAffectSum(t *testing.T) {
	build := func(mutate func(*cluster.Node)) []byte {
		c := newTestCluster("c1", "token")
		n := completeNode("node-a", "10.0.0.1", 2000)
		mutate(n)
		c.Add(n)
		_, sum := c.GetAdvertisedNodes()
		return sum
	}

	base := build(func(*cluster.Node) {})
	cases := map[string]func(*cluster.Node){
		"in token":   func(n *cluster.Node) { n.SetInToken("in-token-value") },
		"out token":  func(n *cluster.Node) { n.SetOutToken("out-token-value") },
		"createtime": func(n *cluster.Node) { n.CreateTime = time.Now().Unix() },
		"heartbeats": func(n *cluster.Node) {
			n.SetRecvHeartBeatTime(time.Now().Unix() - 5)
			n.SetReportHeartBeatTime(time.Now().Unix() - 3)
		},
	}
	for name, mutate := range cases {
		if got := build(mutate); !bytes.Equal(got, base) {
			t.Errorf("%s changed the sum (must not be folded in): got %x want %x", name, got, base)
		}
	}
}

// TestGetAdvertisedNodes_ConcurrentWithMembershipChurn mirrors what a heartbeat
// round actually does: one goroutine snapshots the advertised set while inbound
// RegisterNode/HeartBeat handlers add nodes and refresh their timestamps, and the
// filter loop reclaims stale ones. Under -race this catches unsynchronised reads
// introduced by the new digest path.
func TestGetAdvertisedNodes_ConcurrentWithMembershipChurn(t *testing.T) {
	c := newTestCluster("c1", "token")
	for i := 0; i < 8; i++ {
		c.Add(completeNode(string(rune('a'+i)), "10.0.0.1", int32(2000+i)))
	}

	const iters = 500
	var wg sync.WaitGroup

	// heartbeat rounds snapshotting the advertised set
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			nodes, sum := c.GetAdvertisedNodes()
			if len(sum) == 0 {
				t.Error("empty digest")
				return
			}
			_ = nodes
		}
	}()
	// inbound handlers adding nodes and refreshing heartbeat timestamps
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			c.Add(completeNode("churn", "10.0.9.9", 2999))
			if n := c.Get("a"); n != nil {
				n.SetRecvHeartBeatTime(time.Now().Unix())
			}
		}
	}()
	// the filter loop reclaiming stale peers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			c.Filter(func(n *cluster.Node) bool { return n.IsValid() })
		}
	}()

	wg.Wait()
}

// TestNode_NodesSumMismatchStreak_Concurrent covers the one piece of per-peer
// state this scheme adds. Peer goroutines in a heartbeat round each touch their
// own node, but the same node is also read by the filter loop and HTTP fan-out,
// so the counter has to go through the node mutex like every other runtime field.
func TestNode_NodesSumMismatchStreak_Concurrent(t *testing.T) {
	n := &cluster.Node{Node: protoNode("peer-1", "10.0.0.1", 2000)}
	const iters = 2000
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			n.BumpNodesSumMismatch()
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			n.ResetNodesSumMismatch()
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = n.IsCompleteRegister()
		}
	}()

	wg.Wait()
}

// TestGetAdvertisedNodes_MapAndSumAreOneSnapshot asserts the returned map is
// exactly what the returned sum was computed over. The heartbeat advertises the
// sum and later pushes the map; if they came from different snapshots a peer
// would merge a set that does not match the sum it was told, and the pair would
// take an extra reconcile round every time.
func TestGetAdvertisedNodes_MapAndSumAreOneSnapshot(t *testing.T) {
	c := newTestCluster("c1", "token")
	for i, name := range []string{"node-a", "node-b", "node-c"} {
		c.Add(completeNode(name, "10.0.0.1", int32(2000+i)))
	}

	nodes, sum := c.GetAdvertisedNodes()
	list := make([]*proto.Node, 0, len(nodes))
	for _, n := range nodes {
		list = append(list, n)
	}
	recomputed, count := cluster.ComputeNodesSum(list)
	if !bytes.Equal(recomputed, sum) {
		t.Errorf("sum does not match the returned map:\n got %x\nwant %x", recomputed, sum)
	}
	if count != len(nodes) {
		t.Errorf("count = %d, want %d", count, len(nodes))
	}
}
