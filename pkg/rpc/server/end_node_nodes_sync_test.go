package server

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// testNodeIdentity derives a stable host/port from a node name (FNV-1a). Every
// view of the cluster must describe a given node identically: the digest folds
// host and port, so handing the same node different addresses in different views
// would make the sums differ for reasons unrelated to membership.
func testNodeIdentity(name string) (string, int32) {
	var h uint32 = 2166136261
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 16777619
	}
	return fmt.Sprintf("10.%d.%d.%d", (h>>16)&0xff, (h>>8)&0xff, h&0xff), int32(2000 + h%1000)
}

func testProtoNode(name string) *proto.Node {
	host, port := testNodeIdentity(name)
	// The id is derived from the name too, so every view of a node agrees on it.
	// It is deliberately absent from ComputeNodesSum, so adding it here must not
	// change any digest assertion in this file — TestNodesSumIgnoresNodeID pins that.
	return &proto.Node{Name: name, Host: host, Port: port, NodeId: name + "-id"}
}

// findByName locates a directory entry the way an operator would, by the name
// they typed. Tests cannot use ClusterState.Get for this any more: the directory
// is keyed by identity, and an entry learned from a pre-2.8 peer (no id) is filed
// under a provisional address key rather than under either.
func findByName(t *testing.T, s *EndNodeServer, name string) *cluster.Node {
	t.Helper()
	n, _ := s.clusterState.(*cluster.EndNodeClusterManager).FindByName(name)
	return n
}

// newNodesSyncServer builds an EndNodeServer backed by a real cluster manager
// holding the local node plus `peers` fully-registered peers. It also initialises
// the package-level localNode global (mergeRemoteNodes reads its name/cluster).
func newNodesSyncServer(t *testing.T, selfName string, peers ...string) *EndNodeServer {
	t.Helper()

	selfHost, selfPort := testNodeIdentity(selfName)
	mgr, _, err := cluster.NewEndNodeClusterManagerFromConfig(
		cluster.ClusterInitConfig{ClusterToken: "cluster-token-abcdef01"},
		cluster.NodeInitConfig{Name: selfName, Host: selfHost, Port: selfPort, ID: selfName + "-id"},
	)
	if err != nil {
		t.Fatalf("init cluster manager: %v", err)
	}
	for _, name := range peers {
		n := &cluster.Node{Node: testProtoNode(name)}
		// Both directions of the registration done => IsCompleteRegister().
		n.SetRecvHeartBeatTime(time.Now().Unix())
		n.SetReportHeartBeatTime(time.Now().Unix())
		mgr.Add(n)
	}

	s := &EndNodeServer{}
	s.Name = selfName
	s.clusterState = mgr
	s.userMgr = usermanager.NewUserManager(nil, selfName)
	// defaultAppConfig turns this on; s.cfg is zero-valued here, so set it
	// explicitly to exercise the enabled path.
	s.cfg.Cluster.NodeSumSync = true
	return s
}

// heartbeatFrom builds a request as peer `from` would send it.
func heartbeatFrom(from string, nodesSum []byte, nodes map[string]*proto.Node) *proto.HeartBeatReq {
	return &proto.HeartBeatReq{
		NodeAuthInfo: &proto.NodeAuthInfo{Node: testProtoNode(from)},
		TimestampUs:  time.Now().UnixMicro(),
		NodesSum:     nodesSum,
		Nodes:        nodes,
	}
}

