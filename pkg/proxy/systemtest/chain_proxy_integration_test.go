//go:build integration

package systemtest

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lureiny/v2raymg/pkg/proxy/containers/mihomo"
	"github.com/lureiny/v2raymg/pkg/proxy/containers/xray"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

const (
	defaultXrayChainBin   = "/workspace/v2raymg/tmp/xray-bin/xray"
	defaultMihomoChainBin = "/workspace/ai-assistant-mcp/deployments/rfc-hk/mihomo/mihomo"
)

func resolveChainBinary(t *testing.T, envKey, fallback, label string) string {
	t.Helper()
	bin := strings.TrimSpace(os.Getenv(envKey))
	if bin == "" {
		bin = fallback
	}
	info, err := os.Stat(bin)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		t.Skipf("%s binary not available (%s=%q, err=%v)", label, envKey, bin, err)
	}
	return bin
}

// chainMemNodeGroupsStore provides an in-memory local_node_groups store for Layer 1 gating.
type chainMemNodeGroupsStore struct {
	mu     sync.RWMutex
	groups []string
}

func (s *chainMemNodeGroupsStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.groups))
	copy(out, s.groups)
	return out, nil
}

func (s *chainMemNodeGroupsStore) Set(groups []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(groups))
	copy(out, groups)
	s.groups = out
	return nil
}

// chainEchoServer is an in-process TCP echo server tracking accept hits.
type chainEchoServer struct {
	ln   net.Listener
	hits atomic.Int64

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed atomic.Bool
}

func newChainEchoServer(t *testing.T) *chainEchoServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &chainEchoServer{
		ln:    ln,
		conns: make(map[net.Conn]struct{}),
	}
	t.Cleanup(func() { _ = s.Close() })
	go s.serve()
	return s
}

func (s *chainEchoServer) Addr() string {
	return s.ln.Addr().String()
}

func (s *chainEchoServer) Port() uint16 {
	_, portStr, _ := net.SplitHostPort(s.ln.Addr().String())
	p, _ := strconv.Atoi(portStr)
	return uint16(p)
}

func (s *chainEchoServer) Hits() int64 {
	return s.hits.Load()
}

func (s *chainEchoServer) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	err := s.ln.Close()
	s.mu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.conns = make(map[net.Conn]struct{})
	s.mu.Unlock()
	return err
}

func (s *chainEchoServer) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.hits.Add(1)
		s.mu.Lock()
		if s.closed.Load() {
			s.mu.Unlock()
			_ = c.Close()
			return
		}
		s.conns[c] = struct{}{}
		s.mu.Unlock()

		go func(conn net.Conn) {
			defer func() {
				s.mu.Lock()
				delete(s.conns, conn)
				s.mu.Unlock()
				_ = conn.Close()
			}()
			_, _ = io.Copy(conn, conn)
		}(c)
	}
}

type chainSocksDialRecord struct {
	ATYP byte
	Host string
	Port uint16
	Err  string
}

// chainAuthSocks5Server implements RFC 1928 + RFC 1929 username/password SOCKS5
// and records exact ATYP/host/port requests without altering domain names.
type chainAuthSocks5Server struct {
	ln       net.Listener
	username string
	password string

	authSuccess atomic.Int64
	authFailure atomic.Int64

	mu     sync.Mutex
	dials  []chainSocksDialRecord
	conns  map[net.Conn]struct{}
	closed atomic.Bool
}

func newChainAuthSocks5Server(t *testing.T, username, password string) *chainAuthSocks5Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &chainAuthSocks5Server{
		ln:       ln,
		username: username,
		password: password,
		conns:    make(map[net.Conn]struct{}),
	}
	t.Cleanup(func() { _ = s.Close() })
	go s.serve()
	return s
}

func (s *chainAuthSocks5Server) Port() int {
	_, portStr, _ := net.SplitHostPort(s.ln.Addr().String())
	p, _ := strconv.Atoi(portStr)
	return p
}

func (s *chainAuthSocks5Server) AuthSuccess() int64 { return s.authSuccess.Load() }
func (s *chainAuthSocks5Server) AuthFailure() int64 { return s.authFailure.Load() }

func (s *chainAuthSocks5Server) DialCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.dials)
}

func (s *chainAuthSocks5Server) Records() []chainSocksDialRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]chainSocksDialRecord, len(s.dials))
	copy(out, s.dials)
	return out
}

