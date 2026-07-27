package cluster_test

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// These tests pin the identity model: the directory is keyed by a node's
// permanent node_id, while name, host and port are mutable attributes an
// operator may edit at any time. Every case below used to be either a hard
// rejection (the old host+port+name triple had to match exactly) or a silent
// duplicate.

func claim(name, id, host string, port int32) *proto.Node {
	return &proto.Node{Name: name, NodeId: id, Host: host, Port: port}
}

// pinLocalNode fixes the package-global local node for the duration of a test.
// LoadStaticNode filters seeds that collide with it, and the global is shared
// across every test in the package, so a test that loads seeds must say what it
// is colliding against instead of inheriting whatever ran before it.
func pinLocalNode(t *testing.T, name, host string, port int32) {
	t.Helper()
	ln := cluster.GetLocalNode()
	saved := ln.Node
	ln.Node = proto.Node{Name: name, Host: host, Port: port}
	t.Cleanup(func() { ln.Node = saved })
}

// resolvedManager returns a manager holding one fully-registered peer.
func resolvedManager(t *testing.T) (*cluster.NodeManager, *cluster.Node) {
	t.Helper()
	nm := cluster.NewNodeManager()
	res := nm.ResolveRegistration(claim("peer", "peer-id", "10.0.0.1", 5000), "tok-1", time.Now().Unix())
	if res.Outcome != cluster.ResolveNew {
		t.Fatalf("seed outcome = %v, want new", res.Outcome)
	}
	res.Node.SetReportHeartBeatTime(time.Now().Unix())
	return &nm, res.Node
}

func TestResolveRegistration_Outcomes(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name    string
		second  *proto.Node
		want    cluster.ResolveOutcome
		wantKey string
	}{
		{
			name:    "unchanged re-registration",
			second:  claim("peer", "peer-id", "10.0.0.1", 5000),
			want:    cluster.ResolveSame,
			wantKey: "peer-id",
		},
		{
			// The case the whole redesign exists for: an operator edited
			// proxy_host. Under the old triple-match this was rejected outright
			// and the node could not rejoin until its stale entry aged out.
			name:    "address changed",
			second:  claim("peer", "peer-id", "10.9.9.9", 5000),
			want:    cluster.ResolveMoved,
			wantKey: "peer-id",
		},
		{
			name:    "port changed",
			second:  claim("peer", "peer-id", "10.0.0.1", 6000),
			want:    cluster.ResolveMoved,
			wantKey: "peer-id",
		},
		{
			name:    "name changed",
			second:  claim("renamed", "peer-id", "10.0.0.1", 5000),
			want:    cluster.ResolveRenamed,
			wantKey: "peer-id",
		},
		{
			// A node rebuilt with a fresh database: same config, new identity.
			// Accepted — the instance that owned the old id is gone and will
			// never come back to release it — but reported.
			name:    "identity replaced at the same address",
			second:  claim("peer", "new-id", "10.0.0.1", 5000),
			want:    cluster.ResolveReplaced,
			wantKey: "new-id",
		},
		{
			name:    "name and address changed together",
			second:  claim("renamed", "peer-id", "10.9.9.9", 6000),
			want:    cluster.ResolveMoved,
			wantKey: "peer-id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nm, _ := resolvedManager(t)

			res := nm.ResolveRegistration(tc.second, "tok-2", now)
			if res.Outcome != tc.want {
				t.Errorf("outcome = %v, want %v", res.Outcome, tc.want)
			}
			if got := nm.Get(tc.wantKey); got == nil {
				t.Fatalf("entry not filed under %q; keys are %v", tc.wantKey, keysOf(nm.GetAllNode()))
			}
			if all := nm.GetAllNode(); len(all) != 1 {
				t.Errorf("directory holds %d entries, want exactly 1: %v", len(all), keysOf(nm.GetAllNode()))
			}
			got := nm.Get(tc.wantKey)
			if got.GetHost() != tc.second.GetHost() || got.GetPort() != tc.second.GetPort() {
				t.Errorf("address = %s:%d, want %s:%d",
					got.GetHost(), got.GetPort(), tc.second.GetHost(), tc.second.GetPort())
			}
			if got.GetName() != tc.second.GetName() {
				t.Errorf("name = %q, want %q", got.GetName(), tc.second.GetName())
			}
		})
	}
}

func keysOf(all map[string]*cluster.Node) []string {
	out := []string{}
	for k := range all {
		out = append(out, k)
	}
	return out
}

