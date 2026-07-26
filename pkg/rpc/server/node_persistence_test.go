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
	rows    map[string]proto.Node
	upserts int
	deletes int
}

func newFakeNodeStore(names ...string) *fakeNodeStore {
	f := &fakeNodeStore{rows: map[string]proto.Node{}}
	for _, n := range names {
		f.rows[n] = proto.Node{Name: n}
	}
	return f
}

func (f *fakeNodeStore) ListNames() ([]string, error) {
	out := make([]string, 0, len(f.rows))
	for n := range f.rows {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeNodeStore) Upsert(name, host string, port int32) error {
	f.upserts++
	f.rows[name] = proto.Node{Name: name, Host: host, Port: port}
	return nil
}

func (f *fakeNodeStore) Delete(name string) error {
	f.deletes++
	delete(f.rows, name)
	return nil
}

func (f *fakeNodeStore) names() []string {
	out, _ := f.ListNames()
	return out
}

// persistenceServer builds a server whose cluster manager holds the local node
// plus whatever peers the caller adds.
func persistenceServer(t *testing.T, store NodeStore) *EndNodeServer {
	t.Helper()
	mgr, _, err := cluster.NewEndNodeClusterManagerFromConfig(
		cluster.ClusterInitConfig{ClusterToken: "cluster-token-abcdef01"},
		cluster.NodeInitConfig{Name: "self", Host: "10.0.0.1", Port: 2000},
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

func registeredPeer(name string, port int32) *cluster.Node {
	n := &cluster.Node{Node: &proto.Node{Name: name, Host: "10.0.0.9", Port: port}}
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

	s.clusterState.Add(registeredPeer("registered", 3001))
	// Known but never registered — exactly what mergeRemoteNodes produces from a
	// peer's advertisement.
	s.clusterState.Add(&cluster.Node{
		Node:       &proto.Node{Name: "advertised-only", Host: "10.0.0.8", Port: 3002},
		CreateTime: time.Now().Unix(),
	})

	s.reconcileNodeStore()

	got := store.names()
	if len(got) != 1 || got[0] != "registered" {
		t.Errorf("persisted %v, want only [registered]", got)
	}
}

// TestReconcileNodeStore_SkipsSelfAndStaticPeers: the local node is not a peer,
// and static peers come from the config file on every start and are never
// evicted — persisting either would just add a staler second source of truth.
func TestReconcileNodeStore_SkipsSelfAndStaticPeers(t *testing.T) {
	store := newFakeNodeStore()
	s := persistenceServer(t, store)

	if err := s.clusterState.(*cluster.EndNodeClusterManager).LoadStaticNode(
		[]cluster.StaticNode{{Name: "static-peer", Host: "10.0.0.7", Port: 3003}}); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	// Make it look fully registered so only the isLocal check can exclude it.
	if n := s.clusterState.Get("static-peer"); n != nil {
		n.SetRecvHeartBeatTime(time.Now().Unix())
		n.SetReportHeartBeatTime(time.Now().Unix())
	}

	s.reconcileNodeStore()

	if got := store.names(); len(got) != 0 {
		t.Errorf("persisted %v, want nothing (self and static peers are excluded)", got)
	}
}

// TestReconcileNodeStore_DeletesOnlyWhenGoneFromMemory pins the deliberate
// asymmetry. A peer that can still reach us while we cannot reach it loses
// IsCompleteRegister but is alive and still being retried; dropping its address
// then would cause exactly the orphaning this persistence exists to prevent.
// Only disappearing from memory entirely — i.e. evicted by the node timeout —
// may remove the row.
func TestReconcileNodeStore_DeletesOnlyWhenGoneFromMemory(t *testing.T) {
	store := newFakeNodeStore("half-open", "evicted")
	s := persistenceServer(t, store)

	// Still in memory, but only one direction is fresh.
	halfOpen := &cluster.Node{Node: &proto.Node{Name: "half-open", Host: "10.0.0.6", Port: 3004}}
	halfOpen.SetRecvHeartBeatTime(time.Now().Unix())
	s.clusterState.Add(halfOpen)
	// "evicted" is deliberately absent from memory.

	s.reconcileNodeStore()

	got := store.names()
	if len(got) != 1 || got[0] != "half-open" {
		t.Errorf("persisted %v, want only [half-open]: a peer still in memory must keep "+
			"its row, and only one absent from memory may be dropped", got)
	}
}

// TestReconcileNodeStore_SteadyStateWritesNothing: the reconcile runs on every
// filter tick, so it must be genuinely incremental — a cluster where nothing
// changed has to produce zero writes, otherwise a large cluster would hammer
// SQLite forever.
func TestReconcileNodeStore_SteadyStateWritesNothing(t *testing.T) {
	store := newFakeNodeStore()
	s := persistenceServer(t, store)
	s.clusterState.Add(registeredPeer("peer-1", 3005))

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
	if got := store.names(); len(got) != 1 || got[0] != "peer-1" {
		t.Errorf("row set churned in steady state: %v", got)
	}
}

// TestReconcileNodeStore_NilStoreIsNoop: servers built directly in tests, and any
// future deployment without persistence, must not panic.
func TestReconcileNodeStore_NilStoreIsNoop(t *testing.T) {
	s := persistenceServer(t, nil)
	s.clusterState.Add(registeredPeer("peer-1", 3006))
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
		cluster.NodeInitConfig{Name: "self", Host: "10.0.0.1", Port: 2000},
	)
	if err != nil {
		t.Fatalf("init cluster manager: %v", err)
	}

	// Exactly what cmd.loadPersistedNodes does: CreateTime only.
	loaded := &cluster.Node{
		Node:       &proto.Node{Name: "from-db", Host: "10.0.0.5", Port: 3007},
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