func (s *chainAuthSocks5Server) HasDomainRecord(domain string, port uint16) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.dials {
		if r.ATYP == 0x03 && r.Host == domain && r.Port == port {
			return true
		}
	}
	return false
}

func (s *chainAuthSocks5Server) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	err := s.ln.Close()
	s.mu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.conns = make(map[net.Conn]struct{})
	s.mu.Unlock()
	return err
}

func (s *chainAuthSocks5Server) trackConn(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *chainAuthSocks5Server) untrackConn(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	_ = c.Close()
}

func (s *chainAuthSocks5Server) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		if !s.trackConn(c) {
			_ = c.Close()
			return
		}
		go s.handleConn(c)
	}
}

func (s *chainAuthSocks5Server) handleConn(c net.Conn) {
	defer s.untrackConn(c)
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))

	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil || hdr[0] != 0x05 {
		return
	}
	methods := make([]byte, int(hdr[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	supportsAuth := false
	for _, m := range methods {
		if m == 0x02 {
			supportsAuth = true
			break
		}
	}
	if !supportsAuth {
		s.authFailure.Add(1)
		_, _ = c.Write([]byte{0x05, 0xFF})
		return
	}
	if _, err := c.Write([]byte{0x05, 0x02}); err != nil {
		return
	}

	// RFC 1929 username/password subnegotiation.
	authHdr := make([]byte, 2)
	if _, err := io.ReadFull(c, authHdr); err != nil || authHdr[0] != 0x01 {
		s.authFailure.Add(1)
		return
	}
	uname := make([]byte, int(authHdr[1]))
	if _, err := io.ReadFull(c, uname); err != nil {
		s.authFailure.Add(1)
		return
	}
	plen := make([]byte, 1)
	if _, err := io.ReadFull(c, plen); err != nil {
		s.authFailure.Add(1)
		return
	}
	passwd := make([]byte, int(plen[0]))
	if _, err := io.ReadFull(c, passwd); err != nil {
		s.authFailure.Add(1)
		return
	}
	if string(uname) != s.username || string(passwd) != s.password {
		s.authFailure.Add(1)
		_, _ = c.Write([]byte{0x01, 0x01})
		return
	}
	s.authSuccess.Add(1)
	if _, err := c.Write([]byte{0x01, 0x00}); err != nil {
		return
	}

	reqHdr := make([]byte, 4)
	if _, err := io.ReadFull(c, reqHdr); err != nil || reqHdr[0] != 0x05 {
		return
	}
	if reqHdr[1] != 0x01 {
		_, _ = c.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	atyp := reqHdr[3]
	var host string
	switch atyp {
	case 0x01:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(c, ip); err != nil {
			return
		}
		host = net.IP(ip).String()
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return
		}
		dom := make([]byte, int(l[0]))
		if _, err := io.ReadFull(c, dom); err != nil {
			return
		}
		host = string(dom)
	case 0x04:
		ip := make([]byte, 16)
		if _, err := io.ReadFull(c, ip); err != nil {
			return
		}
		host = net.IP(ip).String()
	default:
		_, _ = c.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(c, portBuf); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBuf)

	rec := chainSocksDialRecord{ATYP: atyp, Host: host, Port: port}
	if s.closed.Load() {
		return
	}
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(int(port))), 3*time.Second)
	if err != nil {
		rec.Err = err.Error()
		s.mu.Lock()
		s.dials = append(s.dials, rec)
		s.mu.Unlock()
		_, _ = c.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	if !s.trackConn(upstream) {
		_ = upstream.Close()
		return
	}
	defer s.untrackConn(upstream)

	s.mu.Lock()
	s.dials = append(s.dials, rec)
	s.mu.Unlock()

	if _, err := c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	// Clear handshake deadline for persistent relayed streams.
	_ = c.SetDeadline(time.Time{})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(upstream, c)
		if tc, ok := upstream.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(c, upstream)
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	wg.Wait()
}

func dialSocks5Target(socksAddr, host string, port uint16) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", socksAddr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))

	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	authResp := make([]byte, 2)
	if _, err := io.ReadFull(conn, authResp); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 greeting read: %w", err)
	}
	if authResp[0] != 0x05 || authResp[1] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 auth rejected: %v", authResp)
	}

	req := make([]byte, 0, 7+len(host))
	req = append(req, 0x05, 0x01, 0x00)
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		req = append(req, 0x01)
		req = append(req, ip.To4()...)
	} else {
		if len(host) > 255 {
			_ = conn.Close()
			return nil, fmt.Errorf("domain too long: %s", host)
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, []byte(host)...)
	}
	req = binary.BigEndian.AppendUint16(req, port)
	if _, err := conn.Write(req); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 connect write: %w", err)
	}

	repHdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, repHdr); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 connect reply read: %w", err)
	}
	var discardLen int
	switch repHdr[3] {
	case 0x01:
		discardLen = 4 + 2
	case 0x03:
		dl := make([]byte, 1)
		if _, err := io.ReadFull(conn, dl); err != nil {
			_ = conn.Close()
			return nil, err
		}
		discardLen = int(dl[0]) + 2
	case 0x04:
		discardLen = 16 + 2
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 bad reply atyp: %d", repHdr[3])
	}
	if discardLen > 0 {
		if _, err := io.ReadFull(conn, make([]byte, discardLen)); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	if repHdr[1] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 connect failed rep=0x%02x", repHdr[1])
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func sendEchoViaSocks5(socksAddr, host string, port uint16, payload string) (string, error) {
	conn, err := dialSocks5Target(socksAddr, host, port)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))

	if _, err := conn.Write([]byte(payload)); err != nil {
		return "", fmt.Errorf("echo write: %w", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", fmt.Errorf("echo read: %w", err)
	}
	return string(buf), nil
}