// TestResolveRegistration_RepeatReturnsSameToken preserves the historical code
// 102 contract: a peer that is already registered gets the token it was given
// before, not a new one, so a redundant registration does not invalidate its
// working session.
func TestResolveRegistration_RepeatReturnsSameToken(t *testing.T) {
	nm, _ := resolvedManager(t)

	res := nm.ResolveRegistration(claim("peer", "peer-id", "10.0.0.1", 5000), "tok-2", time.Now().Unix())
	if !res.Repeat {
		t.Error("Repeat = false; an already-registered peer must be reported as a repeat")
	}
	if res.Token != "tok-1" {
		t.Errorf("token = %q, want the original tok-1", res.Token)
	}
}

// TestResolveRegistration_MovedRotatesToken: the out-token an entry holds was
// issued by whatever answered the OLD address. Carrying it across a move would
// authenticate us to the wrong process.
func TestResolveRegistration_MovedRotatesToken(t *testing.T) {
	nm, first := resolvedManager(t)
	first.SetOutToken("out-token-from-old-address")

	res := nm.ResolveRegistration(claim("peer", "peer-id", "10.9.9.9", 5000), "tok-2", time.Now().Unix())
	if res.Token != "tok-2" {
		t.Errorf("token = %q, want a freshly minted tok-2", res.Token)
	}
	if got := res.Node.GetOutToken(); got != "" {
		t.Errorf("out token = %q, want empty: it was issued by the node at the old address", got)
	}
	if got := res.Node.GetReportHeartBeatTime(); got != 0 {
		t.Errorf("report heartbeat = %d, want 0: we have not reported to the new address yet", got)
	}
}

// TestResolveRegistration_ReplacesEntryRatherThanMutating guards the concurrency
// contract. Identity fields are published read-only and are read unlocked by
// ComputeNodesSum and by gRPC marshalling the shared *proto.Node, so a move must
// swap in a new entry. Mutating in place would also be useless: grpc.Dial pins
// its target, so a cached connection would keep reaching the old address.
func TestResolveRegistration_ReplacesEntryRatherThanMutating(t *testing.T) {
	nm, first := resolvedManager(t)

	res := nm.ResolveRegistration(claim("peer", "peer-id", "10.9.9.9", 5000), "tok-2", time.Now().Unix())
	if res.Node == first {
		t.Fatal("the live node was mutated in place; identity fields are published read-only")
	}
	if first.GetHost() != "10.0.0.1" {
		t.Errorf("the old entry's host was rewritten to %q; it must be left untouched", first.GetHost())
	}
}

// TestResolveRegistration_EmptyIdIsNotAConflict: an absent node_id is the normal
// state for a static seed, a replayed database row, or a pre-2.8 peer. Treating
// it as "a different node" would break every one of those.
func TestResolveRegistration_EmptyIdIsNotAConflict(t *testing.T) {
	pinLocalNode(t, "self", "10.99.99.99", 9999)
	nm := cluster.NewNodeManager()
	// A static seed: address only, no identity.
	if err := nm.LoadStaticNode([]cluster.StaticNode{{Name: "seed", Host: "10.0.0.1", Port: 5000}}); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}

	res := nm.ResolveRegistration(claim("real-name", "real-id", "10.0.0.1", 5000), "tok", time.Now().Unix())
	if res.Outcome == cluster.ResolveReplaced {
		t.Error("learning a seed's identity was reported as a takeover")
	}
	if all := nm.GetAllNode(); len(all) != 1 {
		t.Fatalf("directory holds %d entries, want 1: a resolved seed must not leave a duplicate", len(all))
	}
	n := nm.Get("real-id")
	if n == nil {
		t.Fatal("entry was not re-filed under the identity it reported")
	}
	if !n.IsLocal() {
		t.Error("a resolved static seed must stay static, or it would start being evicted " +
			"despite still being in the config file")
	}
	if n.GetName() != "real-name" {
		t.Errorf("name = %q, want the name the peer reports over the one an operator typed", n.GetName())
	}
}

// TestResolveRegistration_LegacyClaimKeepsKnownIdentity: once we know a peer's
// id, an exchange that carries none must not erase it. A single legacy round
// trip would otherwise strip identity from the whole directory.
func TestResolveRegistration_LegacyClaimKeepsKnownIdentity(t *testing.T) {
	nm, _ := resolvedManager(t)

	// A pre-2.8 repeat from a peer we have already identified: it reports no id,
	// same name, same address. One legacy exchange must not strip identity off
	// the directory — otherwise a single downgraded peer, or one mid-upgrade,
	// would unfile itself and be relearned as a stranger.
	res := nm.ResolveRegistration(claim("peer", "", "10.0.0.1", 5000), "tok-2", time.Now().Unix())
	if got := res.Node.GetNodeId(); got != "peer-id" {
		t.Errorf("node id = %q, want the previously known peer-id", got)
	}
	if nm.Get("peer-id") == nil {
		t.Errorf("entry left its identity key; keys are %v", keysOf(nm.GetAllNode()))
	}
	if all := nm.GetAllNode(); len(all) != 1 {
		t.Errorf("directory holds %d entries, want 1: %v", len(all), keysOf(all))
	}
}

