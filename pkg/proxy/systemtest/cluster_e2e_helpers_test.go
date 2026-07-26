//go:build integration

package systemtest

// cluster_e2e_helpers_test.go — orchestration for the multi-node cluster suite.
//
// Everything here builds on the single-node helpers in e2e_server_helpers_test.go
// (ensureV2raymgBin, generateE2EConfig, the e2eServer HTTP wrappers). The pieces
// that are genuinely different for a cluster:
//
//   - every node after the first is seeded with the first one as a static peer.
//     Static peers are the only bootstrap path since the center node was removed,
//     and they are never reclaimed by the node timeout, so this mirrors how a real
//     cluster is wired.
//   - end nodes share a cluster token, and each needs a unique node name and its
//     own ports/data dir.
//   - "converged" is a cluster-wide predicate, not a per-process one: every node
//     must see every other node before assertions about fan-out mean anything.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Concurrency-safe log capture
// ---------------------------------------------------------------------------

// syncBuffer collects a subprocess's output while the test reads it concurrently.
// The cluster tests assert on log lines *while the process is still running*
// (e.g. that node-directory reconciles stop happening once the cluster settles),
// so a plain bytes.Buffer would be a data race between the exec writer goroutine
// and the test goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// countLines returns how many captured lines contain substr. Used to assert that
// a behaviour happened (>0) and, more importantly, that it stopped happening
// (count stable across a sampling window).
func (s *syncBuffer) countLines(substr string) int {
	n := 0
	for _, line := range strings.Split(s.String(), "\n") {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Cluster
// ---------------------------------------------------------------------------

type clusterOpts struct {
	// HeartbeatIntervalSec drives every cluster timing assertion. Tests use 1 so a
	// "converges within a couple of rounds" assertion takes seconds; production
	// default is 10.
	HeartbeatIntervalSec int
	// XrayBin / MihomoBin are resolved ONCE by the caller and shared by every end
	// node, so a 3-node cluster does not download the same binary three times.
	// Empty XrayBin makes each node fall back to auto_download.
	XrayBin   string
	MihomoBin string
	// NodeSumSync overrides the node-directory delta sync setting on every end
	// node; nil keeps the production default (on).
	NodeSumSync *bool
}

type e2eCluster struct {
	t      *testing.T
	tmpDir string
	bin    string // compiled v2raymg binary, shared by all nodes
	opts   clusterOpts
	ends   []*e2eServer
}

// startE2ECluster compiles the shared binary. No node is started yet: end nodes
// are added one at a time with addEndNode, which is what the discovery assertions
// depend on.
func startE2ECluster(t *testing.T, tmpDir string, opts clusterOpts) *e2eCluster {
	t.Helper()

	if opts.HeartbeatIntervalSec == 0 {
		opts.HeartbeatIntervalSec = 1
	}
	return &e2eCluster{
		t:      t,
		tmpDir: tmpDir,
		bin:    ensureV2raymgBin(t, tmpDir),
		opts:   opts,
	}
}

// addEndNode starts one more end node and waits until it serves HTTP. Every node
// after the first is seeded with the first as a static peer — the first has none,
// and learns the others from their inbound registrations. It does NOT wait for
// cluster convergence; callers assert that explicitly with waitConverged so the
// discovery path is actually under test.
func (c *e2eCluster) addEndNode(t *testing.T) *e2eServer {
	t.Helper()

	idx := len(c.ends) + 1
	name := fmt.Sprintf("e2e-end-%d", idx)
	dir := filepath.Join(c.tmpDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	mihomoDataDir := filepath.Join(dir, "mihomo-data")
	if err := os.MkdirAll(mihomoDataDir, 0o755); err != nil {
		t.Fatalf("mkdir mihomo data: %v", err)
	}

	ports := make([]int, 0, 4)
	for i := 0; i < 4; i++ {
		p, err := freeTCPPort()
		if err != nil {
			t.Fatalf("freeTCPPort for %s: %v", name, err)
		}
		ports = append(ports, p)
	}
	httpPort, rpcPort, xrayGRPCPort, mihomoAPIPort := ports[0], ports[1], ports[2], ports[3]

	// Each node needs its own forward port window; overlapping windows across
	// three co-hosted nodes would produce spurious bind conflicts.
	//
	// Kept BELOW the Linux ephemeral range (32768-60999): PortAllocator never asks
	// the OS whether a port is free, so a pool inside that range collides with
	// outbound connections from anything else on the machine. AddRule retries past
	// such a collision now, but a test suite should not lean on that.
	fwdBase := 25000 + idx*600

	// The first node is the seed and has no static peers of its own; every later
	// node points at it. That is enough to build the whole mesh: the seed learns
	// each joiner from its inbound RegisterNode, and the directory propagates from
	// there to everyone else.
	var staticPeers []staticPeer
	if len(c.ends) > 0 {
		seed := c.ends[0]
		staticPeers = []staticPeer{{Name: seed.name, Host: "127.0.0.1", Port: seed.rpcPort}}
	}

	configPath := generateE2EConfig(t, e2eServerOpts{
		Dir:          dir,
		NodeName:     name,
		HttpPort:     httpPort,
		RpcPort:      rpcPort,
		ClusterToken: e2eClusterToken,
		StaticPeers:  staticPeers,
		// The node-directory reconcile logs at debug; without this the
		// "reconciles happen, then stop" assertion could never see the line and
		// would pass vacuously.
		LogLevel:             "debug",
		HeartbeatIntervalSec: c.opts.HeartbeatIntervalSec,
		// Node-groups routes are only registered when the cluster-user layer is on,
		// so the suite cannot cover them otherwise.
		ClusterUserEnabled: true,
		EnablePrometheus:   true,
		NodeSumSync:        c.opts.NodeSumSync,
		XrayBin:            c.opts.XrayBin,
		XrayGRPCPort:       xrayGRPCPort,
		XrayConfFile:       filepath.Join(dir, "xray.json"),
		MihomoBin:          c.opts.MihomoBin,
		MihomoAPIPort:      mihomoAPIPort,
		MihomoDataDir:      mihomoDataDir,
		MihomoConf:         filepath.Join(dir, "mihomo.yaml"),
		FwdMinPort:         fwdBase,
		FwdMaxPort:         fwdBase + 500,
	})

	logs := &syncBuffer{}
	cmd := exec.Command(c.bin, "server", "--conf", configPath)
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}

	srv := &e2eServer{
		cmd:     cmd,
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", httpPort),
		token:   e2eHTTPToken,
		dir:     dir,
		name:    name,
		rpcPort: rpcPort,
		cfgPath: configPath,
		logs:    logs,
	}
	c.ends = append(c.ends, srv)

	t.Cleanup(func() {
		srv.shutdown()
		if t.Failed() {
			t.Logf("%s logs:\n%s", name, truncate(logs.String(), 8000))
		}
	})

	if err := srv.waitReady(60 * time.Second); err != nil {
		t.Fatalf("%s never became ready: %v\nlogs:\n%s", name, err, truncate(logs.String(), 6000))
	}
	t.Logf("%s ready at %s (rpc %d)", name, srv.baseURL, rpcPort)
	return srv
}

// nodeNames returns the names every end node is expected to know about once the
// cluster has converged — the node directory includes the node itself.
func (c *e2eCluster) nodeNames() []string {
	out := make([]string, 0, len(c.ends))
	for _, s := range c.ends {
		out = append(out, s.name)
	}
	return out
}

// knownNodes reads one node's view of the cluster via GET /api/node.
func (s *e2eServer) knownNodes(t *testing.T) map[string]bool {
	t.Helper()
	code, body, err := s.apiGet("/api/node", nil)
	if err != nil {
		t.Fatalf("%s GET /api/node: %v", s.name, err)
	}
	if code != 200 {
		t.Fatalf("%s GET /api/node: status %d body %s", s.name, code, truncate(string(body), 500))
	}
	// The handler returns a bare array of {name, host, port, cluster_name, groups}.
	var raw struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	var list []struct {
		Name string `json:"name"`
	}
	out := map[string]bool{}
	if err := json.Unmarshal(body, &list); err == nil && len(list) > 0 {
		for _, n := range list {
			out[n.Name] = true
		}
		return out
	}
	// Tolerate a {"data": [...]} envelope so this helper does not become the
	// reason the suite breaks if the handler is wrapped later.
	if err := json.Unmarshal(body, &raw); err == nil {
		for _, n := range raw.Data {
			out[n.Name] = true
		}
		return out
	}
	t.Fatalf("%s GET /api/node: cannot parse body %s", s.name, truncate(string(body), 500))
	return nil
}

// waitConverged blocks until every end node sees every other end node in its
// directory. This is what the node-directory digest work is about, and it is what
// GET /api/node reports.
//
// NOTE: knowing a peer is NOT the same as being able to fan out to it. Use
// waitFanoutReady before asserting on any cross-node API call.
func (c *e2eCluster) waitConverged(t *testing.T, timeout time.Duration) {
	t.Helper()

	want := c.nodeNames()
	deadline := time.Now().Add(timeout)
	var missing map[string][]string

	for time.Now().Before(deadline) {
		missing = map[string][]string{}
		for _, s := range c.ends {
			known := s.knownNodes(t)
			for _, w := range want {
				if !known[w] {
					missing[s.name] = append(missing[s.name], w)
				}
			}
		}
		if len(missing) == 0 {
			t.Logf("cluster converged: %d nodes each see %v", len(c.ends), want)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	for node, miss := range missing {
		t.Errorf("node %s still missing %v", node, miss)
	}
	for _, s := range c.ends {
		t.Logf("%s logs:\n%s", s.name, truncate(s.logs.String(), 4000))
	}
	t.Fatalf("cluster did not converge within %s (want %v)", timeout, want)
}

// waitFanoutReady blocks until every node can actually REACH every other node.
//
// Directory convergence is not enough: ReqToMultiEndNodeServer silently skips any
// peer for which RegisteredRemote() is false (no out-token yet, or no successful
// report within NodeTimeOut), and skipped peers are not even reported in the
// failure list. A node can therefore appear in GET /api/node while a
// "target=all" call still reaches only the local node — which would make every
// fan-out assertion in the suite quietly test nothing.
//
// GET /api/status?target=all is the cheapest honest probe: it fans out over the
// exact same path the mutating endpoints use, and returns one entry per node it
// actually reached.
func (c *e2eCluster) waitFanoutReady(t *testing.T, timeout time.Duration) {
	t.Helper()

	want := c.nodeNames()
	deadline := time.Now().Add(timeout)
	var shortfall map[string][]string

	for time.Now().Before(deadline) {
		shortfall = map[string][]string{}
		for _, s := range c.ends {
			code, body, err := s.apiGet("/api/status", url.Values{"target": {"all"}})
			if err != nil || code != 200 {
				shortfall[s.name] = want
				continue
			}
			for _, w := range want {
				if !strings.Contains(string(body), `"node_name":"`+w+`"`) {
					shortfall[s.name] = append(shortfall[s.name], w)
				}
			}
		}
		if len(shortfall) == 0 {
			t.Logf("cluster fan-out ready: every node reaches %v", want)
			return
		}
		time.Sleep(300 * time.Millisecond)
	}

	for node, miss := range shortfall {
		t.Errorf("node %s cannot fan out to %v", node, miss)
	}
	t.Fatalf("cluster fan-out not ready within %s (mutual registration incomplete)", timeout)
}

// restartWithoutStaticPeers stops a node, strips end_node.cluster.static_nodes
// from its config, and starts it again on the same data directory.
//
// This is the test lever for the persisted directory. With no static peers and no
// center, a restarted node has exactly one way to know whom to dial: the rows it
// wrote to its own database before going down. If it converges again, that path
// worked.
func (c *e2eCluster) restartWithoutStaticPeers(t *testing.T, s *e2eServer) {
	t.Helper()

	s.shutdown()

	raw, err := os.ReadFile(s.cfgPath)
	if err != nil {
		t.Fatalf("read %s config: %v", s.name, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s config: %v", s.name, err)
	}
	endNode, _ := doc["end_node"].(map[string]any)
	if endNode == nil {
		t.Fatalf("%s config has no end_node section", s.name)
	}
	cl, _ := endNode["cluster"].(map[string]any)
	if cl == nil {
		t.Fatalf("%s config has no end_node.cluster section", s.name)
	}
	if _, had := cl["static_nodes"]; !had {
		t.Fatalf("%s had no static_nodes to strip; the test would prove nothing", s.name)
	}
	delete(cl, "static_nodes")

	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("re-marshal %s config: %v", s.name, err)
	}
	if err := os.WriteFile(s.cfgPath, out, 0o644); err != nil {
		t.Fatalf("rewrite %s config: %v", s.name, err)
	}

	logs := &syncBuffer{}
	cmd := exec.Command(c.bin, "server", "--conf", s.cfgPath)
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("restart %s: %v", s.name, err)
	}
	s.cmd = cmd
	s.logs = logs
	s.stopped = false
	t.Cleanup(func() {
		s.shutdown()
		if t.Failed() {
			t.Logf("%s logs after restart:\n%s", s.name, truncate(logs.String(), 8000))
		}
	})

	if err := s.waitReady(60 * time.Second); err != nil {
		t.Fatalf("%s did not come back: %v\nlogs:\n%s", s.name, err, truncate(logs.String(), 6000))
	}
	t.Logf("%s restarted with no static peers", s.name)
}
