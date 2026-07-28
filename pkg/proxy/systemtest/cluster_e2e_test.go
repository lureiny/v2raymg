//go:build integration

package systemtest

// cluster_e2e_test.go — the multi-node regression suite.
//
// Topology: 3 end nodes, each a real v2raymg subprocess talking real gRPC over
// the encrypted cluster codec. The first node is the static seed for the others;
// there is no center node any more. This is the only place the cluster
// plane is exercised end to end: the handler-level unit tests call HeartBeat in
// process and therefore skip the encryption codec, the anti-replay check and the
// method binding entirely.
//
// Run:
//   go test ./pkg/proxy/systemtest -tags=integration -run TestClusterE2E -v -timeout 20m
//
// NEW FEATURES MUST BE COVERED HERE. pkg/http/route_coverage_test.go fails the
// default CI build when a route is registered without a corresponding case in
// this file (see pkg/http/testdata/e2e_covered_routes.txt).

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestClusterE2E_Smoke is the cheapest possible proof that the orchestration
// works: one end node up, seeing itself in the directory.
// Everything else in this file depends on it, so it runs without containers or
// downloads and fails fast with full logs.
func TestClusterE2E_Smoke(t *testing.T) {
	tmpDir := t.TempDir()

	c := startE2ECluster(t, tmpDir, clusterOpts{
		HeartbeatIntervalSec: 1,
		XrayBin:              os.Getenv("XRAY_BIN"),
	})
	c.addEndNode(t)

	// A node always advertises itself, so one node converges to a directory of one.
	c.waitConverged(t, 30*time.Second)
}

// TestClusterE2E_ThreeNodeConvergence brings the end nodes up ONE AT A TIME and
// asserts the whole cluster re-converges after each, which is exactly the
// incremental-discovery path the node-directory digest change rewrote.
//
// Every convergence here runs a real reconcile heartbeat, so this is what proves
// the reconcile call survives the gRPC interceptors — a stale nonce or an
// unstamped dest_method would get it dropped and the cluster would never settle.
func TestClusterE2E_ThreeNodeConvergence(t *testing.T) {
	tmpDir := t.TempDir()

	c := startE2ECluster(t, tmpDir, clusterOpts{
		HeartbeatIntervalSec: 1,
		XrayBin:              os.Getenv("XRAY_BIN"),
	})

	for i := 1; i <= 3; i++ {
		c.addEndNode(t)
		// Generous relative to a 1s heartbeat: discovery needs a registration round
		// plus a directory exchange, and CI machines stall.
		c.waitConverged(t, 45*time.Second)
		t.Logf("converged with %d end node(s)", i)
	}

	// Each node must see all three, itself included.
	for _, s := range c.ends {
		known := s.knownNodes(t)
		if len(known) != 3 {
			t.Errorf("%s sees %d nodes, want 3: %v", s.name, len(known), known)
		}
	}
}

// TestClusterE2E_ReconcileSettles is the direct regression for the digest
// optimisation: reconciles must happen while the cluster is changing and must
// STOP once it has settled. A steady-state cluster that keeps reconciling would
// mean the digests never match — the pessimisation this design is most at risk of.
func TestClusterE2E_ReconcileSettles(t *testing.T) {
	tmpDir := t.TempDir()

	c := startE2ECluster(t, tmpDir, clusterOpts{
		HeartbeatIntervalSec: 1,
		XrayBin:              os.Getenv("XRAY_BIN"),
	})
	for i := 0; i < 3; i++ {
		c.addEndNode(t)
	}
	c.waitConverged(t, 45*time.Second)

	const marker = "reconciled node directory"

	// Let the cluster settle past the rounds that legitimately reconcile.
	time.Sleep(5 * time.Second)
	before := make([]int, len(c.ends))
	total := 0
	for i, s := range c.ends {
		before[i] = s.logs.countLines(marker)
		total += before[i]
	}

	// Guard against a vacuous pass. Bringing three nodes up MUST have produced
	// reconciles; if this is zero the log level is wrong or the marker text
	// drifted, and the "it stopped" assertion below would prove nothing.
	if total == 0 {
		t.Fatalf("no %q lines on any node — the assertion below would be vacuous "+
			"(check the log level is debug and the log message text still matches)", marker)
	}
	t.Logf("reconciles during convergence: %v (total %d)", before, total)

	// Sample across several more heartbeat rounds: in steady state the digests
	// match every round and no node should reconcile again.
	time.Sleep(8 * time.Second)
	for i, s := range c.ends {
		after := s.logs.countLines(marker)
		if after != before[i] {
			t.Errorf("%s kept reconciling in steady state: %d -> %d over ~8 heartbeat rounds "+
				"(digests are not converging)", s.name, before[i], after)
		}
	}
}