// TestResolveRegistration_LegacyClaimAtAReusedAddressDoesNotInherit is the other
// half of that rule. An id-less claim is matched by ADDRESS, so a pre-2.8 node
// that took over a decommissioned address would otherwise adopt the departed
// node's identity — and we would go on to persist it under that id and treat
// every later message from the real owner as a takeover. A differing name is the
// only evidence available that this is not the same peer, so it is respected:
// dropping an id is recoverable (the next identified exchange restores it),
// attributing one to the wrong node is not.
func TestResolveRegistration_LegacyClaimAtAReusedAddressDoesNotInherit(t *testing.T) {
	nm, _ := resolvedManager(t)

	res := nm.ResolveRegistration(claim("a-different-node", "", "10.0.0.1", 5000), "tok-2", time.Now().Unix())
	if got := res.Node.GetNodeId(); got != "" {
		t.Errorf("node id = %q, want empty: a differently-named id-less claim must not "+
			"inherit the identity of whoever held that address", got)
	}
	if nm.Get("peer-id") != nil {
		t.Error("the departed node's identity key still resolves")
	}
}

// TestResolveRegistration_AbsorbsStaleEntryAtSameAddress: the registering peer
// just proved it holds the address over a live authenticated connection, so a
// placeholder filed there — a static seed whose configured name was wrong, say —
// is stale by definition. Leaving it behind is what produced permanent ghost
// entries that every ?target=all fan-out then reported as a failure.
func TestResolveRegistration_AbsorbsStaleEntryAtSameAddress(t *testing.T) {
	pinLocalNode(t, "self", "10.99.99.99", 9999)
	nm := cluster.NewNodeManager()
	if err := nm.LoadStaticNode([]cluster.StaticNode{{Name: "typo-name", Host: "10.0.0.1", Port: 5000}}); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	// A different peer, with its own identity, registers from that same address.
	nm.ResolveRegistration(claim("actual", "actual-id", "10.0.0.2", 5000), "tok-a", time.Now().Unix())
	res := nm.ResolveRegistration(claim("actual", "actual-id", "10.0.0.1", 5000), "tok-b", time.Now().Unix())

	if len(res.Absorbed) == 0 {
		t.Error("the placeholder at the claimed address was not absorbed")
	}
	if all := nm.GetAllNode(); len(all) != 1 {
		t.Errorf("directory holds %d entries, want 1: %v", len(all), keysOf(nm.GetAllNode()))
	}
}

// TestResolveRegistration_NewIdentityAtAKnownAddressIsATakeover pins the
// discriminator between the two things that look alike here.
//
// "A node was rebuilt with a fresh database" and "some other node claims this
// address" both arrive as a new identity at a known address, and in both cases
// the previous identity only stopped heartbeating moments ago so it still looks
// freshly registered. What separates them is the NAME: a rebuild did not change
// its config, so it comes back under the same name. That case is accepted and
// reported. The differently-named case is treated as a collision instead — see
// TestResolveRegistration_TwoLiveNodesOnOneAddressDoNotPingPong for why
// accepting it would make the pair churn forever.
func TestResolveRegistration_NewIdentityAtAKnownAddressIsATakeover(t *testing.T) {
	nm := cluster.NewNodeManager()
	now := time.Now().Unix()
	first := nm.ResolveRegistration(claim("incumbent", "incumbent-id", "10.0.0.1", 5000), "tok-1", now)
	if !first.Node.RegisteredLocal() {
		t.Fatal("precondition: the incumbent must look actively registered")
	}

	res := nm.ResolveRegistration(claim("incumbent", "new-id", "10.0.0.1", 5000), "tok-2", now)

	if res.Outcome != cluster.ResolveReplaced {
		t.Errorf("outcome = %v, want replaced", res.Outcome)
	}
	if all := nm.GetAllNode(); len(all) != 1 {
		t.Errorf("directory holds %d entries, want 1: an address hosts one listening socket", len(all))
	}
	if !nm.IsDirty("incumbent-id", now) {
		t.Error("the displaced identity must be tombstoned, or gossip would restore it")
	}
}

