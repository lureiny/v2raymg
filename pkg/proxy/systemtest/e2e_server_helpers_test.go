//go:build integration

package systemtest

// e2e_server_helpers_test.go — infrastructure for TestE2EAllContainersProtocolMatrix.
//
// Contains:
//   - ensureV2raymgBin   — build or locate the v2raymg binary
//   - e2eServer          — subprocess lifecycle, HTTP API wrappers, subscription helpers
//   - generateE2EConfig  — write a minimal all-containers YAML config
//   - uriToClashProxy    — parse subscription URIs via the project's codec package
//                          and convert to clash proxy maps for spawnMihomoClient

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

// shutdownSignal is the OS signal sent to stop the v2raymg server subprocess.
var shutdownSignal = syscall.SIGTERM

// ---------------------------------------------------------------------------
// Binary management
// ---------------------------------------------------------------------------

var (
	v2raymgBinOnce sync.Once
	v2raymgBinPath string
	v2raymgBinErr  error
)

// ensureV2raymgBin returns a path to a usable v2raymg binary.
// Resolution order:
//  1. V2RAYMG_BIN env var — used as-is if the file exists.
//  2. Otherwise: `go build -o <tmpDir>/v2raymg .` from the module root.
func ensureV2raymgBin(t *testing.T, tmpDir string) string {
	t.Helper()
	if env := os.Getenv("V2RAYMG_BIN"); env != "" {
		if _, err := os.Stat(env); err != nil {
			t.Fatalf("V2RAYMG_BIN=%s set but not usable: %v", env, err)
		}
		return env
	}

	v2raymgBinOnce.Do(func() {
		// Locate module root relative to this test file's package directory.
		// The module root is 3 levels up from pkg/proxy/systemtest.
		moduleRoot, err := findModuleRoot()
		if err != nil {
			v2raymgBinErr = fmt.Errorf("locate module root: %w", err)
			return
		}
		binPath := filepath.Join(tmpDir, "v2raymg")
		cmd := exec.Command("go", "build", "-o", binPath, ".")
		cmd.Dir = moduleRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			v2raymgBinErr = fmt.Errorf("go build: %w\n%s", err, out)
			return
		}
		v2raymgBinPath = binPath
	})

	if v2raymgBinErr != nil {
		t.Fatalf("ensureV2raymgBin: %v", v2raymgBinErr)
	}
	return v2raymgBinPath
}

// findModuleRoot walks up from this source file's compiled path looking for go.mod.
func findModuleRoot() (string, error) {
	// Use go env GOMOD as the authoritative source.
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", err
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		return "", fmt.Errorf("no go.mod found (not in a module)")
	}
	return filepath.Dir(gomod), nil
}

// ---------------------------------------------------------------------------
// Config generation
// ---------------------------------------------------------------------------

// e2eServerOpts holds the runtime parameters for a test v2raymg instance.
// Zero values for optional fields mean "disabled" or "auto-detect".
type e2eServerOpts struct {
	Dir      string // temp dir for configs, DB, logs
	HttpPort int    // HTTP management API port
	RpcPort  int    // gRPC cluster port (required for server start, not used in tests)

	// Xray container — required
	XrayBin      string // path to xray binary; empty → auto_download
	XrayGRPCPort int    // xray gRPC API port
	XrayConfFile string // path for xray's own JSON config

	// Mihomo container — required
	MihomoBin     string // path to mihomo binary; empty → auto_download
	MihomoAPIPort int    // mihomo REST external-controller port
	MihomoDataDir string // mihomo home directory (SAFE_PATHS root)
	MihomoConf    string // path for mihomo's own YAML config

	// Hysteria container — optional; disabled when HystBin == ""
	HystBin     string // path to hysteria binary
	HystUDPPort int    // hysteria server UDP listen port
	HystConf    string // path for hysteria's config file
	HystCert    string // TLS cert for hysteria (from writeTempCert)
	HystKey     string // TLS key for hysteria

	// Snell container — optional; disabled when SnellBin == ""
	SnellBin  string // path to snell-server binary
	SnellPort int    // snell server TCP port
	SnellConf string // path for snell's config file

	// Forward port pool (allocated to users)
	FwdMinPort int
	FwdMaxPort int
}

