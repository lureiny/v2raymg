//go:build integration

package systemtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

type chainProfileInboundInfo struct {
	Node      string `json:"node"`
	Tag       string `json:"tag"`
	Container string `json:"container"`
	Port      int    `json:"port"`
}

type chainInboundSummaryItem struct {
	Container     string   `json:"container"`
	Name          string   `json:"name"`
	InboundType   string   `json:"inbound_type"`
	AccessTags    []string `json:"access_tags"`
	Remark        string   `json:"remark"`
	ChainProtocol string   `json:"chain_protocol"`
	ChainTarget   string   `json:"chain_target"`
}

func apiGetWithBearer(t *testing.T, srv *e2eServer, path string, params url.Values, jwt string) (int, []byte, error) {
	t.Helper()
	recordRouteHit(http.MethodGet, path)

	u := srv.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

func loginBearer(t *testing.T, srv *e2eServer, username, password string) string {
	t.Helper()
	code, body, err := srv.apiPost("/api/login", map[string]any{
		"username": username,
		"password": password,
	})
	requireOK(t, fmt.Sprintf("%s POST /api/login(%s)", srv.name, username), code, body, err)
	var rsp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &rsp))
	require.NotEmpty(t, rsp.Token)
	return rsp.Token
}

func fetchProfileInbounds(t *testing.T, srv *e2eServer, jwt string) map[string]map[string]chainProfileInboundInfo {
	t.Helper()
	code, body, err := apiGetWithBearer(t, srv, "/api/profile", nil, jwt)
	requireOK(t, srv.name+" GET /api/profile", code, body, err)

	var raw map[string][]struct {
		Inbounds []chainProfileInboundInfo `json:"inbounds"`
	}
	require.NoError(t, json.Unmarshal(body, &raw), "body=%s", truncate(string(body), 600))

	out := make(map[string]map[string]chainProfileInboundInfo, len(raw))
	for nodeName, entries := range raw {
		byTag := make(map[string]chainProfileInboundInfo)
		if len(entries) > 0 {
			for _, ib := range entries[0].Inbounds {
				byTag[ib.Tag] = ib
			}
		}
		out[nodeName] = byTag
	}
	return out
}

func fetchAdminInbounds(t *testing.T, srv *e2eServer, target string, forbiddenSubstrs []string) map[string]map[string]chainInboundSummaryItem {
	t.Helper()
	code, body, err := srv.apiGet("/api/inbounds", url.Values{"target": {target}})
	requireOK(t, fmt.Sprintf("%s GET /api/inbounds?target=%s", srv.name, target), code, body, err)

	rawBody := string(body)
	for _, secret := range forbiddenSubstrs {
		require.NotContains(t, rawBody, secret, "GET /api/inbounds must redact sensitive string %q", secret)
	}

	var raw map[string][]chainInboundSummaryItem
	require.NoError(t, json.Unmarshal(body, &raw), "body=%s", truncate(rawBody, 600))

	out := make(map[string]map[string]chainInboundSummaryItem, len(raw))
	for nodeName, items := range raw {
		byTag := make(map[string]chainInboundSummaryItem, len(items))
		for _, it := range items {
			byTag[it.Name] = it
		}
		out[nodeName] = byTag
	}
	return out
}

func extractSuffixSetAndSSMap(t *testing.T, uris []string, expectedSuffixes []string) (map[string]string, bool) {
	t.Helper()
	if len(uris) != len(expectedSuffixes) {
		return nil, false
	}
	bySuffix := make(map[string]string, len(expectedSuffixes))
	for _, uri := range uris {
		ss, err := codec.DecodeShadowsocks(uri)
		if err != nil {
			return nil, false
		}
		for _, sfx := range expectedSuffixes {
			if strings.HasSuffix(ss.NodeName, sfx) {
				bySuffix[sfx] = uri
				break
			}
		}
	}
	if len(bySuffix) != len(expectedSuffixes) {
		return nil, false
	}
	return bySuffix, true
}