// TestResolveRegistration_DoesNotEvictLiveRival: address de-duplication must not
// delete a peer that is demonstrably talking to us. Here the claimant is matched
// by its own identity (it moved), and the address it moved onto is already held
// by a different, actively registered node — a genuine misconfiguration to
// report, not one to resolve by dropping a working peer.
func TestResolveRegistration_DoesNotEvictLiveRival(t *testing.T) {
	nm := cluster.NewNodeManager()
	now := time.Now().Unix()

	// A live peer holding 10.0.0.2:5000.
	nm.ResolveRegistration(claim("holder", "holder-id", "10.0.0.2", 5000), "tok-h", now)
	// A different peer elsewhere...
	nm.ResolveRegistration(claim("mover", "mover-id", "10.0.0.1", 5000), "tok-m", now)
	// ...which now claims the address the live peer holds.
	res := nm.ResolveRegistration(claim("mover", "mover-id", "10.0.0.2", 5000), "tok-m2", now)

	if len(res.LiveAddrRivals) == 0 {
		t.Error("a live entry contesting the claimed address must be reported")
	}
	if nm.Get("holder-id") == nil {
		t.Error("a live, registered peer was deleted to make room for a claimant")
	}
	if nm.IsDirty("holder-id", now) {
		t.Error("a live peer was tombstoned merely for sharing an address")
	}
}

// --- tombstones ---

// TestTombstone_BlocksGossipUntilExpiry is the mechanism that makes a superseded
// identity actually disappear. Peers that have not noticed keep advertising it,
// and the add-only merge would reinsert it with a fresh CreateTime every round,
// renewing it forever. Reclaiming immediately does not work either: peers notice
// at different times, so the entry would just come straight back.
func TestTombstone_BlocksGossipUntilExpiry(t *testing.T) {
	nm := cluster.NewNodeManager()
	now := time.Now().Unix()

	nm.MarkDirty("gone-id", now)

	if !nm.IsDirty("gone-id", now) {
		t.Error("id is not tombstoned immediately after MarkDirty")
	}
	if !nm.IsDirty("gone-id", now+cluster.DirtyNodeTTL-1) {
		t.Error("tombstone expired early; it must outlive cluster-wide convergence")
	}
	if nm.IsDirty("gone-id", now+cluster.DirtyNodeTTL+1) {
		t.Error("tombstone never expires; a reused id could never be relearned")
	}
	if nm.IsDirty("", now) {
		t.Error("an empty id must never be considered tombstoned: it is the normal " +
			"state for static seeds and pre-2.8 peers, and would block all of them")
	}
}

// TestTombstone_DropsTheEntryToo: a tombstone that left the entry in place would
// keep the stale address being dialled for the whole TTL.
func TestTombstone_DropsTheEntryToo(t *testing.T) {
	nm, _ := resolvedManager(t)
	nm.MarkDirty("peer-id", time.Now().Unix())
	if nm.Get("peer-id") != nil {
		t.Error("MarkDirty tombstoned the id but left the directory entry behind")
	}
}

// TestTombstone_ClearedByDirectRegistration: only gossip is held back. A node
// that identifies itself directly is authoritative about its own existence, so a
// stale tombstone must never be able to lock it out.
func TestTombstone_ClearedByDirectRegistration(t *testing.T) {
	nm := cluster.NewNodeManager()
	now := time.Now().Unix()
	nm.MarkDirty("peer-id", now)

	res := nm.ResolveRegistration(claim("peer", "peer-id", "10.0.0.1", 5000), "tok", now)
	if res.Outcome != cluster.ResolveNew {
		t.Errorf("outcome = %v, want new", res.Outcome)
	}
	if nm.IsDirty("peer-id", now) {
		t.Error("the node re-registered itself but its tombstone was not lifted")
	}
}

// TestTombstone_ReplacementTombstonesTheOldIdentity: the rebuilt-node case. The
// superseded id must be blocked, or the peers still advertising it would feed it
// straight back.
func TestTombstone_ReplacementTombstonesTheOldIdentity(t *testing.T) {
	nm, _ := resolvedManager(t)
	now := time.Now().Unix()

	res := nm.ResolveRegistration(claim("peer", "new-id", "10.0.0.1", 5000), "tok-2", now)
	if res.Outcome != cluster.ResolveReplaced {
		t.Fatalf("outcome = %v, want replaced", res.Outcome)
	}
	if !nm.IsDirty("peer-id", now) {
		t.Error("the superseded identity was not tombstoned; gossip would reinstate it")
	}
	if nm.IsDirty("new-id", now) {
		t.Error("the incoming identity was tombstoned")
	}
}

// TestFilter_ExpiresTombstones: nothing else sweeps the dirty set, so an id that
// is never seen again would otherwise be remembered for the life of the process.
func TestFilter_ExpiresTombstones(t *testing.T) {
	nm := cluster.NewNodeManager()
	nm.MarkDirty("old-id", time.Now().Unix()-cluster.DirtyNodeTTL-10)

	nm.Filter(func(*cluster.Node) bool { return true })

	if nm.IsDirty("old-id", time.Now().Unix()) {
		t.Error("an expired tombstone survived a filter pass")
	}
}

// --- AdoptIdentity ---

