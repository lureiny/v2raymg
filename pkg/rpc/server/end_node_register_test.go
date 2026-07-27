package server

import (
	"context"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// RegisterNode had no unit coverage at all before this change, while carrying
// every membership decision in the cluster. These tests pin the branch table.
//
// The governing rule: the registering peer is the authority on its own name,
// address and identity, so a disagreement with what we hold is applied rather
// than refused. Refusal was the pre-2.8 behaviour (code 105 on any mismatch of
// the host+port+name triple) and is structurally unfixable — the party that
// would have to withdraw a stale claim is the instance that no longer exists.
// Only two refusals remain, both unresolvable config errors that would otherwise
// silently corrupt addressing.

const testClusterToken = "cluster-token-abcdef01"

func registerReq(token string, n *proto.Node) *proto.RegisterNodeReq {
	return &proto.RegisterNodeReq{
		NodeAuthInfo: &proto.NodeAuthInfo{Token: token, Node: n},
	}
}

func mustRegister(t *testing.T, s *EndNodeServer, n *proto.Node) *proto.RegisterNodeRsp {
	t.Helper()
	rsp, err := s.RegisterNode(context.Background(), registerReq(testClusterToken, n))
	if err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	return rsp
}

func TestRegisterNode_RejectsStructurallyInvalidClaims(t *testing.T) {
	tests := []struct {
		name string
		node *proto.Node
	}{
		{"empty host", &proto.Node{Name: "peer", Port: 5000, NodeId: "peer-id"}},
		{"zero port", &proto.Node{Name: "peer", Host: "10.0.0.1", NodeId: "peer-id"}},
		{"negative port", &proto.Node{Name: "peer", Host: "10.0.0.1", Port: -1, NodeId: "peer-id"}},
		// A nameless peer used to be accepted and filed under the empty string.
		{"empty name", &proto.Node{Host: "10.0.0.1", Port: 5000, NodeId: "peer-id"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newNodesSyncServer(t, "self")
			rsp := mustRegister(t, s, tc.node)
			if rsp.GetCode() != 100 {
				t.Errorf("code = %d, want 100", rsp.GetCode())
			}
			if len(rsp.GetData()) != 0 {
				t.Error("a rejected registration must not hand out a token")
			}
		})
	}
}

// TestRegisterNode_RejectsOurOwnIdentity is the clone guard. Identity lives in
// the database, so copying a data directory — rather than a config file — to
// stand up a second node duplicates it, and two live nodes would then contend
// for one directory slot. Nothing can repair that automatically, so it is
// refused and reported.
func TestRegisterNode_RejectsOurOwnIdentity(t *testing.T) {
	s := newNodesSyncServer(t, "self")

	rsp := mustRegister(t, s, &proto.Node{
		Name: "impostor", Host: "10.0.0.1", Port: 5000, NodeId: localNode.Node.GetNodeId(),
	})
	if rsp.GetCode() != 103 {
		t.Errorf("code = %d, want 103", rsp.GetCode())
	}
}

func TestRegisterNode_RejectsOurOwnName(t *testing.T) {
	s := newNodesSyncServer(t, "self")

	rsp := mustRegister(t, s, &proto.Node{Name: "self", Host: "10.0.0.9", Port: 5000, NodeId: "other-id"})
	if rsp.GetCode() != 103 {
		t.Errorf("code = %d, want 103", rsp.GetCode())
	}
}

func TestRegisterNode_WrongClusterTokenIsBlacklisted(t *testing.T) {
	s := newNodesSyncServer(t, "self")

	rsp, err := s.RegisterNode(context.Background(),
		registerReq("not-the-cluster-token", claimNode("stranger", "stranger-id", "10.0.0.1", 5000)))
	if err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	if rsp.GetCode() != 101 {
		t.Fatalf("code = %d, want 101", rsp.GetCode())
	}
	if s.clusterState.GetNodeFromWrongNodeList("stranger") == nil {
		t.Error("a peer presenting the wrong cluster token must be blacklisted")
	}
	if findByName(t, s, "stranger") != nil {
		t.Error("a peer presenting the wrong cluster token must not enter the directory")
	}
}

func claimNode(name, id, host string, port int32) *proto.Node {
	return &proto.Node{Name: name, Host: host, Port: port, NodeId: id}
}

func TestRegisterNode_NewPeerGetsTokenAndEntry(t *testing.T) {
	s := newNodesSyncServer(t, "self")

	rsp := mustRegister(t, s, claimNode("peer", "peer-id", "10.0.0.1", 5000))
	if rsp.GetCode() != 0 {
		t.Fatalf("code = %d, msg = %q", rsp.GetCode(), rsp.GetMsg())
	}
	if len(rsp.GetData()) == 0 {
		t.Error("a successful registration must return a token")
	}
	n := s.clusterState.Get("peer-id")
	if n == nil {
		t.Fatal("peer was not filed under its identity")
	}
	if n.GetInToken() != string(rsp.GetData()) {
		t.Error("the token handed out is not the one recorded for the peer")
	}
}

// TestRegisterNode_AnswersWithItsOwnIdentity: nothing in any response used to say
// who produced it, so a caller could not tell that the address it holds for a
// peer had been taken over. This is what assertResponder consumes.
func TestRegisterNode_AnswersWithItsOwnIdentity(t *testing.T) {
	s := newNodesSyncServer(t, "self")

	rsp := mustRegister(t, s, claimNode("peer", "peer-id", "10.0.0.1", 5000))
	if rsp.GetResponderNodeId() != localNode.Node.GetNodeId() {
		t.Errorf("responder node id = %q, want %q", rsp.GetResponderNodeId(), localNode.Node.GetNodeId())
	}
	if rsp.GetResponderName() != "self" {
		t.Errorf("responder name = %q, want self", rsp.GetResponderName())
	}
}

// TestRegisterNode_RepeatIsIdempotent preserves the historical 102 contract, which
// pre-2.8 peers rely on: the response carries the ORIGINAL token in Data, and the
// caller adopts it. Minting a new one would invalidate a working session.
func TestRegisterNode_RepeatIsIdempotent(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	first := mustRegister(t, s, claimNode("peer", "peer-id", "10.0.0.1", 5000))

	second := mustRegister(t, s, claimNode("peer", "peer-id", "10.0.0.1", 5000))
	if second.GetCode() != 102 {
		t.Errorf("code = %d, want 102", second.GetCode())
	}
	if string(second.GetData()) != string(first.GetData()) {
		t.Error("a repeated registration rotated the token; the peer's session would break")
	}
}

// TestRegisterNode_AddressChangeIsAccepted is the case the redesign exists for.
// Pre-2.8 this returned 105 and the node could not rejoin until its stale entry
// aged out — up to NodeTimeOut plus a filter tick, and never at all while the old
// address stayed reachable.
func TestRegisterNode_AddressChangeIsAccepted(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	mustRegister(t, s, claimNode("peer", "peer-id", "10.0.0.1", 5000))

	rsp := mustRegister(t, s, claimNode("peer", "peer-id", "10.9.9.9", 6000))
	if rsp.GetCode() != 0 {
		t.Fatalf("code = %d, msg = %q; an address change must be accepted", rsp.GetCode(), rsp.GetMsg())
	}
	n := s.clusterState.Get("peer-id")
	if n == nil {
		t.Fatal("peer left its identity key")
	}
	if n.GetHost() != "10.9.9.9" || n.GetPort() != 6000 {
		t.Errorf("address = %s:%d, want 10.9.9.9:6000", n.GetHost(), n.GetPort())
	}
	if count := countPeers(s); count != 1 {
		t.Errorf("directory holds %d peers, want 1: a move must update, not duplicate", count)
	}
}

func TestRegisterNode_RenameIsAccepted(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	mustRegister(t, s, claimNode("before", "peer-id", "10.0.0.1", 5000))

	rsp := mustRegister(t, s, claimNode("after", "peer-id", "10.0.0.1", 5000))
	if rsp.GetCode() != 0 {
		t.Fatalf("code = %d, msg = %q", rsp.GetCode(), rsp.GetMsg())
	}
	if findByName(t, s, "before") != nil {
		t.Error("the old name still resolves; the rename left a duplicate")
	}
	if findByName(t, s, "after") == nil {
		t.Error("the new name does not resolve")
	}
}

// TestRegisterNode_RebuiltNodeTakesOverAndTombstonesOldIdentity covers the one
// blind spot identity keying alone leaves: the database is deleted but the config
// is not, so the same name and address come back under a new id. Accepting it is
// required (the old instance is gone and can never withdraw its claim), and
// tombstoning the old id is what stops peers who have not noticed from gossiping
// it straight back.
func TestRegisterNode_RebuiltNodeTakesOverAndTombstonesOldIdentity(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	mustRegister(t, s, claimNode("peer", "old-id", "10.0.0.1", 5000))

	rsp := mustRegister(t, s, claimNode("peer", "new-id", "10.0.0.1", 5000))
	if rsp.GetCode() != 0 {
		t.Fatalf("code = %d, msg = %q", rsp.GetCode(), rsp.GetMsg())
	}
	if s.clusterState.Get("new-id") == nil {
		t.Error("the rebuilt node was not filed under its new identity")
	}
	if s.clusterState.Get("old-id") != nil {
		t.Error("the superseded entry survived")
	}
	if !s.clusterState.IsDirty("old-id", time.Now().Unix()) {
		t.Error("the superseded identity was not tombstoned; gossip would reinstate it")
	}
	if count := countPeers(s); count != 1 {
		t.Errorf("directory holds %d peers, want 1", count)
	}
}

// countPeers counts directory entries other than this node's own.
func countPeers(s *EndNodeServer) int {
	n := 0
	for _, node := range s.clusterState.GetAllNode() {
		if !node.IsSelf() {
			n++
		}
	}
	return n
}

// --- gossip merge ---

// TestMergeRemoteNodes_RejectsKeyBodyMismatch closes a latent hole. The merge
// used to look an entry up by the wire map's KEY while inserting it under
// node.Name, so a map whose key disagreed with its body slipped past the
// "already known?" guard and then overwrote an unrelated entry wholesale —
// tokens, heartbeat timestamps and address included.
func TestMergeRemoteNodes_RejectsKeyBodyMismatch(t *testing.T) {
	s := newNodesSyncServer(t, "self", "victim")
	victim := findByName(t, s, "victim")
	if victim == nil {
		t.Fatal("precondition: victim must be known")
	}
	victim.SetInToken("victim-session-token")
	victimHost := victim.GetHost()

	mergeRemoteNodes(map[string]*proto.Node{
		"unrelated-key": {Name: "victim", Host: "10.6.6.6", Port: 6666, NodeId: "victim-id"},
	}, s, "test")

	got := findByName(t, s, "victim")
	if got == nil {
		t.Fatal("the victim entry was removed")
	}
	if got.GetHost() != victimHost {
		t.Errorf("victim address was rewritten to %q by a mismatched gossip entry", got.GetHost())
	}
	if got.GetInToken() != "victim-session-token" {
		t.Error("victim session token was destroyed by a mismatched gossip entry")
	}
}

// TestMergeRemoteNodes_HonoursTombstones: a peer that has not noticed a takeover
// keeps advertising the superseded identity. Without the tombstone the add-only
// merge would reinsert it with a fresh CreateTime every round, renewing it
// indefinitely — which is why the entry is not simply reclaimed on the spot.
func TestMergeRemoteNodes_HonoursTombstones(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	s.clusterState.MarkDirty("gone-id", time.Now().Unix())

	mergeRemoteNodes(map[string]*proto.Node{
		"gone": {Name: "gone", Host: "10.0.0.9", Port: 5000, NodeId: "gone-id"},
	}, s, "test")

	if s.clusterState.Get("gone-id") != nil {
		t.Error("a tombstoned identity was reintroduced by gossip")
	}
}

// TestMergeRemoteNodes_IsAddOnly: gossip says who exists, not where they live.
// Only a node itself, over an authenticated connection, may change its own
// address — otherwise a third party repeating a stale view could redirect it.
func TestMergeRemoteNodes_IsAddOnly(t *testing.T) {
	s := newNodesSyncServer(t, "self", "peer-1")
	before := findByName(t, s, "peer-1")
	if before == nil {
		t.Fatal("precondition: peer-1 must be known")
	}
	beforeHost := before.GetHost()

	mergeRemoteNodes(map[string]*proto.Node{
		"peer-1": {Name: "peer-1", Host: "10.7.7.7", Port: 7777, NodeId: "peer-1-id"},
	}, s, "test")

	if got := findByName(t, s, "peer-1"); got.GetHost() != beforeHost {
		t.Errorf("gossip rewrote a known peer's address to %q", got.GetHost())
	}
}

// TestMergeRemoteNodes_NeverLearnsSelf: our own entry is a local fact, not a
// claim from the network, and adopting someone else's view of us would replace
// the sentinel timestamps that keep it permanently valid.
func TestMergeRemoteNodes_NeverLearnsSelf(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	selfID := localNode.Node.GetNodeId()

	mergeRemoteNodes(map[string]*proto.Node{
		"self": {Name: "self", Host: "10.5.5.5", Port: 5555, NodeId: selfID},
	}, s, "test")

	if count := countPeers(s); count != 0 {
		t.Errorf("merged %d peers from a map describing only ourselves", count)
	}
}

// --- eviction ---

// TestRegisterNode_DoesNotEvictStaticSeedsOnRejection: static peers are the only
// bootstrap path once the center node is gone, so a rejection must never remove
// one — that would leave a misconfigured node permanently unable to rejoin.
func TestRegisterNode_DoesNotEvictStaticSeedsOnRejection(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	if err := s.clusterState.(*cluster.EndNodeClusterManager).LoadStaticNode(
		[]cluster.StaticNode{{Name: "seed", Host: "10.4.4.4", Port: 4444}}); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	if s.clusterState.(*cluster.EndNodeClusterManager).FindByAddr("10.4.4.4", 4444) == nil {
		t.Fatal("precondition: seed must be loaded")
	}

	// A wrong-token registration from an unrelated peer must not disturb it.
	if _, err := s.RegisterNode(context.Background(),
		registerReq("wrong", claimNode("stranger", "stranger-id", "10.3.3.3", 3333))); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}

	if s.clusterState.(*cluster.EndNodeClusterManager).FindByAddr("10.4.4.4", 4444) == nil {
		t.Error("the static seed was evicted")
	}
}