// TestClusterE2E_MixedSumSync runs a cluster where one node has the node-directory
// delta sync switched off. That node stops sending a digest, so its peers must
// classify it as legacy and keep answering it with the full directory — the exact
// mixed-version path that makes node_sum_sync safe to roll back one node at a
// time. The whole cluster must still converge.
func TestClusterE2E_MixedSumSync(t *testing.T) {
	tmpDir := t.TempDir()

	c := startE2ECluster(t, tmpDir, clusterOpts{
		HeartbeatIntervalSec: 1,
		XrayBin:              os.Getenv("XRAY_BIN"),
	})

	// Two nodes with the optimisation on...
	c.addEndNode(t)
	c.addEndNode(t)
	// ...and one with it off, standing in for an un-upgraded peer.
	off := false
	c.opts.NodeSumSync = &off
	c.addEndNode(t)
	c.opts.NodeSumSync = nil

	c.waitConverged(t, 60*time.Second)
	c.waitFanoutReady(t, 60*time.Second)

	for _, s := range c.ends {
		if known := s.knownNodes(t); len(known) != 3 {
			t.Errorf("%s sees %d nodes, want 3: %v", s.name, len(known), known)
		}
	}
}

// TestClusterE2E_PersistedDirectorySurvivesRestart is the regression for the
// persisted node directory.
//
// A three-node cluster converges, then one node is restarted with its
// static_nodes stripped. With no static peers and no center, the only way that
// node can name its peers is the rows it wrote to its own database while it was
// registered — so re-convergence proves the persist/load path end to end.
//
// Honest limitation: the surviving peers still know the restarted node and will
// register inbound, so this does not isolate "the restarted node initiated the
// contact". That direction is pinned by the reconcile/load unit tests in
// pkg/rpc/server (TestReconcileNodeStore_*, TestLoadedNodeIsNotTreatedAsRegistered);
// what this case adds is that the whole chain works against real processes.
func TestClusterE2E_PersistedDirectorySurvivesRestart(t *testing.T) {
	tmpDir := t.TempDir()

	c := startE2ECluster(t, tmpDir, clusterOpts{
		HeartbeatIntervalSec: 1,
		XrayBin:              os.Getenv("XRAY_BIN"),
	})
	for i := 0; i < 3; i++ {
		c.addEndNode(t)
	}
	c.waitConverged(t, 60*time.Second)
	c.waitFanoutReady(t, 60*time.Second)

	// The reconcile pass runs on the filter ticker (20s), so give it a turn to
	// write the directory out before killing the node.
	time.Sleep(25 * time.Second)

	victim := c.ends[2] // not the seed, so it has static peers to strip
	c.restartWithoutStaticPeers(t, victim)

	c.waitConverged(t, 60*time.Second)
	if known := victim.knownNodes(t); len(known) != 3 {
		t.Errorf("%s sees %d nodes after restarting with no static peers, want 3: %v",
			victim.name, len(known), known)
	}
}