// TestAdoptIdentity_KeepsSessionAndDoesNotFakeHeartbeats is the outbound
// counterpart to registration: we dialled a seed and the response told us who
// answered. The session must survive the re-keying, and the heartbeat timestamps
// must NOT be stamped — IsCompleteRegister verifies no token, so faking them
// would advertise a peer we have merely phoned as an authenticated member.
func TestAdoptIdentity_KeepsSessionAndDoesNotFakeHeartbeats(t *testing.T) {
	pinLocalNode(t, "self", "10.99.99.99", 9999)
	nm := cluster.NewNodeManager()
	if err := nm.LoadStaticNode([]cluster.StaticNode{{Name: "seed", Host: "10.0.0.1", Port: 5000}}); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	seed := nm.FindByAddr("10.0.0.1", 5000)
	if seed == nil {
		t.Fatal("seed not loaded")
	}
	seed.SetOutToken("out-token")

	n, changed := nm.AdoptIdentity("10.0.0.1", 5000, "real-id", "real-name")
	if !changed {
		t.Fatal("AdoptIdentity reported no change for an unidentified entry")
	}
	if nm.Get("real-id") == nil {
		t.Errorf("entry not re-filed under its identity; keys are %v", keysOf(nm.GetAllNode()))
	}
	if all := nm.GetAllNode(); len(all) != 1 {
		t.Errorf("directory holds %d entries, want 1: %v", len(all), keysOf(nm.GetAllNode()))
	}
	if got := n.GetOutToken(); got != "out-token" {
		t.Errorf("out token = %q, want it carried across: the session belongs to the peer, "+
			"not to the key it was filed under", got)
	}
	if n.IsCompleteRegister() {
		t.Error("AdoptIdentity stamped heartbeat timestamps; the peer would be advertised " +
			"as authenticated without any handshake")
	}
	if !n.IsLocal() {
		t.Error("a resolved static seed must stay static")
	}
}

// TestAdoptIdentity_LeavesIdentifiedEntriesAlone: an entry whose id we already
// know is not provisional, and rewriting it from a response would let whatever
// answers an address take over an established entry.
func TestAdoptIdentity_LeavesIdentifiedEntriesAlone(t *testing.T) {
	nm, _ := resolvedManager(t)
	_, changed := nm.AdoptIdentity("10.0.0.1", 5000, "other-id", "other")
	if changed {
		t.Error("AdoptIdentity rewrote an entry that already had an identity")
	}
	if nm.Get("peer-id") == nil {
		t.Error("the established entry was moved")
	}
}

// --- lookup ---

func TestLookupPeer_ResolutionOrder(t *testing.T) {
	nm, _ := resolvedManager(t)

	// By identity, even though every other attribute changed.
	if got := nm.LookupPeer(claim("totally-different", "peer-id", "10.9.9.9", 9999)); got == nil {
		t.Error("lookup by node_id failed; identity must survive any config edit")
	}
	// By address, for an entry we have not identified.
	if got := nm.LookupPeer(claim("peer", "", "10.0.0.1", 5000)); got == nil {
		t.Error("lookup by address failed")
	}
	// A claim carrying an id must NOT be matched by name alone: a mistyped label
	// would otherwise bind a peer to an unrelated address.
	if got := nm.LookupPeer(claim("peer", "stranger-id", "192.168.1.1", 7000)); got != nil {
		t.Error("an identified claim was matched by name; a config typo could bind a peer " +
			"to an unrelated entry")
	}
}

func TestFindByName_ReportsDuplicates(t *testing.T) {
	nm := cluster.NewNodeManager()
	now := time.Now().Unix()
	nm.ResolveRegistration(claim("dup", "id-a", "10.0.0.1", 5000), "t1", now)
	nm.ResolveRegistration(claim("dup", "id-b", "10.0.0.2", 5000), "t2", now)

	n, count := nm.FindByName("dup")
	if n == nil {
		t.Fatal("FindByName returned nothing")
	}
	if count != 2 {
		t.Errorf("count = %d, want 2: two identities may now share a name, and callers "+
			"addressing by name need to know it is ambiguous", count)
	}
}

// --- concurrency ---

// TestResolveRegistration_ConcurrentSameIdentity: two peers registering at once,
// or one retrying, must not interleave into a lost update or a duplicate entry.
// This is why lookup, replacement and address de-duplication all happen under one
// write lock rather than as separate Get/Add/Delete calls.
func TestResolveRegistration_ConcurrentSameIdentity(t *testing.T) {
	nm := cluster.NewNodeManager()
	const n = 64

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Same identity, alternating addresses.
			host := fmt.Sprintf("10.0.0.%d", 1+i%2)
			nm.ResolveRegistration(claim("peer", "peer-id", host, 5000),
				fmt.Sprintf("tok-%d", i), time.Now().Unix())
		}(i)
	}
	wg.Wait()

	all := nm.GetAllNode()
	if len(all) != 1 {
		t.Errorf("directory holds %d entries after concurrent registration, want exactly 1: %v",
			len(all), keysOf(nm.GetAllNode()))
	}
	if nm.Get("peer-id") == nil {
		t.Error("the surviving entry is not filed under its identity")
	}
}