// TestHeartBeat_SteadyStateOmitsNodeDirectory is the core win: once two nodes
// agree on the digest, the response must stop carrying the node map. That map was
// previously sent on every tick, making the directory O(N) bytes per heartbeat
// and O(N^2) per node per round.
func TestHeartBeat_SteadyStateOmitsNodeDirectory(t *testing.T) {
	s := newNodesSyncServer(t, "self", "peer-1", "peer-2")
	_, serverSum := s.clusterState.GetAdvertisedNodes()

	rsp, err := s.HeartBeat(context.Background(), heartbeatFrom("peer-1", serverSum, nil))
	if err != nil {
		t.Fatalf("HeartBeat: %v", err)
	}
	if rsp.GetCode() != 0 {
		t.Fatalf("code = %d, msg = %q", rsp.GetCode(), rsp.GetMsg())
	}
	if len(rsp.GetNodesMap()) != 0 {
		t.Errorf("steady-state response still carries %d nodes; want none", len(rsp.GetNodesMap()))
	}
	if !bytes.Equal(rsp.GetNodesSum(), serverSum) {
		t.Errorf("nodes_sum = %x, want %x", rsp.GetNodesSum(), serverSum)
	}
}

// TestHeartBeat_LegacyPeerStillGetsFullDirectory: a peer that sends no digest
// cannot compare one, so it must keep receiving the full map or it would never
// learn about new nodes. The empty sum is the only capability signal.
func TestHeartBeat_LegacyPeerStillGetsFullDirectory(t *testing.T) {
	s := newNodesSyncServer(t, "self", "peer-1", "peer-2")
	advertised, serverSum := s.clusterState.GetAdvertisedNodes()

	rsp, err := s.HeartBeat(context.Background(), heartbeatFrom("peer-1", nil, nil))
	if err != nil {
		t.Fatalf("HeartBeat: %v", err)
	}
	if len(rsp.GetNodesMap()) != len(advertised) {
		t.Errorf("legacy peer got %d nodes, want the full set of %d",
			len(rsp.GetNodesMap()), len(advertised))
	}
	// The advertised set includes the responding node itself — without that, two
	// converged peers could never compute equal digests.
	if _, ok := rsp.GetNodesMap()["self"]; !ok {
		t.Errorf("advertised map omits the responding node itself: %v", rsp.GetNodesMap())
	}
	if !bytes.Equal(rsp.GetNodesSum(), serverSum) {
		t.Errorf("nodes_sum = %x, want %x", rsp.GetNodesSum(), serverSum)
	}
}

// TestHeartBeat_MismatchAloneDoesNotResendDirectory pins the division of labour:
// the server never acts on a digest mismatch, it only answers what it was asked.
// Reconciliation is client-driven, so there is exactly one mechanism to reason
// about and the server cannot amplify a disagreement into extra traffic.
func TestHeartBeat_MismatchAloneDoesNotResendDirectory(t *testing.T) {
	s := newNodesSyncServer(t, "self", "peer-1")

	rsp, err := s.HeartBeat(context.Background(),
		heartbeatFrom("peer-1", []byte("a-different-digest-entirely-32by"), nil))
	if err != nil {
		t.Fatalf("HeartBeat: %v", err)
	}
	if len(rsp.GetNodesMap()) != 0 {
		t.Errorf("server volunteered %d nodes on a bare mismatch; the client drives reconcile",
			len(rsp.GetNodesMap()))
	}
	if len(rsp.GetNodesSum()) == 0 {
		t.Error("response must always carry a digest")
	}
}

// TestHeartBeat_ReconcileMergesAndReturnsDirectory covers the reconcile round: the
// peer pushes its view, we absorb it AND answer with ours, so the single extra
// round trip converges both directions.
func TestHeartBeat_ReconcileMergesAndReturnsDirectory(t *testing.T) {
	s := newNodesSyncServer(t, "self", "peer-1")

	pushed := map[string]*proto.Node{
		"peer-9": {Name: "peer-9", Host: "10.0.0.9", Port: 2009},
	}
	if findByName(t, s, "peer-9") != nil {
		t.Fatal("precondition: peer-9 must be unknown before the reconcile")
	}

	rsp, err := s.HeartBeat(context.Background(),
		heartbeatFrom("peer-1", []byte("a-different-digest-entirely-32by"), pushed))
	if err != nil {
		t.Fatalf("HeartBeat: %v", err)
	}
	if findByName(t, s, "peer-9") == nil {
		t.Error("reconcile did not merge the pushed node")
	}
	if len(rsp.GetNodesMap()) == 0 {
		t.Error("reconcile response must carry our full directory back")
	}
	if _, ok := rsp.GetNodesMap()["self"]; !ok {
		t.Errorf("reconcile response omits the responding node itself: %v", rsp.GetNodesMap())
	}
}