// --- outbound identity handling ---

// TestDestBinding_OmitsUnconfirmedSeedName: the anti-replay destination binding
// must only assert what we actually know. A static seed's name was typed into a
// config file and confirmed by nobody, so binding a request to it made the far
// side's checkReplay reject the call outright whenever the label was wrong — a
// single mistyped seed name was permanently unusable outbound, and the ghost
// entry it left behind could only ever be cleaned up if that peer happened to
// register to us first.
func TestDestBinding_OmitsUnconfirmedSeedName(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	mgr := s.clusterState.(*cluster.EndNodeClusterManager)
	if err := mgr.LoadStaticNode([]cluster.StaticNode{{Name: "typo", Host: "10.4.4.4", Port: 4444}}); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	seed := mgr.FindByAddr("10.4.4.4", 4444)
	if seed == nil {
		t.Fatal("precondition: seed must be loaded")
	}

	if got := destBinding(seed); got != nil {
		t.Errorf("destBinding = %+v, want nil for an unconfirmed static seed", got)
	}

	// Once the peer has identified itself the name is its own claim, so it is
	// asserted normally again.
	if _, ok := mgr.AdoptIdentity("10.4.4.4", 4444, "real-id", "real-name"); !ok {
		t.Fatal("AdoptIdentity did not resolve the seed")
	}
	resolved := mgr.FindByAddr("10.4.4.4", 4444)
	got := destBinding(resolved)
	if got == nil {
		t.Fatal("destBinding = nil for a confirmed peer; the binding must be asserted")
	}
	if got.GetName() != "real-name" || got.GetNodeId() != "real-id" {
		t.Errorf("destBinding = %s/%s, want real-name/real-id", got.GetName(), got.GetNodeId())
	}
}