func sendRemoteDNSProbeViaSocks5(socksAddr, domain string, port uint16) {
	conn, err := dialSocks5Target(socksAddr, domain, port)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = conn.Write([]byte("dns-probe\n"))
	buf := make([]byte, 64)
	_, _ = conn.Read(buf)
}

func writeChainXrayInitConfig(t *testing.T, cfgPath string, apiPort int) {
	t.Helper()
	cfg := fmt.Sprintf(`{
  "log": {"loglevel": "warning"},
  "stats": {},
  "api": {
    "tag": "api",
    "services": ["HandlerService", "LoggerService", "StatsService", "RoutingService"]
  },
  "policy": {
    "levels": {"0": {"statsUserUplink": true, "statsUserDownlink": true}},
    "system": {"statsInboundUplink": true, "statsInboundDownlink": true}
  },
  "inbounds": [{
    "tag": "api",
    "listen": "127.0.0.1",
    "port": %d,
    "protocol": "dokodemo-door",
    "settings": {"address": "127.0.0.1"}
  }],
  "outbounds": [{"tag": "direct", "protocol": "freedom"}],
  "routing": {
    "domainStrategy": "AsIs",
    "rules": [{"type": "field", "inboundTag": ["api"], "outboundTag": "api"}]
  }
}`, apiPort)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
}

func spawnXraySSClient(t *testing.T, dir, xrayBin, subURI string) string {
	t.Helper()
	ss, err := codec.DecodeShadowsocks(subURI)
	require.NoError(t, err)

	socksPort, err := freeTCPPort()
	require.NoError(t, err)
	cfgPath := filepath.Join(dir, fmt.Sprintf("xray-client-%d.json", socksPort))
	cfg := fmt.Sprintf(`{
  "log": {"loglevel": "warning"},
  "inbounds": [{
    "tag": "socks-in",
    "listen": "127.0.0.1",
    "port": %d,
    "protocol": "socks",
    "settings": {"auth": "noauth", "udp": true}
  }],
  "outbounds": [{
    "tag": "ss-out",
    "protocol": "shadowsocks",
    "settings": {
      "servers": [{
        "address": %q,
        "port": %d,
        "method": %q,
        "password": %q
      }]
    }
  }]
}`, socksPort, ss.Host, ss.Port, ss.Method, ss.Password)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	cmd := exec.Command(xrayBin, "run", "-c", cfgPath)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	addr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitPort(addr, 10*time.Second); err != nil {
		t.Fatalf("xray client failed to listen on %s: %v (stderr=%s)", addr, err, stderr.String())
	}
	return addr
}