// TestFilter_ConcurrentAddIsNotLost: Filter used to build a replacement map under
// a read lock, release it, then take the write lock to swap it in. An Add landing
// in that gap wrote to the map being discarded and vanished — and that gap is
// exactly when a registration completes.
func TestFilter_ConcurrentAddIsNotLost(t *testing.T) {
	nm := cluster.NewNodeManager()
	keep := &cluster.Node{Node: &proto.Node{Name: "keep", NodeId: "keep-id", Host: "10.0.0.1", Port: 5000}}
	keep.SetRecvHeartBeatTime(time.Now().Unix())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			nm.Filter(func(n *cluster.Node) bool { return n.IsValid() })
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			nm.Add(keep)
		}
	}()
	wg.Wait()

	nm.Add(keep)
	if nm.Get("keep-id") == nil {
		t.Error("an Add concurrent with Filter was silently discarded")
	}
}

// TestMarkDirty_DemotesStaticSeedInsteadOfDeleting: static_nodes is the only
// bootstrap path there is, and it is read once at startup. Deleting a configured
// seed because whoever answers its address changed identity would silently remove
// the node's ability to rejoin at all, and nothing would ever put it back.
func TestMarkDirty_DemotesStaticSeedInsteadOfDeleting(t *testing.T) {
	pinLocalNode(t, "self", "10.99.99.99", 9999)
	nm := cluster.NewNodeManager()
	if err := nm.LoadStaticNode([]cluster.StaticNode{{Name: "seed", Host: "10.0.0.1", Port: 5000}}); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	if _, ok := nm.AdoptIdentity("10.0.0.1", 5000, "old-id", "seed"); !ok {
		t.Fatal("AdoptIdentity did not resolve the seed")
	}
	now := time.Now().Unix()

	nm.MarkDirty("old-id", now)

	if !nm.IsDirty("old-id", now) {
		t.Error("the superseded identity was not tombstoned")
	}
	seed := nm.FindByAddr("10.0.0.1", 5000)
	if seed == nil {
		t.Fatal("the static seed was deleted; the node can no longer bootstrap and nothing " +
			"reloads static_nodes after startup")
	}
	if seed.GetNodeId() != "" {
		t.Errorf("seed kept node_id %q; it must be rewound to provisional so it can "+
			"re-identify", seed.GetNodeId())
	}
	if !seed.IsLocal() {
		t.Error("the demoted seed is no longer static and would start being evicted")
	}
}

// TestMarkDirty_DeletesDynamicEntries: only configured seeds are demoted; a peer
// we merely learned about has no config to fall back on and should just go.
func TestMarkDirty_DeletesDynamicEntries(t *testing.T) {
	nm, _ := resolvedManager(t)
	nm.MarkDirty("peer-id", time.Now().Unix())
	if len(nm.GetAllNode()) != 0 {
		t.Errorf("dynamic entry survived MarkDirty: %v", keysOf(nm.GetAllNode()))
	}
}

// TestAddIfAbsent_DoesNotClobberAConcurrentRegistration is why gossip cannot use
// Lookup-then-Add. Those are two lock acquisitions, and a registration landing
// between them would be overwritten by the token-less gossip copy — silently
// destroying the session of a peer that had just completed its handshake.
func TestAddIfAbsent_DoesNotClobberAConcurrentRegistration(t *testing.T) {
	nm, _ := resolvedManager(t)
	established := nm.Get("peer-id")
	if established.GetInToken() == "" {
		t.Fatal("precondition: the established peer must hold a session token")
	}

	gossip := &cluster.Node{
		Node:       &proto.Node{Name: "peer", NodeId: "peer-id", Host: "10.0.0.1", Port: 5000},
		CreateTime: time.Now().Unix(),
	}
	if nm.AddIfAbsent(gossip) {
		t.Error("AddIfAbsent stored a node that was already known")
	}
	if got := nm.Get("peer-id"); got != established {
		t.Error("the established entry was replaced by a gossip copy")
	}
	if nm.Get("peer-id").GetInToken() == "" {
		t.Error("the peer's session token was destroyed")
	}
}

func TestAddIfAbsent_StoresGenuinelyNewNodes(t *testing.T) {
	nm := cluster.NewNodeManager()
	n := &cluster.Node{Node: &proto.Node{Name: "new", NodeId: "new-id", Host: "10.0.0.1", Port: 5000}}
	if !nm.AddIfAbsent(n) {
		t.Fatal("AddIfAbsent refused an unknown node")
	}
	if nm.Get("new-id") == nil {
		t.Error("the node was reported stored but is not in the directory")
	}
}