func TestDestBinding_AssertsRegisteredPeers(t *testing.T) {
	s := newNodesSyncServer(t, "self", "peer-1")
	peer := findByName(t, s, "peer-1")
	if peer == nil {
		t.Fatal("precondition: peer-1 must be known")
	}
	if got := destBinding(peer); got == nil || got.GetName() != "peer-1" {
		t.Errorf("destBinding = %+v, want the peer's own self-reported identity", got)
	}
}

// TestAssertResponder_AdoptsProvisionalIdentity: a seed that answers tells us who
// it is. Adopting it immediately is what stops the same peer being learned a
// second time when it later registers to us.
func TestAssertResponder_AdoptsProvisionalIdentity(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	mgr := s.clusterState.(*cluster.EndNodeClusterManager)
	if err := mgr.LoadStaticNode([]cluster.StaticNode{{Name: "typo", Host: "10.4.4.4", Port: 4444}}); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	seed := mgr.FindByAddr("10.4.4.4", 4444)

	adopted, ok := s.assertResponder(seed, "real-id", "real-name")
	if !ok {
		t.Fatal("assertResponder rejected a first identification")
	}
	n := mgr.Get("real-id")
	if n == nil {
		t.Fatalf("seed was not re-filed under the identity it reported")
	}
	// The caller must be handed the entry that is actually in the directory now.
	// Adoption REPLACES the entry, so a caller that kept using the pointer it
	// started the round with would write the session it just negotiated into an
	// object nobody can reach — losing it, and costing an extra round on every
	// static seed at startup.
	if adopted != n {
		t.Error("assertResponder returned the superseded pointer rather than the adopted entry")
	}
	if n.GetName() != "real-name" {
		t.Errorf("name = %q, want the peer's own name over the mistyped config label", n.GetName())
	}
	if n.IsCompleteRegister() {
		t.Error("adopting an identity must not stamp heartbeat timestamps; the peer would " +
			"be advertised as authenticated without a handshake")
	}
}