func spawnMihomoSSClient(t *testing.T, dir, mihomoBin, subURI string) string {
	t.Helper()
	proxyCfg, err := uriToClashProxy(subURI)
	require.NoError(t, err)
	clientDataDir := filepath.Join(dir, "mihomo-client-data")
	require.NoError(t, os.MkdirAll(clientDataDir, 0o755))
	port := spawnMihomoClient(t, dir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    clientDataDir,
		Proxy:      proxyCfg,
	})
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func findSubBySuffix(specs []contracts.SubscriptionSpec, suffix string) (contracts.SubscriptionSpec, bool) {
	for _, s := range specs {
		if strings.HasSuffix(s.NodeName, suffix) {
			return s, true
		}
	}
	return contracts.SubscriptionSpec{}, false
}

func hasInboundConfig(inbs []inbound.Inbound, tag string) bool {
	for _, inb := range inbs {
		if inb.Tag() == tag {
			return true
		}
	}
	return false
}

type chainContainerFixture struct {
	ctype           contracts.ContainerType
	cont            container.Container
	subMgr          *subscription.Manager
	fm              forward.ForwardManager
	sm              *store.StoreManager
	um              *usermanager.UserManager
	socksClientFrom func(t *testing.T, subURI string) string
}

func runChainProxyRealTwoHopSuite(t *testing.T, fx chainContainerFixture, socksBackend *chainAuthSocks5Server, echoSrv *chainEchoServer) {
	t.Helper()

	directPort, err := freeTCPPort()
	require.NoError(t, err)
	chainPort, err := freeTCPPort()
	require.NoError(t, err)

	directTag := fmt.Sprintf("%s-direct-ss", fx.ctype)
	chainTag := fmt.Sprintf("%s-chain-ss", fx.ctype)

	methodKey := "method"
	extraDirect := map[string]any{"security": "none"}
	if fx.ctype == contracts.ContainerMihomo {
		methodKey = "cipher"
		extraDirect = map[string]any{}
	}

	directParams := map[string]any{
		"protocol": "shadowsocks",
		methodKey:  "2022-blake3-aes-256-gcm",
		"password": randBase64Key(32),
		"port":     directPort,
	}
	for k, v := range extraDirect {
		directParams[k] = v
	}
	require.NoError(t, fx.cont.FastAddInbound(directTag, directParams))

	chainURI := fmt.Sprintf("socks5://chainuser:chainpass@127.0.0.1:%d#Home-SH", socksBackend.Port())
	chainParams := map[string]any{
		"protocol":     "shadowsocks",
		methodKey:      "2022-blake3-aes-256-gcm",
		"password":     randBase64Key(32),
		"port":         chainPort,
		"inbound_type": string(contracts.InboundTypeChain),
		"chain_uri":    chainURI,
		"access_tags":  []string{"vip"},
	}
	for k, v := range extraDirect {
		chainParams[k] = v
	}
	require.NoError(t, fx.cont.FastAddInbound(chainTag, chainParams))

	require.NoError(t, waitPort(fmt.Sprintf("127.0.0.1:%d", directPort), 10*time.Second))
	require.NoError(t, waitPort(fmt.Sprintf("127.0.0.1:%d", chainPort), 10*time.Second))

	// Create 3 users testing Layer 1 + Layer 2 two-layer same-name tag isolation:
	// - carol: group="vip", inbound_tags=["vip"] -> sees BOTH direct and chain
	// - alice: group="vip", inbound_tags=[]      -> passes Layer 1, sees direct ONLY (no cross-layer leak from "vip" group!)
	// - bob:   group="other", inbound_tags=["vip"] -> fails Layer 1, sees NEITHER
	require.NoError(t, fx.um.AddUser(usermanager.AddUserRequest{
		Username:    "carol",
		Password:    "carol-pass",
		TargetGroup: "vip",
		InboundTags: []string{"vip"},
	}))
	carol, err := fx.um.GetUser("carol")
	require.NoError(t, err)

	require.NoError(t, fx.um.AddUser(usermanager.AddUserRequest{
		Username:    "alice",
		Password:    "alice-pass",
		TargetGroup: "vip",
		InboundTags: []string{},
	}))
	alice, err := fx.um.GetUser("alice")
	require.NoError(t, err)

	require.NoError(t, fx.um.AddUser(usermanager.AddUserRequest{
		Username:    "bob",
		Password:    "bob-pass",
		TargetGroup: "other",
		InboundTags: []string{"vip"},
	}))
	bob, err := fx.um.GetUser("bob")
	require.NoError(t, err)

	// === Property 3: Two-layer same-name tag isolation (Data Plane + User View + Admin View) ===
	var carolDirectFwd, carolChainFwd, aliceDirectFwd uint32
	eventually(t, "carol and alice initial forward ports allocated", 5*time.Second, func() bool {
		var ok1, ok2, ok3 bool
		carolDirectFwd, ok1 = fx.um.GetUserPortByDst("carol", uint32(directPort))
		carolChainFwd, ok2 = fx.um.GetUserPortByDst("carol", uint32(chainPort))
		aliceDirectFwd, ok3 = fx.um.GetUserPortByDst("alice", uint32(directPort))
		return ok1 && ok2 && ok3
	})
	require.NotEqual(t, carolDirectFwd, carolChainFwd)

	// Allow any queued async user events (e.g. bob) to flush before asserting negative isolation.
	time.Sleep(150 * time.Millisecond)

	_, ok := fx.um.GetUserPortByDst("alice", uint32(chainPort))
	require.False(t, ok, "alice (group=vip, inbound_tags=[]) MUST NOT get chain forward port (same-name Layer1->Layer2 leak check)")
	require.Nil(t, fx.fm.GetRule(fmt.Sprintf("%s:%s:alice", fx.ctype, chainTag)))

	_, ok = fx.um.GetUserPortByDst("bob", uint32(directPort))
	require.False(t, ok, "bob (group=other) must fail Layer 1 for direct inbound")
	_, ok = fx.um.GetUserPortByDst("bob", uint32(chainPort))
	require.False(t, ok, "bob (group=other, inbound_tags=[vip]) must fail Layer 1 for chain inbound")
	require.Empty(t, fx.fm.GetRulesByUser("bob"))

	// User View (/sub via SubscriptionManager)
	carolSubs, err := fx.subMgr.GetSubscription(contracts.SubscriptionRequest{
		User:     *carol,
		Host:     "127.0.0.1",
		NodeName: "FrontNode",
	})
	require.NoError(t, err)
	require.Len(t, carolSubs, 2, "carol must receive direct + chain subscriptions")
	carolChainSub, hasSuffix := findSubBySuffix(carolSubs, "-Home-SH")
	require.True(t, hasSuffix, "carol chain subscription NodeName must end with -Home-SH, got %+v", carolSubs)
	require.NotEqual(t, carolChainSub.URI, "")
	var carolDirectSub contracts.SubscriptionSpec
	for _, s := range carolSubs {
		if !strings.HasSuffix(s.NodeName, "-Home-SH") {
			carolDirectSub = s
			break
		}
	}
	require.NotEmpty(t, carolDirectSub.URI)

	aliceSubs, err := fx.subMgr.GetSubscription(contracts.SubscriptionRequest{
		User:     *alice,
		Host:     "127.0.0.1",
		NodeName: "FrontNode",
	})
	require.NoError(t, err)
	require.Len(t, aliceSubs, 1, "alice must receive ONLY direct subscription")
	_, aliceHasChain := findSubBySuffix(aliceSubs, "-Home-SH")
	require.False(t, aliceHasChain, "alice must NOT see chain subscription -Home-SH")

	bobSubs, err := fx.subMgr.GetSubscription(contracts.SubscriptionRequest{
		User:     *bob,
		Host:     "127.0.0.1",
		NodeName: "FrontNode",
	})
	require.NoError(t, err)
	require.Empty(t, bobSubs, "bob (group=other) must receive empty subscriptions")

	// Admin View (ListInboundConfigs + GetInboundMeta sees ALL inbounds regardless of tags)
	allInbs := fx.cont.ListInboundConfigs()
	require.True(t, hasInboundConfig(allInbs, directTag))
	require.True(t, hasInboundConfig(allInbs, chainTag))

	metaProv, ok := fx.cont.(container.InboundMetaProvider)
	require.True(t, ok, "%T must implement InboundMetaProvider", fx.cont)
	directMeta, ok := metaProv.GetInboundMeta(directTag)
	require.True(t, ok)
	require.Equal(t, contracts.InboundTypeDirect, directMeta.InboundType)
	require.Empty(t, directMeta.AccessTags)

	chainMeta, ok := metaProv.GetInboundMeta(chainTag)
	require.True(t, ok)
	require.Equal(t, contracts.InboundTypeChain, chainMeta.InboundType)
	require.Equal(t, []string{"vip"}, chainMeta.AccessTags)
	require.Equal(t, "Home-SH", chainMeta.Remark)

	// === Property 1: Real 2-hop data path ===
	carolChainSocks := fx.socksClientFrom(t, carolChainSub.URI)
	carolDirectSocks := fx.socksClientFrom(t, carolDirectSub.URI)

	// Direct baseline must NOT hit socksBackend.
	dialsBeforeDirect := socksBackend.DialCount()
	directPayload := fmt.Sprintf("direct-ok-%s\n", fx.ctype)
	gotDirect, err := sendEchoViaSocks5(carolDirectSocks, "127.0.0.1", echoSrv.Port(), directPayload)
	require.NoError(t, err)
	require.Equal(t, directPayload, gotDirect)
	require.Equal(t, dialsBeforeDirect, socksBackend.DialCount(), "direct inbound must NOT dial backend SOCKS5")

	// Chain 2-hop MUST go through authenticated socksBackend -> echoSrv.
	dialsBeforeChain := socksBackend.DialCount()
	echoBeforeChain := echoSrv.Hits()
	chainPayload := fmt.Sprintf("two-hop-ok-%s-%s\n", fx.ctype, carol.Username)
	gotChain, err := sendEchoViaSocks5(carolChainSocks, "127.0.0.1", echoSrv.Port(), chainPayload)
	require.NoError(t, err)
	require.Equal(t, chainPayload, gotChain)
	require.Greater(t, socksBackend.DialCount(), dialsBeforeChain, "chain inbound MUST dial through backend SOCKS5")
	require.Greater(t, echoSrv.Hits(), echoBeforeChain, "chain inbound MUST reach echo server via backend SOCKS5")
	require.Greater(t, socksBackend.AuthSuccess(), int64(0), "backend SOCKS5 auth must succeed")
	require.Equal(t, int64(0), socksBackend.AuthFailure(), "backend SOCKS5 must not see wrong credentials")

	// === Property 2: Remote DNS zero-leak ===
	const unresolvableDomain = "nonexistent.chain.invalid"
	sendRemoteDNSProbeViaSocks5(carolChainSocks, unresolvableDomain, 12345)
	eventually(t, "backend SOCKS5 records raw unresolvable domain ATYP=0x03", 5*time.Second, func() bool {
		return socksBackend.HasDomainRecord(unresolvableDomain, 12345)
	})

	// === Property 4: Dynamic SetInboundAccessTags + dynamic tag grant/revoke (zero restart) ===
	// Keep a persistent connection for carol open across tag mutations to prove zero restart & zero disruption.
	persistConn, err := dialSocks5Target(carolChainSocks, "127.0.0.1", echoSrv.Port())
	require.NoError(t, err)
	defer persistConn.Close()
	_ = persistConn.SetDeadline(time.Now().Add(15 * time.Second))
	_, err = persistConn.Write([]byte("before-tag-update\n"))
	require.NoError(t, err)
	buf := make([]byte, len("before-tag-update\n"))
	_, err = io.ReadFull(persistConn, buf)
	require.NoError(t, err)
	require.Equal(t, "before-tag-update\n", string(buf))

	tagUpdater, ok := fx.cont.(container.InboundTagUpdater)
	require.True(t, ok, "%T must implement InboundTagUpdater", fx.cont)
	require.NoError(t, tagUpdater.SetInboundAccessTags(chainTag, []string{"vip", "vip-plus"}))

	updatedMeta, ok := metaProv.GetInboundMeta(chainTag)
	require.True(t, ok)
	require.Equal(t, []string{"vip", "vip-plus"}, updatedMeta.AccessTags)

	// Grant alice "vip-plus" dynamically -> alice immediately gets Forward port, /sub entry, and passes 2-hop traffic.
	require.NoError(t, fx.um.SetUserInboundTags("alice", []string{"vip-plus"}))
	var aliceChainFwd uint32
	eventually(t, "alice gets dynamic chain forward port", 5*time.Second, func() bool {
		var found bool
		aliceChainFwd, found = fx.um.GetUserPortByDst("alice", uint32(chainPort))
		return found
	})
	require.NotEqual(t, aliceDirectFwd, aliceChainFwd)

	aliceUpdated, err := fx.um.GetUser("alice")
	require.NoError(t, err)
	aliceSubsGranted, err := fx.subMgr.GetSubscription(contracts.SubscriptionRequest{
		User:     *aliceUpdated,
		Host:     "127.0.0.1",
		NodeName: "FrontNode",
	})
	require.NoError(t, err)
	aliceChainSub, aliceNowHasChain := findSubBySuffix(aliceSubsGranted, "-Home-SH")
	require.True(t, aliceNowHasChain, "alice subscription must now include -Home-SH after tag grant")

	aliceChainSocks := fx.socksClientFrom(t, aliceChainSub.URI)
	alicePayload := fmt.Sprintf("alice-dynamic-grant-%s\n", fx.ctype)
	dialsBeforeAlice := socksBackend.DialCount()
	gotAlice, err := sendEchoViaSocks5(aliceChainSocks, "127.0.0.1", echoSrv.Port(), alicePayload)
	require.NoError(t, err)
	require.Equal(t, alicePayload, gotAlice)
	require.Greater(t, socksBackend.DialCount(), dialsBeforeAlice)

	// Revoke alice's tags -> Forward port must immediately release & stop accepting connections; carol unaffected!
	require.NoError(t, fx.um.SetUserInboundTags("alice", []string{}))
	eventually(t, "alice chain forward port revoked", 5*time.Second, func() bool {
		_, stillHas := fx.um.GetUserPortByDst("alice", uint32(chainPort))
		return !stillHas && fx.fm.GetRule(fmt.Sprintf("%s:%s:alice", fx.ctype, chainTag)) == nil
	})
	eventually(t, "alice revoked forward port closed", 5*time.Second, func() bool {
		return !isPortOpen(fmt.Sprintf("127.0.0.1:%d", aliceChainFwd))
	})
	aliceRevoked, err := fx.um.GetUser("alice")
	require.NoError(t, err)
	aliceSubsRevoked, err := fx.subMgr.GetSubscription(contracts.SubscriptionRequest{
		User:     *aliceRevoked,
		Host:     "127.0.0.1",
		NodeName: "FrontNode",
	})
	require.NoError(t, err)
	_, aliceStillHasChain := findSubBySuffix(aliceSubsRevoked, "-Home-SH")
	require.False(t, aliceStillHasChain)

	// Shrink chainTag access_tags back to ["vip"] while keeping carol's persistent connection intact.
	require.NoError(t, tagUpdater.SetInboundAccessTags(chainTag, []string{"vip"}))

	// Persistent connection opened before tag updates MUST still work cleanly (proving zero container restart).
	_, err = persistConn.Write([]byte("after-tag-update\n"))
	require.NoError(t, err)
	afterBuf := make([]byte, len("after-tag-update\n"))
	_, err = io.ReadFull(persistConn, afterBuf)
	require.NoError(t, err)
	require.Equal(t, "after-tag-update\n", string(afterBuf))
	_ = persistConn.Close()

	// === Property 5: Fail-Closed zero direct leak ===
	require.NoError(t, socksBackend.Close())
	echoHitsAtShutdown := echoSrv.Hits()

	failPayload := "must-fail-closed\n"
	_, failErr := sendEchoViaSocks5(carolChainSocks, "127.0.0.1", echoSrv.Port(), failPayload)
	require.Error(t, failErr, "chain proxy MUST fail closed when backend SOCKS5 is down")
	require.Equal(t, echoHitsAtShutdown, echoSrv.Hits(), "chain proxy MUST NOT fall back to direct echo server when backend SOCKS5 is down")
}