// generateE2EConfig writes the v2raymg YAML config file and returns its path.
// All containers are written; containers whose binary path is empty get
// enabled=false so the server skips them cleanly.
func generateE2EConfig(t *testing.T, opts e2eServerOpts) string {
	t.Helper()

	// ---- build container list ----
	type containerEntry struct {
		Type    string         `yaml:"type"`
		Enabled bool           `yaml:"enabled"`
		Config  map[string]any `yaml:"config"`
	}

	containers := []containerEntry{
		{
			Type:    "xray",
			Enabled: true,
			Config: map[string]any{
				"binary_path":    opts.XrayBin,
				"config_file":    opts.XrayConfFile,
				"grpc_port":      opts.XrayGRPCPort,
				"auto_download":  opts.XrayBin == "",
				"debug":          false,
			},
		},
		{
			Type:    "mihomo",
			Enabled: true,
			Config: func() map[string]any {
				m := map[string]any{
					"config_file_path":    opts.MihomoConf,
					"data_dir":            opts.MihomoDataDir,
					"external_controller": fmt.Sprintf("127.0.0.1:%d", opts.MihomoAPIPort),
					"secret":              "",
					"release_tag":         "Prerelease-Alpha",
					"auto_download":       opts.MihomoBin == "",
				}
				// Only set binary_path when non-empty; empty overrides the
				// default path (/usr/local/bin/mihomo) and breaks validation.
				if opts.MihomoBin != "" {
					m["binary_path"] = opts.MihomoBin
				}
				return m
			}(),
		},
	}

	if opts.HystBin != "" {
		containers = append(containers, containerEntry{
			Type:    "hysteria",
			Enabled: true,
			Config: map[string]any{
				"binary_path":          opts.HystBin,
				"config_file_path":     opts.HystConf,
				"listen":               "127.0.0.1",
				"port":                 opts.HystUDPPort,
				"domain":               "",
				"cert_file":            opts.HystCert,
				"key_file":             opts.HystKey,
				"traffic_stats_port":   0,
				"traffic_stats_secret": "",
				"auto_download":        false,
			},
		})
	}

	if opts.SnellBin != "" {
		containers = append(containers, containerEntry{
			Type:    "snell",
			Enabled: true,
			Config: map[string]any{
				"binary_path":      opts.SnellBin,
				"config_file_path": opts.SnellConf,
				"listen":           "127.0.0.1",
				"port":             opts.SnellPort,
				"psk":              "auto",
				"auto_download":    false,
			},
		})
	}

	fwdMin := opts.FwdMinPort
	if fwdMin == 0 {
		fwdMin = 40000
	}
	fwdMax := opts.FwdMaxPort
	if fwdMax == 0 {
		fwdMax = 50000
	}

	cfg := map[string]any{
		"node_type": "end",
		"log": map[string]any{
			"level":  "info",
			"format": "text",
			"output": "stdout",
		},
		"store": map[string]any{
			"dsn": filepath.Join(opts.Dir, "data.db"),
		},
		"forward": map[string]any{
			"min_port":   fwdMin,
			"max_port":   fwdMax,
			"use_random": true,
		},
		"containers": map[string]any{
			"containers": containers,
		},
		"end_node": map[string]any{
			"name":              "e2e-test",
			"proxy_host":        "127.0.0.1",
			"listen":            "127.0.0.1",
			"http_listen":       "127.0.0.1",
			"rpc_port":          opts.RpcPort,
			"http_port":         opts.HttpPort,
			"http_token":        e2eHTTPToken,
			"jwt_secret":        "e2e-jwt-secret-this-is-32-chars!",
			"jwt_expire_hours":  24,
			"enable_prometheus": false,
			"only_gateway":      false,
		},
		"cluster_user": map[string]any{
			"enabled": false,
		},
	}

	body, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal e2e config: %v", err)
	}

	configPath := filepath.Join(opts.Dir, "config.yaml")
	if err := os.WriteFile(configPath, body, 0o644); err != nil {
		t.Fatalf("write e2e config: %v", err)
	}
	return configPath
}

// e2eHTTPToken is the X-Token used for all HTTP API calls in E2E tests.
const e2eHTTPToken = "e2e-test-token-static"

// ---------------------------------------------------------------------------
// Server lifecycle
// ---------------------------------------------------------------------------

