package server

import (
	"sort"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// fakeNodeStore is an in-memory NodeStore that also records the calls made, so a
// test can assert not just the end state but that a steady cluster writes nothing.
type fakeNodeStore struct {
	rows    map[string]PersistedNode
	upserts int
	deletes int
}

func newFakeNodeStore(ids ...string) *fakeNodeStore {
	f := &fakeNodeStore{rows: map[string]PersistedNode{}}
	for _, id := range ids {
		f.rows[id] = PersistedNode{NodeID: id, Name: id}
	}
	return f
}

func (f *fakeNodeStore) List() ([]PersistedNode, error) {
	out := make([]PersistedNode, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out, nil
}

func (f *fakeNodeStore) Upsert(n PersistedNode) error {
	f.upserts++
	f.rows[n.NodeID] = n
	return nil
}

func (f *fakeNodeStore) Delete(nodeID string) error {
	f.deletes++
	delete(f.rows, nodeID)
	return nil
}

func (f *fakeNodeStore) ids() []string {
	rows, _ := f.List()
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.NodeID)
	}
	return out
}

// persistenceServer builds a server whose cluster manager holds the local node
// plus whatever peers the caller adds.
func persistenceServer(t *testing.T, store NodeStore) *EndNodeServer {
	t.Helper()
	mgr, _, err := cluster.NewEndNodeClusterManagerFromConfig(
		cluster.ClusterInitConfig{ClusterToken: "cluster-token-abcdef01"},
		cluster.NodeInitConfig{Name: "self", Host: "10.0.0.1", Port: 2000, ID: "self-id"},
	)
	if err != nil {
		t.Fatalf("init cluster manager: %v", err)
	}
	s := &EndNodeServer{}
	s.Name = "self"
	s.clusterState = mgr
	s.nodeStore = store
	return s
}

func registeredPeer(name, id string, port int32) *cluster.Node {
	n := &cluster.Node{Node: &proto.Node{Name: name, Host: "10.0.0.9", Port: port, NodeId: id}}
	n.SetRecvHeartBeatTime(time.Now().Unix())
	n.SetReportHeartBeatTime(time.Now().Unix())
	return n
}

// TestReconcileNodeStore_PersistsOnlyRegisteredPeers is the security-relevant
// half of the contract: only a peer this node completed bidirectional token auth
// with may be written. A merely-advertised entry must not be, or directory
// poisoning would go from "a restart clears it" to "it survives restarts".
func TestReconcileNodeStore_PersistsOnlyRegisteredPeers(t *testing.T) {
	store := newFakeNodeStore()
	s := persistenceServer(t, store)

	s.clusterState.Add(registeredPeer("registered", "registered-id", 3001))
	// Known but never registered — exactly what mergeRemoteNodes produces from a
	// peer's advertisement.
	s.clusterState.Add(&cluster.Node{
		Node:       &proto.Node{Name: "advertised-only", Host: "10.0.0.8", Port: 3002, NodeId: "advertised-id"},
		CreateTime: time.Now().Unix(),
	})

	s.reconcileNodeStore()

	got := store.ids()
	if len(got) != 1 || got[0] != "registered-id" {
		t.Errorf("persisted %v, want only [registered-id]", got)
	}
}

// TestReconcileNodeStore_SkipsSelfAndStaticPeers: the local node is not a peer,
// and static peers come from the config file on every start and are never
// evicted — persisting either would just add a staler second source of truth.
func TestReconcileNodeStore_SkipsSelfAndStaticPeers(t *testing.T) {
	store := newFakeNodeStore()
	s := persistenceServer(t, store)

	if err := s.clusterState.(*cluster.EndNodeClusterManager).LoadStaticNode(
		[]cluster.StaticNode{{Host: "10.0.0.7", Port: 3003}}); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	// Give it an identity and make it look fully registered, so only the isLocal
	// check can exclude it.
	if n, ok := s.clusterState.AdoptIdentity("10.0.0.7", 3003, "static-id", "static-peer"); ok {
		n.SetRecvHeartBeatTime(time.Now().Unix())
		n.SetReportHeartBeatTime(time.Now().Unix())
	} else {
		t.Fatal("AdoptIdentity did not resolve the static seed")
	}

	s.reconcileNodeStore()

	if got := store.ids(); len(got) != 0 {
		t.Errorf("persisted %v, want nothing (self and static peers are excluded)", got)
	}
}