func TestChainProxy_Xray_RealTwoHop(t *testing.T) {
	xrayBin := resolveChainBinary(t, "XRAY_BIN", defaultXrayChainBin, "xray")
	tmpDir := t.TempDir()

	echoSrv := newChainEchoServer(t)
	socksBackend := newChainAuthSocks5Server(t, "chainuser", "chainpass")

	fm, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort:   28000,
		MaxPort:   28499,
		UseRandom: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = fm.Close() })

	sm, err := store.NewStoreManager(filepath.Join(tmpDir, "xray-chain.db"), migrations.All)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sm.Close() })

	um, err := usermanager.NewUserManagerWithStore(fm, sm, "xray-front")
	require.NoError(t, err)
	um.EnableClusterSync("default", &chainMemNodeGroupsStore{groups: []string{"vip"}})

	grpcPort, err := freeTCPPort()
	require.NoError(t, err)
	cfgPath := filepath.Join(tmpDir, "xray.json")
	writeChainXrayInitConfig(t, cfgPath, grpcPort)

	execCont, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:        xrayBin,
		ConfigFilePath:    cfgPath,
		GRPCAPIAddress:    fmt.Sprintf("127.0.0.1:%d", grpcPort),
		PortClaimer:       fm,
		StoreMgr:          sm,
		UserManager:       um,
		ReconcileInterval: 30 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, execCont.Start())
	t.Cleanup(func() { _ = execCont.Stop() })

	require.NoError(t, waitPort(fmt.Sprintf("127.0.0.1:%d", grpcPort), 10*time.Second))

	cm := container.NewContainerMgr(sm, container.BuildOptions{UserManager: um, PortClaimer: fm})
	cm.SetForTest(contracts.ContainerXray, execCont)
	subMgr := subscription.NewManager(cm, um)

	clientIdx := 0
	runChainProxyRealTwoHopSuite(t, chainContainerFixture{
		ctype:  contracts.ContainerXray,
		cont:   execCont,
		subMgr: subMgr,
		fm:     fm,
		sm:     sm,
		um:     um,
		socksClientFrom: func(t *testing.T, subURI string) string {
			clientIdx++
			clientDir := filepath.Join(tmpDir, fmt.Sprintf("xray-client-%d", clientIdx))
			require.NoError(t, os.MkdirAll(clientDir, 0o755))
			return spawnXraySSClient(t, clientDir, xrayBin, subURI)
		},
	}, socksBackend, echoSrv)
}