// TestClusterE2E_AddressChangeUpdatesInPlace is the regression for the whole
// identity-keyed directory.
//
// A node's advertised address (end_node.proxy_host) is an ordinary config field
// an operator may edit. Before the directory was keyed by node_id, doing so made
// the node a stranger to its own peers: RegisterNode compared a host+port+name
// triple and refused the registration (code 105), so the node could not rejoin
// until its stale entry aged out — and while the old address stayed reachable it
// never aged out at all, because outbound calls kept its reportHeartBeatTime
// fresh.
//
// The assertion that matters is the ENTRY COUNT held constant throughout, not
// just the final address: a design that "converges" by adding a second entry for
// the same node would satisfy a name-only check while doubling the cluster.
func TestClusterE2E_AddressChangeUpdatesInPlace(t *testing.T) {
	tmpDir := t.TempDir()

	c := startE2ECluster(t, tmpDir, clusterOpts{
		HeartbeatIntervalSec: 1,
		XrayBin:              os.Getenv("XRAY_BIN"),
	})
	for i := 0; i < 3; i++ {
		c.addEndNode(t)
	}
	c.waitConverged(t, 60*time.Second)
	c.waitFanoutReady(t, 60*time.Second)

	// Not the seed: the other two have it in static_nodes by address, and this
	// case is about the dynamic path.
	victim := c.ends[2]
	before := c.ends[0].knownNodes(t)[victim.name]
	if before.NodeID == "" {
		t.Fatalf("precondition: %s must have been identified before the move", victim.name)
	}

	// 127.0.0.0/8 is entirely loopback on Linux, so this is a genuine address
	// change without needing a second interface.
	c.restartWithProxyHost(t, victim, "127.0.0.2")

	// The DEADLINE is half the assertion. Identity-keyed repair takes a heartbeat
	// or two (1s in these tests). The pre-2.8 behaviour also "converged" eventually,
	// but only by letting the stale entry age out — which cannot happen in less than
	// NodeTimeOut (60s) plus a filter tick. A bound below 60s is therefore what
	// distinguishes a real in-place update from the old wait-it-out path.
	wantAddr := fmt.Sprintf("127.0.0.2:%d", victim.rpcPort)
	c.waitPeerAddress(t, victim.name, wantAddr, len(c.ends), 30*time.Second)

	// Identity survived the move; that is what let peers update in place.
	after := c.ends[0].knownNodes(t)[victim.name]
	if after.NodeID != before.NodeID {
		t.Errorf("%s node_id changed across a config edit: %q -> %q; identity must be "+
			"stable or every edit would look like a new node",
			victim.name, before.NodeID, after.NodeID)
	}

	// The address is only useful if traffic actually follows it. A cached
	// grpc.ClientConn is pinned to the address it was dialled with, so a peer that
	// updated the field without replacing its entry would still reach the old one.
	c.waitFanoutReady(t, 30*time.Second)

	for _, s := range c.ends {
		if got := s.nodeCount(t); got != len(c.ends) {
			t.Errorf("%s holds %d directory entries, want %d", s.name, got, len(c.ends))
		}
	}
}