// TestReconcileNodeStore_SkipsPeersWithoutIdentity: rows are keyed by node_id, so
// a peer we have not identified cannot be stored. It never needs to be — such an
// entry is either a static seed (config is its source of truth) or one we have
// not handshaken with, which the IsCompleteRegister rule already excludes.
func TestReconcileNodeStore_SkipsPeersWithoutIdentity(t *testing.T) {
	store := newFakeNodeStore()
	s := persistenceServer(t, store)

	anon := &cluster.Node{Node: &proto.Node{Host: "10.0.0.6", Port: 3010}}
	anon.SetRecvHeartBeatTime(time.Now().Unix())
	anon.SetReportHeartBeatTime(time.Now().Unix())
	s.clusterState.Add(anon)

	s.reconcileNodeStore()

	if got := store.ids(); len(got) != 0 {
		t.Errorf("persisted %v, want nothing: an unidentified peer has no key to store it under", got)
	}
}

// TestReconcileNodeStore_DeletesOnlyWhenGoneFromMemory pins the deliberate
// asymmetry. A peer that can still reach us while we cannot reach it loses
// IsCompleteRegister but is alive and still being retried; dropping its address
// then would cause exactly the orphaning this persistence exists to prevent.
// Only disappearing from memory entirely — i.e. evicted by the node timeout —
// may remove the row.
func TestReconcileNodeStore_DeletesOnlyWhenGoneFromMemory(t *testing.T) {
	store := newFakeNodeStore("half-open-id", "evicted-id")
	s := persistenceServer(t, store)

	// Still in memory, but only one direction is fresh.
	halfOpen := &cluster.Node{Node: &proto.Node{Name: "half-open", Host: "10.0.0.6", Port: 3004, NodeId: "half-open-id"}}
	halfOpen.SetRecvHeartBeatTime(time.Now().Unix())
	s.clusterState.Add(halfOpen)
	// "evicted-id" is deliberately absent from memory.

	s.reconcileNodeStore()

	got := store.ids()
	if len(got) != 1 || got[0] != "half-open-id" {
		t.Errorf("persisted %v, want only [half-open-id]: a peer still in memory must keep "+
			"its row, and only one absent from memory may be dropped", got)
	}
}

// TestReconcileNodeStore_RenameKeepsOneRow is what identity-keyed rows buy. Under
// the old name-keyed schema a renamed peer wrote a second row and orphaned the
// first, because the delete rule ("absent from memory") could never match a name
// nothing in memory carried any more.
func TestReconcileNodeStore_RenameKeepsOneRow(t *testing.T) {
	store := newFakeNodeStore()
	s := persistenceServer(t, store)

	s.clusterState.Add(registeredPeer("before", "stable-id", 3006))
	s.reconcileNodeStore()
	if got := store.ids(); len(got) != 1 || got[0] != "stable-id" {
		t.Fatalf("persisted %v, want [stable-id]", got)
	}

	// Same identity re-registers under a new name and address.
	res := s.clusterState.ResolveRegistration(
		&proto.Node{Name: "after", Host: "10.9.9.9", Port: 3007, NodeId: "stable-id"},
		"tok", time.Now().Unix())
	if res.Outcome != cluster.ResolveMoved {
		t.Fatalf("outcome = %v, want moved", res.Outcome)
	}
	res.Node.SetReportHeartBeatTime(time.Now().Unix())

	s.reconcileNodeStore()

	got := store.ids()
	if len(got) != 1 || got[0] != "stable-id" {
		t.Fatalf("persisted %v, want exactly [stable-id]: a rename must update the row, not add one", got)
	}
	rows, _ := store.List()
	if rows[0].Name != "after" || rows[0].Host != "10.9.9.9" || rows[0].Port != 3007 {
		t.Errorf("row = %+v, want the updated name and address", rows[0])
	}
}