// TestAssertResponder_TombstonesTakenOverAddress is the outbound half of the
// takeover detection. Without it, a peer holding a stale address keeps
// registering to whatever now answers there — succeeding every time, since that
// node accepts any registration — which keeps reportHeartBeatTime fresh, which
// keeps IsValid true, which means the stale entry is never reclaimed. That is the
// one shape of the problem that does not self-heal.
func TestAssertResponder_TombstonesTakenOverAddress(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	mustRegister(t, s, claimNode("peer", "old-id", "10.0.0.1", 5000))
	entry := s.clusterState.Get("old-id")
	if entry == nil {
		t.Fatal("precondition: peer must be registered")
	}

	if _, ok := s.assertResponder(entry, "someone-else-id", "someone-else"); ok {
		t.Error("assertResponder accepted a response from a different identity")
	}
	if s.clusterState.Get("old-id") != nil {
		t.Error("the stale entry survived; it would keep being dialled and kept alive")
	}
	if !s.clusterState.IsDirty("old-id", time.Now().Unix()) {
		t.Error("the stale identity was not tombstoned; gossip would restore it")
	}
}

// TestAssertResponder_ToleratesLegacyPeers: a pre-2.8 peer reports no identity,
// which must read as "no information", not as a mismatch.
func TestAssertResponder_ToleratesLegacyPeers(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	mustRegister(t, s, claimNode("peer", "peer-id", "10.0.0.1", 5000))
	entry := s.clusterState.Get("peer-id")

	if _, ok := s.assertResponder(entry, "", ""); !ok {
		t.Error("a peer that reports no identity was treated as a mismatch")
	}
	if s.clusterState.Get("peer-id") == nil {
		t.Error("a legacy response evicted a healthy entry")
	}
}

// TestAssertResponder_AcceptsMatchingIdentity is the steady state: the common
// path must be free of side effects.
func TestAssertResponder_AcceptsMatchingIdentity(t *testing.T) {
	s := newNodesSyncServer(t, "self")
	mustRegister(t, s, claimNode("peer", "peer-id", "10.0.0.1", 5000))
	entry := s.clusterState.Get("peer-id")

	if _, ok := s.assertResponder(entry, "peer-id", "peer"); !ok {
		t.Error("assertResponder rejected the identity it expected")
	}
	if s.clusterState.Get("peer-id") == nil {
		t.Error("the entry was disturbed on the happy path")
	}
	if s.clusterState.IsDirty("peer-id", time.Now().Unix()) {
		t.Error("a matching identity was tombstoned")
	}
}