// e2eServer manages a v2raymg subprocess for integration testing.
type e2eServer struct {
	cmd     *exec.Cmd
	baseURL string // http://127.0.0.1:<httpPort>
	token   string // X-Token header value
	dir     string // temp dir (configs, DB)

	mu      sync.Mutex
	stopped bool // SIGTERM already sent (for idempotent shutdown)
}

// startE2EServer allocates ports, writes config, starts the server subprocess,
// and waits until it is accepting HTTP requests.
func startE2EServer(t *testing.T, xrayBin, mihomoBin, hystBin, snellBin, tmpDir string) *e2eServer {
	t.Helper()

	dir := filepath.Join(tmpDir, "server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir server dir: %v", err)
	}

	httpPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort http: %v", err)
	}
	rpcPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort rpc: %v", err)
	}
	xrayGRPCPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort xray grpc: %v", err)
	}
	mihomoAPIPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort mihomo api: %v", err)
	}

	mihomoDataDir := filepath.Join(dir, "mihomo-data")
	if err := os.MkdirAll(mihomoDataDir, 0o755); err != nil {
		t.Fatalf("mkdir mihomo data: %v", err)
	}

	opts := e2eServerOpts{
		Dir:           dir,
		HttpPort:      httpPort,
		RpcPort:       rpcPort,
		XrayBin:       xrayBin,
		XrayGRPCPort:  xrayGRPCPort,
		XrayConfFile:  filepath.Join(dir, "xray.json"),
		MihomoBin:     mihomoBin,
		MihomoAPIPort: mihomoAPIPort,
		MihomoDataDir: mihomoDataDir,
		MihomoConf:    filepath.Join(dir, "mihomo.yaml"),
	}

	if hystBin != "" {
		hystCert, hystKey, _, _, err := writeTempCert(dir)
		if err != nil {
			t.Fatalf("writeTempCert for hysteria: %v", err)
		}
		hystUDPPort := freeUDPPort(t)
		opts.HystBin = hystBin
		opts.HystConf = filepath.Join(dir, "hysteria.yaml")
		opts.HystUDPPort = hystUDPPort
		opts.HystCert = hystCert
		opts.HystKey = hystKey
	}

	if snellBin != "" {
		snellPort, err := freeTCPPort()
		if err != nil {
			t.Fatalf("freeTCPPort snell: %v", err)
		}
		opts.SnellBin = snellBin
		opts.SnellConf = filepath.Join(dir, "snell.conf")
		opts.SnellPort = snellPort
	}

	v2raymgBin := ensureV2raymgBin(t, tmpDir)
	configPath := generateE2EConfig(t, opts)

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(v2raymgBin, "server", "--conf", configPath)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start v2raymg server: %v", err)
	}

	srv := &e2eServer{
		cmd:     cmd,
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", httpPort),
		token:   e2eHTTPToken,
		dir:     dir,
	}

	t.Cleanup(func() {
		srv.shutdown()
		// Log server output on failure for diagnostics.
		if t.Failed() {
			if out := stdout.String(); out != "" {
				t.Logf("v2raymg stdout:\n%s", truncate(out, 8000))
			}
			if out := stderr.String(); out != "" {
				t.Logf("v2raymg stderr:\n%s", truncate(out, 8000))
			}
		}
	})

	if err := srv.waitReady(60 * time.Second); err != nil {
		t.Fatalf("v2raymg server never became ready: %v\nstderr: %s", err, stderr.String())
	}
	t.Logf("v2raymg server ready at %s", srv.baseURL)
	return srv
}

// waitReady polls GET /api/status until it returns 200 or the timeout fires.
func (s *e2eServer) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, s.baseURL+"/api/status", nil)
		req.Header.Set("X-Token", s.token)
		resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if err == nil {
			resp.Body.Close()
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("server not ready after %s", timeout)
}

// shutdown sends SIGTERM to the server process. Idempotent: safe to call multiple times.
func (s *e2eServer) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(shutdownSignal)
		_ = s.cmd.Wait()
	}
}

// ---------------------------------------------------------------------------
// HTTP API wrappers
// ---------------------------------------------------------------------------

// apiPost sends an authenticated POST request and returns the response body.
func (s *e2eServer) apiPost(path string, body any) (int, []byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, s.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", s.token)

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}