// TestHeartBeat_ReconcileRejectsUntrustworthyNodes: the reconcile path lets a
// cluster member write into our directory through the request, which the response
// path never allowed. Structurally incomplete entries must be dropped.
//
// There is no longer a cluster-name check here: the sender has already proved
// possession of the cluster token to reach this handler, and it only advertises
// peers it completed bidirectional auth with, so everything reachable through
// this path is by construction in the same cluster.
func TestHeartBeat_ReconcileRejectsUntrustworthyNodes(t *testing.T) {
	s := newNodesSyncServer(t, "self", "peer-1")

	pushed := map[string]*proto.Node{
		"no-host":  {Name: "no-host", Port: 2007},
		"low-port": {Name: "low-port", Host: "10.0.0.7", Port: 80},
		"no-name":  {Host: "10.0.0.6", Port: 2006},
		"good":     {Name: "good", Host: "10.0.0.5", Port: 2005},
	}

	if _, err := s.HeartBeat(context.Background(),
		heartbeatFrom("peer-1", []byte("a-different-digest-entirely-32by"), pushed)); err != nil {
		t.Fatalf("HeartBeat: %v", err)
	}

	for _, name := range []string{"no-host", "low-port", "no-name"} {
		if findByName(t, s, name) != nil {
			t.Errorf("node %q should have been rejected but was merged", name)
		}
	}
	if findByName(t, s, "good") == nil {
		t.Error("a valid pushed node was rejected")
	}
}

// TestHeartBeat_ReconcileLeavesUserSyncUntouched: the reconcile call deliberately
// omits user_digests so a node-directory disagreement does not also double the
// per-user digest payload. The server must read that as "nothing to compare"
// rather than "this peer has no users" (which would request every user back).
func TestHeartBeat_ReconcileLeavesUserSyncUntouched(t *testing.T) {
	s := newNodesSyncServer(t, "self", "peer-1")
	s.userMgr.EnableClusterSync("default", newTestNGStore("default"))
	if !s.userMgr.IsClusterEnabled() {
		t.Fatal("precondition: cluster user sync must be enabled")
	}

	pushed := map[string]*proto.Node{
		"peer-9": {Name: "peer-9", Host: "10.0.0.9", Port: 2009},
	}
	rsp, err := s.HeartBeat(context.Background(),
		heartbeatFrom("peer-1", []byte("a-different-digest-entirely-32by"), pushed))
	if err != nil {
		t.Fatalf("HeartBeat: %v", err)
	}
	if n := len(rsp.GetNeedClusterUsers()); n != 0 {
		t.Errorf("reconcile heartbeat requested %d user payloads; an empty digest list means "+
			"'nothing to compare', not 'peer holds no users'", n)
	}
}

