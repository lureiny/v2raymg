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