// apiGet sends an authenticated GET request.
func (s *e2eServer) apiGet(path string, params url.Values) (int, []byte, error) {
	u := s.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Token", s.token)

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

// addUser creates the given user on the server.
func (s *e2eServer) addUser(username, password string) error {
	status, body, err := s.apiPost("/api/user", map[string]any{
		"user": username,
		"pwd":  password,
	})
	if err != nil {
		return fmt.Errorf("POST /api/user: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("POST /api/user: status=%d body=%s", status, body)
	}
	return nil
}

// fastAddInbound calls POST /api/inbound/fast with the given parameters.
// extra is merged into the request body alongside tag, container, protocol, etc.
func (s *e2eServer) fastAddInbound(tag, container, protocol, transport, security string, extra map[string]any) error {
	body := make(map[string]any, len(extra)+6)
	for k, v := range extra {
		body[k] = v
	}
	body["tag"] = tag
	body["protocol"] = protocol
	if container != "" {
		body["container"] = container
	}
	if transport != "" {
		body["transport"] = transport
	}
	if security != "" {
		body["security"] = security
	}

	status, respBody, err := s.apiPost("/api/inbound/fast", body)
	if err != nil {
		return fmt.Errorf("POST /api/inbound/fast: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("POST /api/inbound/fast: status=%d body=%s", status, respBody)
	}
	return nil
}

// getRawSub fetches the common-format subscription for the given user and
// returns the decoded list of individual proxy URIs. Returns an error if the
// response indicates auth failure or server error.
func (s *e2eServer) getRawSub(username, password string) ([]string, error) {
	params := url.Values{
		"user": []string{username},
		"pwd":  []string{password},
	}
	status, body, err := s.apiGet("/sub", params)
	if err != nil {
		return nil, fmt.Errorf("GET /api/sub: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET /api/sub: status=%d", status)
	}
	text := strings.TrimSpace(string(body))
	if text == "invalid user" || text == "no avaliable node" {
		return nil, fmt.Errorf("GET /sub: %s", text)
	}
	// Empty body is valid when no inbounds are configured yet (baseline state).
	if text == "" {
		return nil, nil
	}
	// Decode base64 envelope.
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		// Some builds omit padding — try with RawStdEncoding.
		decoded, err = base64.RawStdEncoding.DecodeString(text)
		if err != nil {
			return nil, fmt.Errorf("base64 decode subscription: %w (raw=%q)", err, truncate(text, 100))
		}
	}
	var uris []string
	for _, line := range strings.Split(string(decoded), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			uris = append(uris, line)
		}
	}
	return uris, nil
}

// sliceToSet converts a string slice to a set map for O(1) lookup.
func sliceToSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

// getNewURI polls getRawSub until a URI appears that is not in baseline,
// or the timeout fires. Returns the first new URI found.
func (s *e2eServer) getNewURI(username, password string, baseline map[string]struct{}, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		uris, err := s.getRawSub(username, password)
		if err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		for _, u := range uris {
			if _, inBaseline := baseline[u]; !inBaseline {
				return u, nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", fmt.Errorf("no new URI appeared after %s", timeout)
}

// findURIsByScheme returns all URIs in the list whose scheme prefix matches.
func findURIsByScheme(uris []string, scheme string) []string {
	prefix := scheme + "://"
	var found []string
	for _, u := range uris {
		if strings.HasPrefix(u, prefix) {
			found = append(found, u)
		}
	}
	return found
}

// ---------------------------------------------------------------------------
// URI → clash proxy map conversion
// ---------------------------------------------------------------------------

// uriToClashProxy decodes a subscription URI using the project's codec package
// and converts the resulting Node to a clash-style proxy map for spawnMihomoClient.
func uriToClashProxy(uri string) (map[string]any, error) {
	node, err := codec.Decode(uri)
	if err != nil {
		return nil, fmt.Errorf("decode URI %q: %w", uri, err)
	}
	switch n := node.(type) {
	case *codec.VLessNode:
		return vlessNodeToProxy(n), nil
	case *codec.VMessNode:
		return vmessNodeToProxy(n), nil
	case *codec.TrojanNode:
		return trojanNodeToProxy(n), nil
	case *codec.ShadowsocksNode:
		return ssNodeToProxy(n), nil
	case *codec.Hysteria2Node:
		return hy2NodeToProxy(n), nil
	case *codec.TuicNode:
		return tuicNodeToProxy(n), nil
	case *codec.AnyTLSNode:
		return anytlsNodeToProxy(n), nil
	case *codec.SnellNode:
		return snellNodeToProxy(n), nil
	default:
		return nil, fmt.Errorf("uriToClashProxy: unsupported node type %T", node)
	}
}

func vlessNodeToProxy(n *codec.VLessNode) map[string]any {
	p := map[string]any{
		"name":   n.NodeName,
		"type":   "vless",
		"server": n.Host,
		"port":   n.Port,
		"uuid":   n.UUID,
		"udp":    true,
	}
	switch n.Security {
	case "tls":
		p["tls"] = true
		if n.SNI != "" {
			p["servername"] = n.SNI
		}
		if n.SkipCertVerify {
			p["skip-cert-verify"] = true
		}
		if n.Fingerprint != "" {
			p["client-fingerprint"] = n.Fingerprint
		}
	case "reality":
		p["tls"] = true
		if n.SNI != "" {
			p["servername"] = n.SNI
		}
		p["reality-opts"] = map[string]any{
			"public-key": n.RealityPublicKey,
			"short-id":   n.RealityShortID,
		}
		p["client-fingerprint"] = func() string {
			if n.Fingerprint != "" {
				return n.Fingerprint
			}
			return "chrome"
		}()
	}
	if n.Flow != "" {
		p["flow"] = n.Flow
	}
	applyTransportVLess(p, n)
	return p
}

func applyTransportVLess(p map[string]any, n *codec.VLessNode) {
	switch n.Transport {
	case "ws":
		p["network"] = "ws"
		opts := map[string]any{}
		if n.WSPath != "" {
			opts["path"] = n.WSPath
		}
		if n.WSHost != "" {
			opts["headers"] = map[string]any{"Host": n.WSHost}
		}
		p["ws-opts"] = opts
	case "grpc":
		p["network"] = "grpc"
		p["grpc-opts"] = map[string]any{
			"grpc-service-name": n.GRPCServiceName,
		}
	case "xhttp", "splithttp":
		p["network"] = "xhttp"
		xh := map[string]any{}
		if n.XHTTPPath != "" {
			xh["path"] = n.XHTTPPath
		}
		if n.XHTTPMode != "" {
			xh["mode"] = n.XHTTPMode
		}
		p["xhttp-opts"] = xh
	case "httpupgrade":
		p["network"] = "httpupgrade"
		// httpupgrade-opts not encoded in codec VLessNode; minimal config
	}
}

func vmessNodeToProxy(n *codec.VMessNode) map[string]any {
	p := map[string]any{
		"name":     n.NodeName,
		"type":     "vmess",
		"server":   n.Host,
		"port":     n.Port,
		"uuid":     n.UUID,
		"alterId":  n.AlterId,
		"cipher":   "auto",
		"udp":      true,
	}
	if n.Security == "tls" {
		p["tls"] = true
		if n.SNI != "" {
			p["servername"] = n.SNI
		}
		if n.SkipCertVerify {
			p["skip-cert-verify"] = true
		}
		if n.Fingerprint != "" {
			p["client-fingerprint"] = n.Fingerprint
		}
	}
	switch n.Transport {
	case "ws":
		p["network"] = "ws"
		opts := map[string]any{}
		if n.WSPath != "" {
			opts["path"] = n.WSPath
		}
		if n.WSHost != "" {
			opts["headers"] = map[string]any{"Host": n.WSHost}
		}
		p["ws-opts"] = opts
	case "grpc":
		p["network"] = "grpc"
		p["grpc-opts"] = map[string]any{
			"grpc-service-name": n.GRPCServiceName,
		}
	case "h2":
		p["network"] = "h2"
		h2opts := map[string]any{}
		if n.H2Path != "" {
			h2opts["path"] = n.H2Path
		}
		if n.H2Host != "" {
			h2opts["host"] = []string{n.H2Host}
		}
		p["h2-opts"] = h2opts
	case "xhttp", "splithttp":
		p["network"] = "xhttp"
		xh := map[string]any{}
		if n.XHTTPPath != "" {
			xh["path"] = n.XHTTPPath
		}
		p["xhttp-opts"] = xh
	}
	return p
}

func trojanNodeToProxy(n *codec.TrojanNode) map[string]any {
	p := map[string]any{
		"name":     n.NodeName,
		"type":     "trojan",
		"server":   n.Host,
		"port":     n.Port,
		"password": n.Password,
		"udp":      true,
	}
	// Trojan always uses TLS at the wire level.
	if n.SNI != "" {
		p["sni"] = n.SNI
	}
	if n.SkipCertVerify {
		p["skip-cert-verify"] = true
	}
	if n.Fingerprint != "" {
		p["client-fingerprint"] = n.Fingerprint
	}
	if n.Security == "reality" {
		p["reality-opts"] = map[string]any{
			"public-key": n.RealityPublicKey,
			"short-id":   n.RealityShortID,
		}
	}
	switch n.Transport {
	case "ws":
		p["network"] = "ws"
		opts := map[string]any{}
		if n.WSPath != "" {
			opts["path"] = n.WSPath
		}
		if n.WSHost != "" {
			opts["headers"] = map[string]any{"Host": n.WSHost}
		}
		p["ws-opts"] = opts
	case "grpc":
		p["network"] = "grpc"
		p["grpc-opts"] = map[string]any{
			"grpc-service-name": n.GRPCServiceName,
		}
	}
	return p
}

func ssNodeToProxy(n *codec.ShadowsocksNode) map[string]any {
	p := map[string]any{
		"name":     n.NodeName,
		"type":     "ss",
		"server":   n.Host,
		"port":     n.Port,
		"cipher":   n.Method,
		"password": n.Password,
		"udp":      true,
	}
	return p
}

func hy2NodeToProxy(n *codec.Hysteria2Node) map[string]any {
	p := map[string]any{
		"name":     n.NodeName,
		"type":     "hysteria2",
		"server":   n.Host,
		"port":     n.Port,
		"password": n.Password,
		"udp":      true,
		"alpn":     []string{"h3"},
	}
	if n.SNI != "" {
		p["sni"] = n.SNI
	}
	if n.SkipCertVerify {
		p["skip-cert-verify"] = true
	}
	if n.Obfs != "" {
		p["obfs"] = n.Obfs
		if n.ObfsPassword != "" {
			p["obfs-password"] = n.ObfsPassword
		}
	}
	if n.Up != "" {
		p["up"] = n.Up
	}
	if n.Down != "" {
		p["down"] = n.Down
	}
	if len(n.ALPN) > 0 {
		p["alpn"] = n.ALPN
	}
	return p
}

func tuicNodeToProxy(n *codec.TuicNode) map[string]any {
	p := map[string]any{
		"name":     n.NodeName,
		"type":     "tuic",
		"server":   n.Host,
		"port":     n.Port,
		"uuid":     n.UUID,
		"password": n.Password,
		"udp":      true,
	}
	if n.SNI != "" {
		p["sni"] = n.SNI
	}
	if n.SkipCertVerify {
		p["skip-cert-verify"] = true
	}
	if len(n.ALPN) > 0 {
		p["alpn"] = n.ALPN
	} else {
		p["alpn"] = []string{"h3"}
	}
	if n.CongestionControl != "" {
		p["congestion-controller"] = n.CongestionControl
	}
	if n.UDPRelayMode != "" {
		p["udp-relay-mode"] = n.UDPRelayMode
	}
	return p
}

func anytlsNodeToProxy(n *codec.AnyTLSNode) map[string]any {
	p := map[string]any{
		"name":     n.NodeName,
		"type":     "anytls",
		"server":   n.Host,
		"port":     n.Port,
		"password": n.Password,
	}
	if n.SNI != "" {
		p["sni"] = n.SNI
	}
	if n.SkipCertVerify {
		p["skip-cert-verify"] = true
	}
	if n.Fingerprint != "" {
		p["fingerprint"] = n.Fingerprint
	}
	return p
}

func snellNodeToProxy(n *codec.SnellNode) map[string]any {
	p := map[string]any{
		"name":    n.NodeName,
		"type":    "snell",
		"server":  n.Host,
		"port":    n.Port,
		"psk":     n.PSK,
		"version": n.Version,
	}
	if n.Obfs != "" {
		p["obfs-opts"] = map[string]any{
			"mode": n.Obfs,
			"host": n.ObfsHost,
		}
	}
	return p
}

