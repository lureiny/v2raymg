//go:build integration

package systemtest

// cluster_e2e_helpers_test.go — orchestration for the multi-node cluster suite.
//
// Everything here builds on the single-node helpers in e2e_server_helpers_test.go
// (ensureV2raymgBin, generateE2EConfig, the e2eServer HTTP wrappers). The pieces
// that are genuinely different for a cluster:
//
//   - a CENTER node is a different kind of process. cmd.runCenterNode starts only
//     the gRPC directory server — no HTTP, no store, no containers — so it cannot
//     be probed with waitReady (/api/status does not exist) and does not fit the
//     e2eServer HTTP shape at all.
//   - end nodes must share a cluster name + token, and each needs a unique node
//     name and its own ports/data dir.
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
// Center node
// ---------------------------------------------------------------------------

// e2eCenter is the center-node process. Deliberately NOT an e2eServer: a center
// exposes no HTTP API, so every e2eServer method would be a trap.
type e2eCenter struct {
	cmd  *exec.Cmd
	port int
	logs *syncBuffer

	mu      sync.Mutex
	stopped bool
}

func (c *e2eCenter) addr() string { return fmt.Sprintf("127.0.0.1:%d", c.port) }

func (c *e2eCenter) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || c.cmd.Process == nil {
		return
	}
	c.stopped = true
	_ = c.cmd.Process.Signal(shutdownSignal)
	_ = c.cmd.Wait()
}

// ---------------------------------------------------------------------------
// Cluster
// ---------------------------------------------------------------------------

type clusterOpts struct {
	// HeartbeatIntervalSec drives every cluster timing assertion. Tests use 1 so a
	// "converges within a couple of rounds" assertion takes seconds; production
	// default is 10.
	HeartbeatIntervalSec int
	// CenterToken, when set, wraps the end->center channel in an AES envelope on
	// both sides.
	CenterToken string
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
	center *e2eCenter
	ends   []*e2eServer
}

// startE2ECluster compiles the binary and brings up the center node. End nodes are
// added one at a time with addEndNode, which is what the cluster-discovery
// assertions depend on.
func startE2ECluster(t *testing.T, tmpDir string, opts clusterOpts) *e2eCluster {
	t.Helper()

	if opts.HeartbeatIntervalSec == 0 {
		opts.HeartbeatIntervalSec = 1
	}

	c := &e2eCluster{
		t:      t,
		tmpDir: tmpDir,
		bin:    ensureV2raymgBin(t, tmpDir),
		opts:   opts,
	}

	dir := filepath.Join(tmpDir, "center")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir center dir: %v", err)
	}
	port, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort center rpc: %v", err)
	}

	configPath := generateE2EConfig(t, e2eServerOpts{
		Dir:      dir,
		NodeType: "center",
		NodeName: "e2e-center",
		RpcPort:  port,
		LogLevel: "debug",
		// A center serves potentially many clusters; only listed ones are accepted.
		ClusterTokens: map[string]string{e2eClusterName: e2eClusterToken},
		CenterToken:   opts.CenterToken,
	})

	logs := &syncBuffer{}
	cmd := exec.Command(c.bin, "server", "--conf", configPath)
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start center node: %v", err)
	}
	c.center = &e2eCenter{cmd: cmd, port: port, logs: logs}

	t.Cleanup(func() {
		c.center.shutdown()
		if t.Failed() {
			t.Logf("center logs:\n%s", truncate(logs.String(), 8000))
		}
	})

	// A center has no HTTP endpoint to poll, so readiness is "the gRPC port
	// accepts a connection".
	if err := waitPort(c.center.addr(), 30*time.Second); err != nil {
		t.Fatalf("center never listened on %s: %v\nlogs:\n%s",
			c.center.addr(), err, truncate(logs.String(), 4000))
	}
	t.Logf("center ready at %s", c.center.addr())
	return c
}

// addEndNode starts one more end node pointed at the center and waits until it
// serves HTTP. It does NOT wait for cluster convergence — callers assert that
// explicitly with waitConverged so the discovery path is actually under test.
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
	fwdBase := 41000 + idx*1000

	configPath := generateE2EConfig(t, e2eServerOpts{
		Dir:                  dir,
		NodeName:             name,
		HttpPort:             httpPort,
		RpcPort:              rpcPort,
		ClusterName:          e2eClusterName,
		ClusterToken:         e2eClusterToken,
		// The node-directory reconcile logs at debug; without this the
		// "reconciles happen, then stop" assertion could never see the line and
		// would pass vacuously.
		LogLevel: "debug",
		CenterToken:          c.opts.CenterToken,
		CenterHost:           "127.0.0.1",
		CenterPort:           c.center.port,
		HeartbeatIntervalSec: c.opts.HeartbeatIntervalSec,
		// Node-groups routes are only registered when the cluster-user layer is on,
		// so the suite cannot cover them otherwise.
		ClusterUserEnabled: true,
		EnablePrometheus:   true,
		NodeSumSync:        c.opts.NodeSumSync,
		XrayBin:              c.opts.XrayBin,
		XrayGRPCPort:         xrayGRPCPort,
		XrayConfFile:         filepath.Join(dir, "xray.json"),
		MihomoBin:            c.opts.MihomoBin,
		MihomoAPIPort:        mihomoAPIPort,
		MihomoDataDir:        mihomoDataDir,
		MihomoConf:           filepath.Join(dir, "mihomo.yaml"),
		FwdMinPort:           fwdBase,
		FwdMaxPort:           fwdBase + 800,
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