// TestClusterE2E_RebuiltNodeTakesOverItsAddress covers the one case identity
// keying alone cannot resolve: the database is deleted but the config is not, so
// the node returns with the same name and address under a brand-new identity.
//
// On the wire that is indistinguishable from an unrelated node claiming the
// address, and it must be accepted anyway — the instance that owned the old
// identity is gone and can never come back to withdraw its claim. What makes it
// safe is that the superseded identity is tombstoned, so the peers that have not
// noticed yet cannot gossip it back in.
//
// The restart happens well inside NodeTimeOut, which is the hard part: the old
// entry still looks freshly registered, so nothing ages out and the takeover has
// to be handled explicitly.
func TestClusterE2E_RebuiltNodeTakesOverItsAddress(t *testing.T) {
	tmpDir := t.TempDir()

	c := startE2ECluster(t, tmpDir, clusterOpts{
		HeartbeatIntervalSec: 1,
		XrayBin:              os.Getenv("XRAY_BIN"),
	})
	for i := 0; i < 3; i++ {
		c.addEndNode(t)
	}
	c.waitConverged(t, 60*time.Second)
	c.waitFanoutReady(t, 60*time.Second)

	victim := c.ends[2]
	oldID := c.ends[0].knownNodes(t)[victim.name].NodeID
	if oldID == "" {
		t.Fatalf("precondition: %s must have been identified before the rebuild", victim.name)
	}

	c.restartWithFreshDatabase(t, victim)

	// EVERY surviving peer must pick the new identity up, not just the one the
	// victim happens to list in static_nodes. That distinction is load-bearing:
	// the victim can only register outbound to peers it still knows, and its
	// database — the only other record of who they were — is exactly what was
	// deleted. Peers it cannot reach must repair through the response-identity
	// path instead, and an assertion that only checked the seed would pass while
	// the rest of the cluster stayed pinned to a dead identity forever.
	// Bounded well below NodeTimeOut (60s) on purpose: the repair must come from
	// the responder-identity path, not from the stale entry simply ageing out.
	// A generous deadline here would pass either way and prove nothing.
	deadline := time.Now().Add(30 * time.Second)
	var stuck []string
	for time.Now().Before(deadline) {
		stuck = nil
		for _, s := range c.ends {
			if s.stopped {
				continue
			}
			if got := s.nodeCount(t); got > len(c.ends) {
				t.Fatalf("%s holds %d directory entries, want at most %d: the rebuilt node was "+
					"learned as a duplicate instead of replacing its predecessor",
					s.name, got, len(c.ends))
			}
			id := s.knownNodes(t)[victim.name].NodeID
			if id == "" || id == oldID {
				stuck = append(stuck, s.name)
			}
		}
		if len(stuck) == 0 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if len(stuck) > 0 {
		t.Fatalf("%v still report %s under the old identity %q after a rebuild",
			stuck, victim.name, oldID)
	}

	c.waitConverged(t, 30*time.Second)
	c.waitFanoutReady(t, 30*time.Second)

	for _, s := range c.ends {
		if got := s.nodeCount(t); got != len(c.ends) {
			t.Errorf("%s holds %d directory entries, want %d", s.name, got, len(c.ends))
		}
	}

	// The takeover must be reported: it is the operator's only signal that a node
	// lost its identity, which is otherwise silent. Either half of the mechanism
	// counts — the peer that accepted the re-registration logs the replacement,
	// the peers that had to notice through a response log the stale entry.
	reported := 0
	for _, s := range c.ends {
		reported += s.logs.countLines("node identity replaced")
		reported += s.logs.countLines("directory entry is stale")
	}
	if reported == 0 {
		t.Error("no peer logged the identity takeover; a silent replacement is exactly " +
			"what this design set out to make visible")
	}
}

// TestClusterE2E_LegacySeedNameIsIgnored is the upgrade guard for dropping the
// static_nodes name.
//
// A seed is an ADDRESS. Each node is identified by its own persistent id and
// reports its own name in the first response, so a label written in the config
// could only ever be an unverified guess presented as fact — the field is gone.
// Configs written before that still carry `name:`, and they must keep working
// with no edit: decoding is lenient, so the key is silently dropped.
//
// The label emitted here is deliberately WRONG. If it were still load-bearing
// anywhere, nodes would end up mislabelled while still "converging" by count,
// which is why the name assertions below matter more than the count.
func TestClusterE2E_LegacySeedNameIsIgnored(t *testing.T) {
	tmpDir := t.TempDir()

	c := startE2ECluster(t, tmpDir, clusterOpts{
		HeartbeatIntervalSec: 1,
		XrayBin:              os.Getenv("XRAY_BIN"),
		LegacySeedName:       "a-stale-label-from-an-older-config",
	})
	for i := 0; i < 3; i++ {
		c.addEndNode(t)
	}

	c.waitConverged(t, 45*time.Second)
	c.waitFanoutReady(t, 45*time.Second)

	for _, s := range c.ends {
		known := s.knownNodes(t)
		if len(known) != len(c.ends) {
			t.Errorf("%s sees %d nodes, want %d: %v", s.name, len(known), len(c.ends), known)
		}
		if _, bogus := known["a-stale-label-from-an-older-config"]; bogus {
			t.Errorf("%s adopted the label from the config; the seed name must be ignored", s.name)
		}
		for _, want := range c.nodeNames() {
			view, ok := known[want]
			if !ok {
				t.Errorf("%s does not know %q by the name it reported over the wire", s.name, want)
				continue
			}
			if view.NodeID == "" {
				t.Errorf("%s knows %q but with no node_id", s.name, want)
			}
		}
	}
}