// TestDeleteNode_IgnoresSupersededPointers: callers hold a *Node captured at the
// start of a heartbeat round, and an address or identity change REPLACES the
// entry. A key recomputed from a stale pointer can by then belong to a different,
// live node, which must not be evicted on its behalf.
func TestDeleteNode_IgnoresSupersededPointers(t *testing.T) {
	pinLocalNode(t, "self", "10.99.99.99", 9999)
	nm := cluster.NewNodeManager()
	if err := nm.LoadStaticNode([]cluster.StaticNode{{Name: "seed", Host: "10.0.0.1", Port: 5000}}); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	// A caller captured this pointer at the start of a heartbeat round. It is
	// filed under a provisional ADDRESS key, which is the key most likely to be
	// re-occupied by someone else.
	stale := nm.FindByAddr("10.0.0.1", 5000)
	if stale == nil {
		t.Fatal("precondition: seed must be loaded")
	}

	// Meanwhile a different node takes that address over, so the key the stale
	// pointer computes now belongs to a live entry.
	nm.MarkDirty("unrelated", time.Now().Unix())
	nm.Delete(cluster.ProvisionalKey("10.0.0.1", 5000))
	live := &cluster.Node{Node: &proto.Node{Name: "other", Host: "10.0.0.1", Port: 5000}}
	nm.Add(live)

	nm.DeleteNode(stale)

	if got := nm.FindByAddr("10.0.0.1", 5000); got != live {
		t.Error("DeleteNode evicted a live entry on behalf of a superseded pointer that " +
			"merely computed the same key")
	}
}

// TestDeleteNode_RemovesTheEntryItStillOwns: the guard must not make DeleteNode
// a no-op in the ordinary case.
func TestDeleteNode_RemovesTheEntryItStillOwns(t *testing.T) {
	nm, n := resolvedManager(t)
	nm.DeleteNode(n)
	if nm.Get("peer-id") != nil {
		t.Error("DeleteNode did not remove the entry it still owns")
	}
}

// TestGetProtoNodesWithFilter_DuplicateNamesAreDeterministic: Go randomises map
// iteration, so picking an arbitrary winner among same-named nodes would make a
// node's own advertised set — and therefore its digest — differ between two
// successive calls. Every heartbeat would then report a mismatch and every round
// would pay a full directory reconcile, forever.
func TestGetProtoNodesWithFilter_DuplicateNamesAreDeterministic(t *testing.T) {
	nm := cluster.NewNodeManager()
	now := time.Now().Unix()
	for _, c := range []*proto.Node{
		claim("dup", "id-a", "10.0.0.1", 5000),
		claim("dup", "id-b", "10.0.0.2", 5000),
		claim("dup", "id-c", "10.0.0.3", 5000),
	} {
		res := nm.ResolveRegistration(c, "tok", now)
		res.Node.SetReportHeartBeatTime(now)
	}

	c := &cluster.Cluster{NodeManager: nm}
	first := c.GetProtoNodesWithFilter(func(*cluster.Node) bool { return true })
	firstSum, _ := cluster.ComputeNodesSum(protoValues(first))
	for i := 0; i < 50; i++ {
		again := c.GetProtoNodesWithFilter(func(*cluster.Node) bool { return true })
		sum, _ := cluster.ComputeNodesSum(protoValues(again))
		if !bytes.Equal(firstSum, sum) {
			t.Fatalf("advertised digest changed between calls on the SAME node (%x vs %x); "+
				"the cluster would reconcile every round forever", firstSum, sum)
		}
	}
}

func protoValues(m map[string]*proto.Node) []*proto.Node {
	out := make([]*proto.Node, 0, len(m))
	for _, n := range m {
		out = append(out, n)
	}
	return out
}

// TestResolveRegistration_SameAddressChangePreservesSession: learning a peer's id
// for the first time, or recording a rename, must not tear down a working
// session. The socket has not moved, so the token and connection are still with
// the same process — and dropping them would make every peer in the cluster
// re-register once during a rolling upgrade, falling out of each other's
// advertised sets and churning the digest to record a field that changes nothing
// about reachability.
func TestResolveRegistration_SameAddressChangePreservesSession(t *testing.T) {
	nm := cluster.NewNodeManager()
	now := time.Now().Unix()
	// A peer known without an identity yet (as a pre-2.8 peer would be).
	seed := nm.ResolveRegistration(claim("peer", "", "10.0.0.1", 5000), "tok-1", now)
	seed.Node.SetOutToken("out-token")
	seed.Node.SetReportHeartBeatTime(now)

	// It upgrades and now reports an id. Nothing about its address changed.
	res := nm.ResolveRegistration(claim("peer", "peer-id", "10.0.0.1", 5000), "tok-2", now)
	if res.Node.GetNodeId() != "peer-id" {
		t.Fatalf("node id = %q, want peer-id", res.Node.GetNodeId())
	}
	if got := res.Node.GetOutToken(); got != "out-token" {
		t.Errorf("out token = %q, want it preserved: the socket did not move, so the "+
			"session is still with the same process", got)
	}
	if res.Node.GetReportHeartBeatTime() != now {
		t.Error("report heartbeat was reset, dropping the peer out of the advertised set")
	}
	if len(res.RetiredConns) != 0 {
		t.Error("a still-valid connection was retired for an address that did not change")
	}
}