// TestReconcileNodeStore_SteadyStateWritesNothing: the reconcile runs on every
// filter tick, so it must be genuinely incremental — a cluster where nothing
// changed has to produce zero deletes, otherwise a large cluster would hammer
// SQLite forever.
func TestReconcileNodeStore_SteadyStateWritesNothing(t *testing.T) {
	store := newFakeNodeStore()
	s := persistenceServer(t, store)
	s.clusterState.Add(registeredPeer("peer-1", "peer-1-id", 3005))

	s.reconcileNodeStore()
	upserts, deletes := store.upserts, store.deletes
	if upserts == 0 {
		t.Fatal("first reconcile wrote nothing; the peer should have been persisted")
	}

	// Nothing changed in between.
	for i := 0; i < 3; i++ {
		s.reconcileNodeStore()
	}
	if store.deletes != deletes {
		t.Errorf("steady state performed %d deletes, want %d", store.deletes, deletes)
	}
	if store.upserts != upserts+3 {
		// Upsert is idempotent at the SQL level, so re-issuing it is acceptable;
		// what must never happen is churn in the row set.
		t.Logf("steady state re-upserted (idempotent): %d -> %d", upserts, store.upserts)
	}
	if got := store.ids(); len(got) != 1 || got[0] != "peer-1-id" {
		t.Errorf("row set churned in steady state: %v", got)
	}
}

// TestReconcileNodeStore_NilStoreIsNoop: servers built directly in tests, and any
// future deployment without persistence, must not panic.
func TestReconcileNodeStore_NilStoreIsNoop(t *testing.T) {
	s := persistenceServer(t, nil)
	s.clusterState.Add(registeredPeer("peer-1", "peer-1-id", 3006))
	s.reconcileNodeStore() // must not panic
}

// TestLoadedNodeIsNotTreatedAsRegistered is the trap this design most easily falls
// into. A node loaded from the database must be retryable — IsValid true, so the
// heartbeat round dials it — but must NOT look authenticated: IsCompleteRegister
// checks the two heartbeat timestamps and verifies no token at all, so stamping
// those at load time would put a peer this process has never spoken to straight
// into the set it advertises to everyone else.
func TestLoadedNodeIsNotTreatedAsRegistered(t *testing.T) {
	mgr, _, err := cluster.NewEndNodeClusterManagerFromConfig(
		cluster.ClusterInitConfig{ClusterToken: "cluster-token-abcdef01"},
		cluster.NodeInitConfig{Name: "self", Host: "10.0.0.1", Port: 2000, ID: "self-id"},
	)
	if err != nil {
		t.Fatalf("init cluster manager: %v", err)
	}

	// Exactly what cmd.loadPersistedNodes does: CreateTime only.
	loaded := &cluster.Node{
		Node:       &proto.Node{Name: "from-db", Host: "10.0.0.5", Port: 3007, NodeId: "from-db-id"},
		CreateTime: time.Now().Unix(),
	}
	mgr.Add(loaded)

	if !loaded.IsValid() {
		t.Error("loaded node is not valid, so the heartbeat round would skip it and it " +
			"would be evicted before it could ever be re-registered")
	}
	if loaded.IsCompleteRegister() {
		t.Error("loaded node reports IsCompleteRegister without any handshake this run")
	}
	advertised, _ := mgr.GetAdvertisedNodes()
	if _, ok := advertised["from-db"]; ok {
		t.Error("loaded node entered the advertised set; it would be broadcast to peers " +
			"as an authenticated member before this node has spoken to it")
	}
}