// TestHeartBeat_KillSwitchRestoresFullDirectory: with node_sum_sync off the
// server must answer every heartbeat with the full map again, even one that
// carries a matching digest. That is what makes the switch a genuine rollback
// rather than a half-disabled state, and it can be flipped per node because a
// peer that stops receiving a digest falls back to the legacy path anyway.
func TestHeartBeat_KillSwitchRestoresFullDirectory(t *testing.T) {
	s := newNodesSyncServer(t, "self", "peer-1", "peer-2")
	s.cfg.Cluster.NodeSumSync = false

	advertised, serverSum := s.clusterState.GetAdvertisedNodes()
	rsp, err := s.HeartBeat(context.Background(), heartbeatFrom("peer-1", serverSum, nil))
	if err != nil {
		t.Fatalf("HeartBeat: %v", err)
	}
	if len(rsp.GetNodesMap()) != len(advertised) {
		t.Errorf("kill switch on, got %d nodes, want the full set of %d",
			len(rsp.GetNodesMap()), len(advertised))
	}
	// The digest must be withheld too, otherwise peers keep comparing against us
	// and firing reconcile heartbeats at a node whose whole point is to be back on
	// the old behaviour.
	if len(rsp.GetNodesSum()) != 0 {
		t.Errorf("kill switch on but response still advertises a digest %x", rsp.GetNodesSum())
	}
}

// TestReconcileBackoff_DampsPermanentDisagreement pins the damping arithmetic:
// a peer whose digest never matches must not cost a reconcile every round
// forever. Rounds 1..limit still reconcile (a real disagreement usually clears
// immediately), after which only every nodesReconcileBackoffRounds-th round does.
func TestReconcileBackoff_DampsPermanentDisagreement(t *testing.T) {
	n := &cluster.Node{Node: testProtoNode("peer-1")}

	attempts := 0
	const rounds = 30
	for i := 0; i < rounds; i++ {
		streak := n.BumpNodesSumMismatch()
		if streak > nodesReconcileStreakLimit && streak%nodesReconcileBackoffRounds != 0 {
			continue
		}
		attempts++
	}

	// Without damping this would be one attempt per round.
	if attempts >= rounds {
		t.Fatalf("no damping: %d attempts over %d rounds", attempts, rounds)
	}
	// Streaks 1..3 always attempt, then only multiples of 6 (6,12,18,24,30).
	want := nodesReconcileStreakLimit + rounds/nodesReconcileBackoffRounds
	if attempts != want {
		t.Errorf("attempts = %d, want %d over %d rounds", attempts, want, rounds)
	}

	// A round that agrees clears the streak, so the next real disagreement gets
	// the responsive path again instead of inheriting the backoff.
	n.ResetNodesSumMismatch()
	if streak := n.BumpNodesSumMismatch(); streak != 1 {
		t.Errorf("streak after reset = %d, want 1", streak)
	}
}

// TestHeartBeat_ConvergedPeersAgree is the end-to-end invariant the whole scheme
// rests on: two nodes holding the same cluster view compute the same digest, so
// steady state never exchanges the directory. It fails if the advertised set ever
// stops including the local node, or stops being symmetric across a pair.
func TestHeartBeat_ConvergedPeersAgree(t *testing.T) {
	names := []string{"node-a", "node-b", "node-c"}

	// Build each node's view of the same three-node cluster and collect the digest
	// its heartbeat response would advertise.
	sums := make([][]byte, 0, len(names))
	for _, self := range names {
		peers := make([]string, 0, len(names)-1)
		for _, n := range names {
			if n != self {
				peers = append(peers, n)
			}
		}
		s := newNodesSyncServer(t, self, peers...)
		// Probe as a known peer: HeartBeat rejects requests from nodes it has
		// already dropped, and legacy framing (no digest) makes it return the
		// full advertised set so the assertion below can size it.
		rsp, err := s.HeartBeat(context.Background(), heartbeatFrom(peers[0], nil, nil))
		if err != nil {
			t.Fatalf("HeartBeat on %s: %v", self, err)
		}
		if len(rsp.GetNodesMap()) != len(names) {
			t.Fatalf("%s advertises %d nodes, want %d", self, len(rsp.GetNodesMap()), len(names))
		}
		sums = append(sums, rsp.GetNodesSum())
	}

	for i := 1; i < len(sums); i++ {
		if !bytes.Equal(sums[i], sums[0]) {
			t.Fatalf("converged peers disagree on the digest: %s=%x %s=%x",
				names[0], sums[0], names[i], sums[i])
		}
	}
}