func uniqueClientDir(t *testing.T, rootDir, label string) string {
	t.Helper()
	dir := filepath.Join(rootDir, "client-"+label)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

func TestClusterE2E_ChainProxy_ThreeNodeRealDataPlane(t *testing.T) {
	xrayBin := resolveChainBinary(t, "XRAY_BIN", defaultXrayChainBin, "xray")
	mihomoBin := resolveChainBinary(t, "MIHOMO_BIN", defaultMihomoChainBin, "mihomo")
	tmpDir := t.TempDir()

	// Real echo destination + 3 distinct authenticated SOCKS5 chain backends.
	echoSrv := newChainEchoServer(t)
	backendA := newChainAuthSocks5Server(t, "userA", "secretPassA")
	backendB := newChainAuthSocks5Server(t, "userB", "secretPassB")
	backendC := newChainAuthSocks5Server(t, "userC", "secretPassC")

	// Start 3 independent real v2raymg end nodes (each with real Xray + real Mihomo cores).
	c := startE2ECluster(t, tmpDir, clusterOpts{
		HeartbeatIntervalSec:     1,
		XrayBin:                  xrayBin,
		MihomoBin:                mihomoBin,
		AllowLoopbackChainTarget: true,
	})
	for i := 0; i < 3; i++ {
		c.addEndNode(t)
	}
	c.waitConverged(t, 60*time.Second)
	c.waitFanoutReady(t, 60*time.Second)

	nodeA, nodeB, nodeC := c.ends[0], c.ends[1], c.ends[2]

	// Configure Layer 1 node groups across the 3 nodes via HTTP API on nodeA.
	setNodeGroups := func(from *e2eServer, targetNode *e2eServer, groups []string) {
		code, body, err := from.apiPut("/api/node/"+targetNode.name+"/groups", map[string]any{
			"groups": groups,
		})
		requireOK(t, fmt.Sprintf("PUT /api/node/%s/groups=%v", targetNode.name, groups), code, body, err)

		want := append([]string(nil), groups...)
		sort.Strings(want)
		eventually(t, fmt.Sprintf("%s groups updated to %v", targetNode.name, want), 15*time.Second, func() bool {
			code, body, err := from.apiGet("/api/node/"+targetNode.name+"/groups", nil)
			if err != nil || code != 200 {
				return false
			}
			var rsp struct {
				Groups []string `json:"groups"`
			}
			if json.Unmarshal(body, &rsp) != nil {
				return false
			}
			got := append([]string(nil), rsp.Groups...)
			sort.Strings(got)
			return strings.Join(got, ",") == strings.Join(want, ",")
		})
	}

	setNodeGroups(nodeA, nodeA, []string{"vip", "hk"})
	setNodeGroups(nodeA, nodeB, []string{"vip", "jp"})
	setNodeGroups(nodeA, nodeC, []string{"sg"})

	// Create 5 inbounds across the 3 nodes (issued from nodeA over real gRPC fan-out):
	// - nodeA: xray direct (public) + xray chain (access_tags=["vip"]) -> backendA
	// - nodeB: mihomo direct (public) + mihomo chain (access_tags=["vip","home-b"]) -> backendB
	// - nodeC: xray chain (access_tags=["vip"]) -> backendC
	fastAddSS := func(targetNode, tag, cont, inboundType, chainURI, remark string, accessTags []string) {
		payload := map[string]any{
			"target":       targetNode,
			"tag":          tag,
			"container":    cont,
			"protocol":     "ss",
			"transport":    "tcp",
			"security":     "none",
			"inbound_type": inboundType,
		}
		if chainURI != "" {
			payload["chain_uri"] = chainURI
		}
		if remark != "" {
			payload["remark"] = remark
		}
		if accessTags != nil {
			payload["access_tags"] = accessTags
		}
		code, body, err := nodeA.apiPost("/api/inbound/fast", payload)
		requireOK(t, fmt.Sprintf("POST /api/inbound/fast(%s/%s)", targetNode, tag), code, body, err)
	}

	fastAddSS(nodeA.name, "a-xray-direct", "xray", "direct", "", "DirectA", nil)
	fastAddSS(nodeA.name, "a-xray-chain", "xray", "chain",
		fmt.Sprintf("socks5://userA:secretPassA@127.0.0.1:%d#ChainXrayA", backendA.Port()), "", []string{"vip"})

	fastAddSS(nodeB.name, "b-mihomo-direct", "mihomo", "direct", "", "DirectB", nil)
	fastAddSS(nodeB.name, "b-mihomo-chain", "mihomo", "chain",
		fmt.Sprintf("socks5://userB:secretPassB@127.0.0.1:%d#ChainMihomoB", backendB.Port()), "", []string{"vip", "home-b"})

	fastAddSS(nodeC.name, "c-xray-chain", "xray", "chain",
		fmt.Sprintf("socks5://userC:secretPassC@127.0.0.1:%d#ChainXrayC", backendC.Port()), "", []string{"vip"})

	// Create 3 normal-role users with target="all" testing Layer 1 vs Layer 2 same-name isolation:
	// - carol: group="vip", inbound_tags=["vip"] -> sees nodeA + nodeB (direct+chain), NOT nodeC (group="sg")
	// - alice: group="vip", inbound_tags=[]      -> sees nodeA + nodeB direct ONLY (no Layer1 "vip" -> Layer2 "vip" leak)
	// - dave:  group="sg",  inbound_tags=["vip"] -> sees ONLY nodeC c-xray-chain
	const (
		carolUser, carolPass = "e2e-carol", "carol-pass-12345"
		aliceUser, alicePass = "e2e-alice", "alice-pass-12345"
		daveUser, davePass   = "e2e-dave", "dave-pass-12345"
	)
	addClusterUser := func(username, password, group string, inboundTags []string) {
		code, body, err := nodeA.apiPost("/api/user", map[string]any{
			"target":       "all",
			"user":         username,
			"pwd":          password,
			"role":         "normal",
			"ttl":          86400,
			"group":        group,
			"inbound_tags": inboundTags,
		})
		requireOK(t, fmt.Sprintf("POST /api/user(%s)", username), code, body, err)
		t.Cleanup(func() {
			_, _, _ = nodeA.apiDelete("/api/user", map[string]any{
				"target": "all",
				"user":   username,
			})
		})
		for _, s := range c.ends {
			s := s
			eventually(t, fmt.Sprintf("%s sees user %s", s.name, username), 20*time.Second, func() bool {
				return userNames(t, s, s.name)[username]
			})
		}
	}

	addClusterUser(carolUser, carolPass, "vip", []string{"vip"})
	addClusterUser(aliceUser, alicePass, "vip", []string{})
	addClusterUser(daveUser, davePass, "sg", []string{"vip"})

	// =========================================================================
	// Phase 1: Aggregated /sub, /api/profile, and Admin Control Plane /api/inbounds
	// =========================================================================

	// Fetch subscriptions from Layer-1-ineligible access nodes (nodeC for carol/alice, nodeA for dave)
	// to prove cross-node RPC subscription aggregation ignores the access node's own Layer 1 visibility.
	var carolSubBySuffix, aliceSubBySuffix, daveSubBySuffix map[string]string

	eventually(t, "carol aggregated /sub from nodeC has 4 expected URIs", 20*time.Second, func() bool {
		uris, err := nodeC.getRawSub(carolUser, carolPass)
		if err != nil {
			return false
		}
		m, ok := extractSuffixSetAndSSMap(t, uris, []string{"-DirectA", "-ChainXrayA", "-DirectB", "-ChainMihomoB"})
		if !ok {
			return false
		}
		carolSubBySuffix = m
		return true
	})

	eventually(t, "alice aggregated /sub from nodeC has 2 direct URIs only", 20*time.Second, func() bool {
		uris, err := nodeC.getRawSub(aliceUser, alicePass)
		if err != nil {
			return false
		}
		m, ok := extractSuffixSetAndSSMap(t, uris, []string{"-DirectA", "-DirectB"})
		if !ok {
			return false
		}
		aliceSubBySuffix = m
		return true
	})

	eventually(t, "dave aggregated /sub from nodeA has 1 nodeC chain URI only", 20*time.Second, func() bool {
		uris, err := nodeA.getRawSub(daveUser, davePass)
		if err != nil {
			return false
		}
		m, ok := extractSuffixSetAndSSMap(t, uris, []string{"-ChainXrayC"})
		if !ok {
			return false
		}
		daveSubBySuffix = m
		return true
	})

	require.Len(t, carolSubBySuffix, 4)
	require.Len(t, aliceSubBySuffix, 2)
	require.NotEmpty(t, aliceSubBySuffix["-DirectA"])
	require.NotEmpty(t, aliceSubBySuffix["-DirectB"])
	require.Len(t, daveSubBySuffix, 1)

	carolJWT := loginBearer(t, nodeA, carolUser, carolPass)
	aliceJWT := loginBearer(t, nodeB, aliceUser, alicePass)
	daveJWT := loginBearer(t, nodeC, daveUser, davePass)

	// Verify GET /api/profile (default target=all) for each user.
	eventually(t, "carol /api/profile matches Layer1+Layer2 across all 3 nodes", 15*time.Second, func() bool {
		prof := fetchProfileInbounds(t, nodeC, carolJWT)
		aInbs := prof[nodeA.name]
		bInbs := prof[nodeB.name]
		cInbs := prof[nodeC.name]
		return len(aInbs) == 2 && aInbs["a-xray-direct"].Port > 0 && aInbs["a-xray-chain"].Port > 0 &&
			len(bInbs) == 2 && bInbs["b-mihomo-direct"].Port > 0 && bInbs["b-mihomo-chain"].Port > 0 &&
			len(cInbs) == 0
	})

	eventually(t, "alice /api/profile excludes tagged chain inbounds", 15*time.Second, func() bool {
		prof := fetchProfileInbounds(t, nodeA, aliceJWT)
		aInbs := prof[nodeA.name]
		bInbs := prof[nodeB.name]
		cInbs := prof[nodeC.name]
		return len(aInbs) == 1 && aInbs["a-xray-direct"].Port > 0 &&
			len(bInbs) == 1 && bInbs["b-mihomo-direct"].Port > 0 &&
			len(cInbs) == 0
	})

	eventually(t, "dave /api/profile sees only nodeC c-xray-chain", 15*time.Second, func() bool {
		prof := fetchProfileInbounds(t, nodeA, daveJWT)
		return len(prof[nodeA.name]) == 0 &&
			len(prof[nodeB.name]) == 0 &&
			len(prof[nodeC.name]) == 1 && prof[nodeC.name]["c-xray-chain"].Port > 0
	})

	// Admin Control Plane GET /api/inbounds?target=all (from nodeC) returns ALL 5 inbounds with redacted chain info.
	adminInbounds := fetchAdminInbounds(t, nodeC, "all", []string{
		"secretPassA", "secretPassB", "secretPassC",
		"userA", "userB", "userC",
		"chain_uri", "chain_outbound_uri",
	})
	require.Equal(t, "direct", adminInbounds[nodeA.name]["a-xray-direct"].InboundType)
	require.Empty(t, adminInbounds[nodeA.name]["a-xray-direct"].ChainTarget)
	require.Equal(t, "chain", adminInbounds[nodeA.name]["a-xray-chain"].InboundType)
	require.Equal(t, "socks5", adminInbounds[nodeA.name]["a-xray-chain"].ChainProtocol)
	require.Equal(t, fmt.Sprintf("127.0.0.1:%d", backendA.Port()), adminInbounds[nodeA.name]["a-xray-chain"].ChainTarget)
	require.Equal(t, []string{"vip"}, adminInbounds[nodeA.name]["a-xray-chain"].AccessTags)

	require.Equal(t, "direct", adminInbounds[nodeB.name]["b-mihomo-direct"].InboundType)
	require.Equal(t, "chain", adminInbounds[nodeB.name]["b-mihomo-chain"].InboundType)
	require.Equal(t, "socks5", adminInbounds[nodeB.name]["b-mihomo-chain"].ChainProtocol)
	require.Equal(t, fmt.Sprintf("127.0.0.1:%d", backendB.Port()), adminInbounds[nodeB.name]["b-mihomo-chain"].ChainTarget)
	require.Equal(t, []string{"home-b", "vip"}, adminInbounds[nodeB.name]["b-mihomo-chain"].AccessTags)

	require.Equal(t, "chain", adminInbounds[nodeC.name]["c-xray-chain"].InboundType)
	require.Equal(t, "socks5", adminInbounds[nodeC.name]["c-xray-chain"].ChainProtocol)
	require.Equal(t, fmt.Sprintf("127.0.0.1:%d", backendC.Port()), adminInbounds[nodeC.name]["c-xray-chain"].ChainTarget)

	// =========================================================================
	// Phase 2: Real Cross-Node Two-Hop Data Plane + Remote DNS Zero-Leak
	// =========================================================================

	carolDirectXraySocks := spawnXraySSClient(t, uniqueClientDir(t, tmpDir, "carol-direct-a"), xrayBin, carolSubBySuffix["-DirectA"])
	carolChainXraySocks := spawnXraySSClient(t, uniqueClientDir(t, tmpDir, "carol-chain-a"), xrayBin, carolSubBySuffix["-ChainXrayA"])
	carolDirectMihomoSocks := spawnMihomoSSClient(t, uniqueClientDir(t, tmpDir, "carol-direct-b"), mihomoBin, carolSubBySuffix["-DirectB"])
	carolChainMihomoSocks := spawnMihomoSSClient(t, uniqueClientDir(t, tmpDir, "carol-chain-b"), mihomoBin, carolSubBySuffix["-ChainMihomoB"])
	daveChainXraySocks := spawnXraySSClient(t, uniqueClientDir(t, tmpDir, "dave-chain-c"), xrayBin, daveSubBySuffix["-ChainXrayC"])

	// 1. Direct Xray on nodeA must NOT hit backendA.
	dialsBefore := backendA.DialCount()
	got, err := sendEchoViaSocks5(carolDirectXraySocks, "127.0.0.1", echoSrv.Port(), "cluster-direct-a\n")
	require.NoError(t, err)
	require.Equal(t, "cluster-direct-a\n", got)
	require.Equal(t, dialsBefore, backendA.DialCount(), "direct Xray inbound must NOT hit backendA")

	// 2. Chain Xray on nodeA MUST route through backendA ONLY.
	dialsA, dialsB, dialsC := backendA.DialCount(), backendB.DialCount(), backendC.DialCount()
	got, err = sendEchoViaSocks5(carolChainXraySocks, "127.0.0.1", echoSrv.Port(), "cluster-chain-a\n")
	require.NoError(t, err)
	require.Equal(t, "cluster-chain-a\n", got)
	require.Greater(t, backendA.DialCount(), dialsA)
	require.Greater(t, backendA.AuthSuccess(), int64(0))
	require.Equal(t, int64(0), backendA.AuthFailure())
	require.Equal(t, dialsB, backendB.DialCount())
	require.Equal(t, dialsC, backendC.DialCount())

	// 3. Direct Mihomo on nodeB must NOT hit backendB.
	dialsBefore = backendB.DialCount()
	got, err = sendEchoViaSocks5(carolDirectMihomoSocks, "127.0.0.1", echoSrv.Port(), "cluster-direct-b\n")
	require.NoError(t, err)
	require.Equal(t, "cluster-direct-b\n", got)
	require.Equal(t, dialsBefore, backendB.DialCount(), "direct Mihomo inbound must NOT hit backendB")

	// 4. Chain Mihomo on nodeB MUST route through backendB ONLY.
	dialsA, dialsB, dialsC = backendA.DialCount(), backendB.DialCount(), backendC.DialCount()
	got, err = sendEchoViaSocks5(carolChainMihomoSocks, "127.0.0.1", echoSrv.Port(), "cluster-chain-b\n")
	require.NoError(t, err)
	require.Equal(t, "cluster-chain-b\n", got)
	require.Greater(t, backendB.DialCount(), dialsB)
	require.Greater(t, backendB.AuthSuccess(), int64(0))
	require.Equal(t, int64(0), backendB.AuthFailure())
	require.Equal(t, dialsA, backendA.DialCount())
	require.Equal(t, dialsC, backendC.DialCount())

	// 5. Dave Chain Xray on nodeC MUST route through backendC ONLY.
	dialsA, dialsB, dialsC = backendA.DialCount(), backendB.DialCount(), backendC.DialCount()
	got, err = sendEchoViaSocks5(daveChainXraySocks, "127.0.0.1", echoSrv.Port(), "cluster-chain-c\n")
	require.NoError(t, err)
	require.Equal(t, "cluster-chain-c\n", got)
	require.Greater(t, backendC.DialCount(), dialsC)
	require.Greater(t, backendC.AuthSuccess(), int64(0))
	require.Equal(t, int64(0), backendC.AuthFailure())
	require.Equal(t, dialsA, backendA.DialCount())
	require.Equal(t, dialsB, backendB.DialCount())

	// 6. Remote DNS zero-leak across both Xray (nodeA) and Mihomo (nodeB) chain inbounds.
	sendRemoteDNSProbeViaSocks5(carolChainXraySocks, "xray-cluster.chain.invalid", 12345)
	eventually(t, "backendA records ATYP=0x03 domain from nodeA Xray chain", 5*time.Second, func() bool {
		return backendA.HasDomainRecord("xray-cluster.chain.invalid", 12345)
	})
	sendRemoteDNSProbeViaSocks5(carolChainMihomoSocks, "mihomo-cluster.chain.invalid", 12345)
	eventually(t, "backendB records ATYP=0x03 domain from nodeB Mihomo chain", 5*time.Second, func() bool {
		return backendB.HasDomainRecord("mihomo-cluster.chain.invalid", 12345)
	})

	// =========================================================================
	// Phase 3: Dynamic Cluster Mutations (Zero Restart + Immediate Grant/Revoke)
	// =========================================================================

	// Open two persistent connections for carol BEFORE mutations to prove zero container restart.
	persistA, err := dialSocks5Target(carolChainXraySocks, "127.0.0.1", echoSrv.Port())
	require.NoError(t, err)
	defer persistA.Close()
	_ = persistA.SetDeadline(time.Now().Add(60 * time.Second))

	persistB, err := dialSocks5Target(carolChainMihomoSocks, "127.0.0.1", echoSrv.Port())
	require.NoError(t, err)
	defer persistB.Close()
	_ = persistB.SetDeadline(time.Now().Add(60 * time.Second))

	for idx, conn := range []io.ReadWriter{persistA, persistB} {
		const msg = "before-cluster-mutation\n"
		_, wErr := conn.Write([]byte(msg))
		require.NoError(t, wErr, "persist[%d] initial write", idx)
		buf := make([]byte, len(msg))
		_, rErr := io.ReadFull(conn, buf)
		require.NoError(t, rErr, "persist[%d] initial read", idx)
		require.Equal(t, msg, string(buf))
	}

	// Dynamic inbound tag updates across cluster nodes (issued from nodeB -> nodeA, nodeC -> nodeB).
	code, body, err := nodeB.apiPut("/api/inbounds/tags", map[string]any{
		"target":      nodeA.name,
		"container":   "xray",
		"name":        "a-xray-chain",
		"access_tags": []string{"vip", "promo"},
	})
	requireOK(t, "PUT /api/inbounds/tags(nodeA)", code, body, err)

	code, body, err = nodeC.apiPut("/api/inbounds/tags", map[string]any{
		"target":      nodeB.name,
		"container":   "mihomo",
		"name":        "b-mihomo-chain",
		"access_tags": []string{"vip", "home-b", "promo"},
	})
	requireOK(t, "PUT /api/inbounds/tags(nodeB)", code, body, err)

	// Grant alice inbound_tags=["promo"] via nodeC targeting all nodes.
	code, body, err = nodeC.apiPut("/api/user", map[string]any{
		"target":       "all",
		"user":         aliceUser,
		"inbound_tags": []string{"promo"},
	})
	requireOK(t, "PUT /api/user grant alice promo", code, body, err)

	var aliceGrantedSub map[string]string
	eventually(t, "alice aggregated /sub gains ChainXrayA + ChainMihomoB", 20*time.Second, func() bool {
		uris, err := nodeC.getRawSub(aliceUser, alicePass)
		if err != nil {
			return false
		}
		m, ok := extractSuffixSetAndSSMap(t, uris, []string{"-DirectA", "-ChainXrayA", "-DirectB", "-ChainMihomoB"})
		if !ok {
			return false
		}
		aliceGrantedSub = m
		return true
	})

	var alicePortA, alicePortB int
	eventually(t, "alice /api/profile allocates chain Forward ports on nodeA and nodeB", 15*time.Second, func() bool {
		prof := fetchProfileInbounds(t, nodeA, aliceJWT)
		pa := prof[nodeA.name]["a-xray-chain"].Port
		pb := prof[nodeB.name]["b-mihomo-chain"].Port
		if pa > 0 && pb > 0 {
			alicePortA = pa
			alicePortB = pb
			return true
		}
		return false
	})

	// Prove alice now sends real 2-hop traffic through nodeA->backendA and nodeB->backendB.
	aliceChainXraySocks := spawnXraySSClient(t, uniqueClientDir(t, tmpDir, "alice-chain-a"), xrayBin, aliceGrantedSub["-ChainXrayA"])
	aliceChainMihomoSocks := spawnMihomoSSClient(t, uniqueClientDir(t, tmpDir, "alice-chain-b"), mihomoBin, aliceGrantedSub["-ChainMihomoB"])

	dialsA = backendA.DialCount()
	got, err = sendEchoViaSocks5(aliceChainXraySocks, "127.0.0.1", echoSrv.Port(), "alice-promo-a\n")
	require.NoError(t, err)
	require.Equal(t, "alice-promo-a\n", got)
	require.Greater(t, backendA.DialCount(), dialsA)

	dialsB = backendB.DialCount()
	got, err = sendEchoViaSocks5(aliceChainMihomoSocks, "127.0.0.1", echoSrv.Port(), "alice-promo-b\n")
	require.NoError(t, err)
	require.Equal(t, "alice-promo-b\n", got)
	require.Greater(t, backendB.DialCount(), dialsB)

	// Revoke alice's inbound_tags across the cluster and assert immediate Forward port closure.
	code, body, err = nodeA.apiPut("/api/user", map[string]any{
		"target":       "all",
		"user":         aliceUser,
		"inbound_tags": []string{},
	})
	requireOK(t, "PUT /api/user revoke alice tags", code, body, err)

	eventually(t, "alice /sub drops back to 2 direct URIs", 20*time.Second, func() bool {
		uris, err := nodeC.getRawSub(aliceUser, alicePass)
		if err != nil {
			return false
		}
		_, ok := extractSuffixSetAndSSMap(t, uris, []string{"-DirectA", "-DirectB"})
		return ok
	})
	eventually(t, "alice /api/profile removes chain inbounds", 15*time.Second, func() bool {
		prof := fetchProfileInbounds(t, nodeB, aliceJWT)
		_, hasA := prof[nodeA.name]["a-xray-chain"]
		_, hasB := prof[nodeB.name]["b-mihomo-chain"]
		return !hasA && !hasB
	})
	eventually(t, "alice revoked Forward ports closed at TCP level", 10*time.Second, func() bool {
		return !isPortOpen(fmt.Sprintf("127.0.0.1:%d", alicePortA)) &&
			!isPortOpen(fmt.Sprintf("127.0.0.1:%d", alicePortB))
	})

	// Dynamic Layer 1 node-group switch on nodeC: add "vip" so carol gains c-xray-chain.
	setNodeGroups(nodeA, nodeC, []string{"sg", "vip"})

	var carolExpandedSub map[string]string
	eventually(t, "carol /sub gains ChainXrayC after nodeC adds vip group", 20*time.Second, func() bool {
		uris, err := nodeC.getRawSub(carolUser, carolPass)
		if err != nil {
			return false
		}
		m, ok := extractSuffixSetAndSSMap(t, uris, []string{"-DirectA", "-ChainXrayA", "-DirectB", "-ChainMihomoB", "-ChainXrayC"})
		if !ok {
			return false
		}
		carolExpandedSub = m
		return true
	})

	var carolPortC int
	eventually(t, "carol /api/profile gains c-xray-chain on nodeC", 15*time.Second, func() bool {
		prof := fetchProfileInbounds(t, nodeA, carolJWT)
		p := prof[nodeC.name]["c-xray-chain"].Port
		if p > 0 {
			carolPortC = p
			return true
		}
		return false
	})

	// Prove carol sends real 2-hop traffic through nodeC -> backendC -> echoSrv.
	carolChainC := spawnXraySSClient(t, uniqueClientDir(t, tmpDir, "carol-chain-c"), xrayBin, carolExpandedSub["-ChainXrayC"])
	dialsC = backendC.DialCount()
	got, err = sendEchoViaSocks5(carolChainC, "127.0.0.1", echoSrv.Port(), "carol-nodec-2hop\n")
	require.NoError(t, err)
	require.Equal(t, "carol-nodec-2hop\n", got)
	require.Greater(t, backendC.DialCount(), dialsC)

	// Restore nodeC groups to ["sg"]: carol loses c-xray-chain + Forward port closes; dave still works.
	setNodeGroups(nodeA, nodeC, []string{"sg"})

	eventually(t, "carol /sub drops ChainXrayC after nodeC removes vip group", 20*time.Second, func() bool {
		uris, err := nodeC.getRawSub(carolUser, carolPass)
		if err != nil {
			return false
		}
		_, ok := extractSuffixSetAndSSMap(t, uris, []string{"-DirectA", "-ChainXrayA", "-DirectB", "-ChainMihomoB"})
		return ok
	})
	eventually(t, "carol /api/profile removes c-xray-chain and closes port", 15*time.Second, func() bool {
		prof := fetchProfileInbounds(t, nodeA, carolJWT)
		_, stillHas := prof[nodeC.name]["c-xray-chain"]
		return !stillHas && !isPortOpen(fmt.Sprintf("127.0.0.1:%d", carolPortC))
	})

	dialsC = backendC.DialCount()
	got, err = sendEchoViaSocks5(daveChainXraySocks, "127.0.0.1", echoSrv.Port(), "dave-still-alive\n")
	require.NoError(t, err)
	require.Equal(t, "dave-still-alive\n", got)
	require.Greater(t, backendC.DialCount(), dialsC)

	// Persistent connections opened BEFORE Phase 3 must still echo without interruption (zero restart).
	for idx, conn := range []io.ReadWriter{persistA, persistB} {
		const msg = "after-cluster-mutation\n"
		_, wErr := conn.Write([]byte(msg))
		require.NoError(t, wErr, "persist[%d] post-mutation write", idx)
		buf := make([]byte, len(msg))
		_, rErr := io.ReadFull(conn, buf)
		require.NoError(t, rErr, "persist[%d] post-mutation read", idx)
		require.Equal(t, msg, string(buf))
	}
	_ = persistA.Close()
	_ = persistB.Close()

	// =========================================================================
	// Phase 4: Fail-Closed Proof on Real Cluster Nodes
	// =========================================================================
	require.NoError(t, backendA.Close())
	require.NoError(t, backendB.Close())
	echoHitsAtShutdown := echoSrv.Hits()

	_, failErrA := sendEchoViaSocks5(carolChainXraySocks, "127.0.0.1", echoSrv.Port(), "must-fail-closed-a\n")
	require.Error(t, failErrA, "nodeA Xray chain MUST fail closed when backendA is down")

	_, failErrB := sendEchoViaSocks5(carolChainMihomoSocks, "127.0.0.1", echoSrv.Port(), "must-fail-closed-b\n")
	require.Error(t, failErrB, "nodeB Mihomo chain MUST fail closed when backendB is down")

	require.Equal(t, echoHitsAtShutdown, echoSrv.Hits(), "chain inbounds MUST NOT leak directly to echoSrv when backends are down")
}