func TestChainProxy_Mihomo_RealTwoHop(t *testing.T) {
	mihomoBin := resolveChainBinary(t, "MIHOMO_BIN", defaultMihomoChainBin, "mihomo")
	tmpDir := t.TempDir()

	echoSrv := newChainEchoServer(t)
	socksBackend := newChainAuthSocks5Server(t, "chainuser", "chainpass")

	fm, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort:   28500,
		MaxPort:   28999,
		UseRandom: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = fm.Close() })

	sm, err := store.NewStoreManager(filepath.Join(tmpDir, "mihomo-chain.db"), migrations.All)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sm.Close() })

	um, err := usermanager.NewUserManagerWithStore(fm, sm, "mihomo-front")
	require.NoError(t, err)
	um.EnableClusterSync("default", &chainMemNodeGroupsStore{groups: []string{"vip"}})

	apiPort, err := freeTCPPort()
	require.NoError(t, err)
	dataDir := filepath.Join(tmpDir, "mihomo-data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	mihomoCont, err := mihomo.NewMihomoContainer(mihomo.MihomoConfig{
		BinaryPath:         mihomoBin,
		ConfigFilePath:     filepath.Join(tmpDir, "mihomo.yaml"),
		DataDir:            dataDir,
		ExternalController: fmt.Sprintf("127.0.0.1:%d", apiPort),
		ReleaseTag:         "Prerelease-Alpha",
		AutoDownload:       false,
	}, mihomo.WithStoreMgr(sm), mihomo.WithPortClaimer(fm), mihomo.WithUserManager(um))
	require.NoError(t, err)
	require.NoError(t, mihomoCont.Start())
	t.Cleanup(func() { _ = mihomoCont.Stop() })

	require.NoError(t, waitPort(fmt.Sprintf("127.0.0.1:%d", apiPort), 10*time.Second))

	cm := container.NewContainerMgr(sm, container.BuildOptions{UserManager: um, PortClaimer: fm})
	cm.SetForTest(contracts.ContainerMihomo, mihomoCont)
	subMgr := subscription.NewManager(cm, um)

	clientIdx := 0
	runChainProxyRealTwoHopSuite(t, chainContainerFixture{
		ctype:  contracts.ContainerMihomo,
		cont:   mihomoCont,
		subMgr: subMgr,
		fm:     fm,
		sm:     sm,
		um:     um,
		socksClientFrom: func(t *testing.T, subURI string) string {
			clientIdx++
			clientDir := filepath.Join(tmpDir, fmt.Sprintf("mihomo-client-%d", clientIdx))
			require.NoError(t, os.MkdirAll(clientDir, 0o755))
			return spawnMihomoSSClient(t, clientDir, mihomoBin, subURI)
		},
	}, socksBackend, echoSrv)
}