// TestResolveRegistration_TwoLiveNodesOnOneAddressDoNotPingPong: address matching
// is what lets a rebuilt node reclaim its entry, but it must not make two
// genuinely different nodes misconfigured onto one address take turns replacing
// each other — each replacement tombstones the other's identity, so the pair
// would churn the directory and each other's tombstones on every heartbeat.
//
// A rebuild keeps its name (its config did not change); two nodes have two names.
// That is the discriminator.
func TestResolveRegistration_TwoLiveNodesOnOneAddressDoNotPingPong(t *testing.T) {
	nm := cluster.NewNodeManager()
	now := time.Now().Unix()

	nm.ResolveRegistration(claim("alpha", "alpha-id", "10.0.0.1", 5000), "tok-a", now)
	res := nm.ResolveRegistration(claim("beta", "beta-id", "10.0.0.1", 5000), "tok-b", now)

	if res.Outcome == cluster.ResolveReplaced {
		t.Error("a differently-named live peer was treated as a takeover; the two would " +
			"replace each other every round")
	}
	if nm.IsDirty("alpha-id", now) {
		t.Error("a live peer's identity was tombstoned merely for a colliding address")
	}
	if nm.Get("alpha-id") == nil {
		t.Error("the incumbent was evicted")
	}
	if nm.Get("beta-id") == nil {
		t.Error("the claimant was not stored")
	}
	if len(res.LiveAddrRivals) == 0 {
		t.Error("the address collision was not reported; an operator has no other signal")
	}

	// And it must be stable: repeating either registration changes nothing.
	for i := 0; i < 3; i++ {
		nm.ResolveRegistration(claim("alpha", "alpha-id", "10.0.0.1", 5000), "tok-a2", now)
		nm.ResolveRegistration(claim("beta", "beta-id", "10.0.0.1", 5000), "tok-b2", now)
	}
	if nm.Get("alpha-id") == nil || nm.Get("beta-id") == nil {
		t.Errorf("the pair did not reach a stable state; keys are %v", keysOf(nm.GetAllNode()))
	}
	if nm.IsDirty("alpha-id", now) || nm.IsDirty("beta-id", now) {
		t.Error("repeated registrations tombstoned a live identity")
	}
}

// TestResolveRegistration_RebuildStillReclaimsItsEntry guards the other side of
// that discriminator: a rebuilt node keeps its name, so it must still be matched
// by address and take its entry over.
func TestResolveRegistration_RebuildStillReclaimsItsEntry(t *testing.T) {
	nm, _ := resolvedManager(t)
	now := time.Now().Unix()

	res := nm.ResolveRegistration(claim("peer", "rebuilt-id", "10.0.0.1", 5000), "tok-2", now)
	if res.Outcome != cluster.ResolveReplaced {
		t.Fatalf("outcome = %v, want replaced: a rebuilt node keeps its name and must "+
			"reclaim its own entry", res.Outcome)
	}
	if all := nm.GetAllNode(); len(all) != 1 {
		t.Errorf("directory holds %d entries, want 1: %v", len(all), keysOf(all))
	}
	if !nm.IsDirty("peer-id", now) {
		t.Error("the superseded identity was not tombstoned")
	}
}

// TestResolveRegistration_ReplacedDropsTheOldSession: an identity change at the
// SAME address is still a different process. "Same socket, same session" holds
// only while the identity holds — a rebuilt node did not inherit its
// predecessor's tokens, so the out-token we were issued is dead and continuing
// to present it would just fail every call until the next heartbeat noticed.
func TestResolveRegistration_ReplacedDropsTheOldSession(t *testing.T) {
	nm, first := resolvedManager(t)
	first.SetOutToken("token-issued-by-the-previous-process")
	now := time.Now().Unix()

	res := nm.ResolveRegistration(claim("peer", "rebuilt-id", "10.0.0.1", 5000), "tok-2", now)
	if res.Outcome != cluster.ResolveReplaced {
		t.Fatalf("outcome = %v, want replaced", res.Outcome)
	}
	if got := res.Node.GetOutToken(); got != "" {
		t.Errorf("out token = %q, want empty: it was issued by the process that no longer exists", got)
	}
	if got := res.Node.GetReportHeartBeatTime(); got != 0 {
		t.Error("report heartbeat carried over from a session with a different process")
	}
}
