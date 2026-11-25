package mihomo

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
	"gopkg.in/yaml.v3"
)

func TestMihomoContainer_OptionalInterfaceCompliance(t *testing.T) {
	c := newTestContainer(t)

	cc, ok := any(c).(container.ChainCapable)
	if !ok {
		t.Fatal("MihomoContainer must implement container.ChainCapable")
	}
	if !cc.SupportsChainOutbound() {
		t.Fatal("MihomoContainer.SupportsChainOutbound() must return true")
	}

	if _, ok := any(c).(container.InboundTagUpdater); !ok {
		t.Fatal("MihomoContainer must implement container.InboundTagUpdater")
	}
	if _, ok := any(c).(container.InboundMetaProvider); !ok {
		t.Fatal("MihomoContainer must implement container.InboundMetaProvider")
	}
}

func TestMihomoContainer_ValidateChainURI(t *testing.T) {
	c := newTestContainer(t)

	validURIs := []string{
		"socks5://user:pass@100.64.1.2:1080#TailscaleHome",
		"hy2://alice:secret@home.ddns.example:28443?sni=bing.com&insecure=1&mport=20000-40000#SH-Home",
		"vless://11111111-1111-4111-8111-111111111111@hk.example.com:443?security=reality&pbk=pub1&sid=ab12&sni=www.microsoft.com&encryption=none#VL-Chain",
		"https://u:p@proxy.example.com:8443?sni=proxy.example.com",
	}
	for _, raw := range validURIs {
		if err := c.ValidateChainURI(raw); err != nil {
			t.Errorf("ValidateChainURI(%q) unexpected error: %v", raw, err)
		}
	}

	invalidCases := []struct {
		name    string
		uri     string
		wantSub string
	}{
		{"empty", "", "empty"},
		{"missing scheme", "1.2.3.4:1080", "scheme"},
		{"vless httpupgrade suggests xray", "vless://uuid@host.com:443?security=tls&type=httpupgrade&path=/up", "xray"},
		{"vless xhttp suggests xray", "vless://uuid@host.com:443?security=tls&type=xhttp&path=/xh", "xray"},
		{"vless mlkem768 suggests xray", "vless://uuid@host.com:443?security=tls&encryption=mlkem768x25519plus.native", "xray"},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateChainURI(tc.uri)
			if err == nil {
				t.Fatalf("ValidateChainURI(%q) expected error", tc.uri)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.wantSub) {
				t.Errorf("error = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestBuildConfig_ChainRoutingYAML(t *testing.T) {
	c := newTestContainer(t)

	direct := NewMihomoInbound("z-direct", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u-direct"})
	chainB := NewMihomoInbound("b-chain", contracts.ProtocolTrojan, 10002, MihomoSharedCred{Password: "pw-b"})
	chainB.SetMeta(contracts.InboundMeta{
		InboundType:      contracts.InboundTypeChain,
		ChainOutboundURI: "hy2://user:pass@home-b.ddns.example:28443?sni=bing.com&insecure=1&mport=20000-30000#HomeB",
	})
	chainA := NewMihomoInbound("a-chain", contracts.ProtocolShadowsocks, 10003, MihomoSharedCred{Password: "pw-a", Cipher: "aes-128-gcm"})
	chainA.SetMeta(contracts.InboundMeta{
		InboundType:      contracts.InboundTypeChain,
		ChainOutboundURI: "socks5://u:p@100.64.0.5:1080#HomeA",
	})

	// Pass out of tag order; output must be sorted deterministically by tag.
	data, err := BuildConfig(c.buildBaseConfig(), []*MihomoInbound{direct, chainB, chainA})
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	if cfg["mode"] != "rule" {
		t.Errorf("mode = %v, want rule", cfg["mode"])
	}
	if cfg["ipv6"] != true {
		t.Errorf("ipv6 = %v, want true", cfg["ipv6"])
	}

	// Verify proxies[]
	rawProxies, ok := cfg["proxies"].([]any)
	if !ok || len(rawProxies) != 2 {
		t.Fatalf("proxies = %#v, want 2 entries", cfg["proxies"])
	}
	p0 := rawProxies[0].(map[string]any)
	p1 := rawProxies[1].(map[string]any)
	if p0["name"] != "__chain_out__:a-chain" || p0["type"] != "socks5" || p0["server"] != "100.64.0.5" {
		t.Errorf("proxies[0] mismatch: %#v", p0)
	}
	if p1["name"] != "__chain_out__:b-chain" || p1["type"] != "hysteria2" || p1["ports"] != "20000-30000" {
		t.Errorf("proxies[1] mismatch: %#v", p1)
	}

	// Verify listeners[].proxy
	rawListeners := cfg["listeners"].([]any)
	byName := map[string]map[string]any{}
	for _, entry := range rawListeners {
		m := entry.(map[string]any)
		byName[m["name"].(string)] = m
	}
	if byName["a-chain"]["proxy"] != "__chain_out__:a-chain" {
		t.Errorf("a-chain listener proxy = %v", byName["a-chain"]["proxy"])
	}
	if byName["b-chain"]["proxy"] != "__chain_out__:b-chain" {
		t.Errorf("b-chain listener proxy = %v", byName["b-chain"]["proxy"])
	}
	if _, hasProxy := byName["z-direct"]["proxy"]; hasProxy {
		t.Errorf("direct listener must not set proxy, got %v", byName["z-direct"]["proxy"])
	}

	// Verify rules[] deterministic order before MATCH,DIRECT
	rawRules, _ := cfg["rules"].([]any)
	wantRules := []any{
		"IN-NAME,a-chain,__chain_out__:a-chain",
		"IN-NAME,b-chain,__chain_out__:b-chain",
		"MATCH,DIRECT",
	}
	if !reflect.DeepEqual(rawRules, wantRules) {
		t.Errorf("rules = %#v, want %#v", rawRules, wantRules)
	}
}

func TestBuildListener_ChainRejectsNonLoopback(t *testing.T) {
	inb := NewMihomoInbound("bad-chain", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	inb.DefaultInbound.SetListenAddr("0.0.0.0")
	inb.SetMeta(contracts.InboundMeta{
		InboundType:      contracts.InboundTypeChain,
		ChainOutboundURI: "socks5://1.2.3.4:1080",
	})

	if _, err := BuildListener(inb); err == nil {
		t.Fatal("BuildListener must reject non-127.0.0.1 chain inbound")
	}
}

func TestFastAddInbound_ChainValidationAndListenLock(t *testing.T) {
	c := newTestContainer(t)

	// Invalid tags rejected
	for _, badTag := range []string{"", "__chain_out__:x", "bad,tag", "bad tag"} {
		if err := c.FastAddInbound(badTag, map[string]any{
			"protocol": "vmess",
			"port":     10001,
			"uuid":     "u",
		}); err == nil {
			t.Errorf("expected tag %q to be rejected", badTag)
		}
	}

	// Mutual consistency checks
	consistencyCases := []struct {
		name   string
		params map[string]any
	}{
		{
			name: "chain requires chain_uri",
			params: map[string]any{
				"protocol":     "vmess",
				"port":         10001,
				"uuid":         "u",
				"inbound_type": "chain",
			},
		},
		{
			name: "direct forbids chain_uri",
			params: map[string]any{
				"protocol":     "vmess",
				"port":         10001,
				"uuid":         "u",
				"inbound_type": "direct",
				"chain_uri":    "socks5://1.2.3.4:1080",
			},
		},
		{
			name: "invalid inbound_type",
			params: map[string]any{
				"protocol":     "vmess",
				"port":         10001,
				"uuid":         "u",
				"inbound_type": "bogus",
			},
		},
		{
			name: "invalid access_tags",
			params: map[string]any{
				"protocol":    "vmess",
				"port":        10001,
				"uuid":        "u",
				"access_tags": []string{"ok", "bad,tag"},
			},
		},
		{
			name: "chain rejects non-loopback listen on ProtocolParams path",
			params: map[string]any{
				"protocol":     "vless",
				"port":         10001,
				"uuid":         "u-vless",
				"inbound_type": "chain",
				"chain_uri":    "socks5://1.2.3.4:1080",
				"listen":       "0.0.0.0",
			},
		},
		{
			name: "chain rejects non-loopback listen_addr on ProtocolParams path",
			params: map[string]any{
				"protocol":     "vless",
				"port":         10001,
				"uuid":         "u-vless",
				"inbound_type": "chain",
				"chain_uri":    "socks5://1.2.3.4:1080",
				"listen_addr":  "0.0.0.0",
			},
		},
		{
			name: "chain rejects unsupported backend transport",
			params: map[string]any{
				"protocol":     "vless",
				"port":         10001,
				"uuid":         "u-vless",
				"inbound_type": "chain",
				"chain_uri":    "vless://uuid@host:443?security=tls&type=httpupgrade&path=/up",
			},
		},
	}
	for _, tc := range consistencyCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := c.FastAddInbound("test-in", tc.params); err == nil {
				t.Fatalf("expected FastAddInbound to fail for %s", tc.name)
			}
		})
	}

	// Happy path: empty inbound_type + non-empty chain_uri normalizes to chain,
	// remark falls back to URI #fragment, access_tags are normalized/sorted/deduped,
	// and listen defaults to 127.0.0.1.
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := c.FastAddInbound("sh-home", map[string]any{
		"protocol":    "vless",
		"port":        10101,
		"uuid":        "u-vless",
		"chain_uri":   "hy2://alice:secret@home.example:28443?sni=bing.com#SH-Home",
		"access_tags": []string{" vip ", "home-sh", "vip", ""},
		"listen":      "",
	}); err != nil {
		t.Fatalf("FastAddInbound valid chain: %v", err)
	}

	inb, err := c.GetInboundConfig("sh-home")
	if err != nil {
		t.Fatalf("GetInboundConfig: %v", err)
	}
	if inb.ListenAddr() != "127.0.0.1" {
		t.Errorf("ListenAddr = %q, want 127.0.0.1", inb.ListenAddr())
	}

	meta, ok := c.GetInboundMeta("sh-home")
	if !ok {
		t.Fatal("GetInboundMeta returned false")
	}
	if meta.InboundType != contracts.InboundTypeChain {
		t.Errorf("InboundType = %q, want chain", meta.InboundType)
	}
	if meta.Remark != "SH-Home" {
		t.Errorf("Remark = %q, want SH-Home", meta.Remark)
	}
	if !reflect.DeepEqual(meta.AccessTags, []string{"home-sh", "vip"}) {
		t.Errorf("AccessTags = %v, want [home-sh vip]", meta.AccessTags)
	}

	// Verify GetInboundMeta returns a cloned AccessTags slice.
	meta.AccessTags[0] = "tampered"
	meta2, _ := c.GetInboundMeta("sh-home")
	if !reflect.DeepEqual(meta2.AccessTags, []string{"home-sh", "vip"}) {
		t.Errorf("GetInboundMeta did not clone AccessTags: %v", meta2.AccessTags)
	}
}

func TestFastAddInbound_ChainPushFailure_RollsBackStoreMapAndPort(t *testing.T) {
	c, fm := newClaimingContainer(t, 26000, 26099)
	srv := newCapturingRESTServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"rejected"}`))
	})
	attachRunningREST(c, srv.URL())

	err := c.FastAddInbound("chain-fail", map[string]any{
		"protocol":     "vless",
		"port":         uint32(26050),
		"uuid":         "u-vless",
		"inbound_type": "chain",
		"chain_uri":    "socks5://1.2.3.4:1080#Fail",
		"access_tags":  []string{"vip"},
	})
	if err == nil {
		t.Fatal("expected REST push failure")
	}

	if _, getErr := c.GetInboundConfig("chain-fail"); getErr == nil {
		t.Error("failed chain inbound must be rolled back from map")
	}
	recs, loadErr := c.storeMgr.InboundStore().Load()
	if loadErr != nil {
		t.Fatalf("InboundStore.Load: %v", loadErr)
	}
	if len(recs) != 0 {
		t.Errorf("failed chain inbound must be rolled back from store, got %+v", recs)
	}
	if allocErr := fm.AllocateSpecificPort(26050); allocErr != nil {
		t.Errorf("port 26050 must be released back to PortClaimer on rollback: %v", allocErr)
	}
}

func TestPersistAndRestore_ChainMetadataAndEmptyInboundTypeDefault(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "persist.db")
	sm, err := store.NewStoreManager(dsn, migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })

	cfg := MihomoConfig{
		BinaryPath:         "/unused",
		ConfigFilePath:     "/unused",
		DataDir:            t.TempDir(),
		ExternalController: "127.0.0.1:9090",
		ReleaseTag:         "Prerelease-Alpha",
	}
	c1, err := NewMihomoContainer(cfg, WithStoreMgr(sm))
	if err != nil {
		t.Fatalf("NewMihomoContainer c1: %v", err)
	}

	if err := c1.FastAddInbound("chain-sh", map[string]any{
		"protocol":     "vless",
		"port":         10101,
		"uuid":         "u-1",
		"inbound_type": "chain",
		"chain_uri":    "socks5://u:p@sh.example.com:1080#SH",
		"access_tags":  []string{"vip", "home-sh"},
		"remark":       "Shanghai-Home",
	}); err != nil {
		t.Fatalf("FastAddInbound chain-sh: %v", err)
	}

	// Verify InboundStore record persisted all 4 fields explicitly.
	rec, err := sm.InboundStore().Get("chain-sh")
	if err != nil || rec == nil {
		t.Fatalf("InboundStore.Get(chain-sh) = (%+v, %v)", rec, err)
	}
	if rec.InboundType != "chain" || rec.ChainOutboundURI != "socks5://u:p@sh.example.com:1080#SH" ||
		rec.Remark != "Shanghai-Home" || !reflect.DeepEqual(rec.AccessTags, []string{"home-sh", "vip"}) {
		t.Errorf("persisted record mismatch: %+v", rec)
	}

	// Also seed a legacy direct record with empty InboundType directly in DB.
	legacyDirect := NewMihomoInbound("legacy-direct", contracts.ProtocolVMess, 10102, MihomoSharedCred{UUID: "u-legacy"})
	native, _ := legacyDirect.ToNative()
	if err := sm.InboundStore().Save(&store.InboundRecord{
		Tag:           "legacy-direct",
		ContainerType: string(contracts.ContainerMihomo),
		NativeJSON:    native,
		InboundType:   "", // empty should normalize to direct
	}); err != nil {
		t.Fatalf("Save legacy-direct: %v", err)
	}

	// Seed a corrupt chain record; Restore should skip it without failing healthy records.
	if err := sm.InboundStore().Save(&store.InboundRecord{
		Tag:              "corrupt-chain",
		ContainerType:    string(contracts.ContainerMihomo),
		NativeJSON:       native, // valid native JSON, but bad tag/chain URI
		InboundType:      "chain",
		ChainOutboundURI: "not-a-valid-uri",
	}); err != nil {
		t.Fatalf("Save corrupt-chain: %v", err)
	}

	// Restore into a fresh container instance.
	c2, err := NewMihomoContainer(cfg, WithStoreMgr(sm))
	if err != nil {
		t.Fatalf("NewMihomoContainer c2: %v", err)
	}
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c2, srv.URL())

	if err := c2.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if _, ok := c2.GetInboundMeta("corrupt-chain"); ok {
		t.Error("corrupt-chain should have been skipped during restore")
	}

	chainMeta, ok := c2.GetInboundMeta("chain-sh")
	if !ok {
		t.Fatal("chain-sh missing after restore")
	}
	if chainMeta.InboundType != contracts.InboundTypeChain ||
		chainMeta.ChainOutboundURI != "socks5://u:p@sh.example.com:1080#SH" ||
		chainMeta.Remark != "Shanghai-Home" ||
		!reflect.DeepEqual(chainMeta.AccessTags, []string{"home-sh", "vip"}) {
		t.Errorf("restored chain meta mismatch: %+v", chainMeta)
	}

	directMeta, ok := c2.GetInboundMeta("legacy-direct")
	if !ok {
		t.Fatal("legacy-direct missing after restore")
	}
	if directMeta.InboundType != contracts.InboundTypeDirect {
		t.Errorf("legacy-direct InboundType = %q, want direct", directMeta.InboundType)
	}

	// Verify restored YAML pushed to mihomo has proxies + listener.proxy + IN-NAME rule.
	srv.mu.Lock()
	lastYAML := srv.bodies[len(srv.bodies)-1]
	srv.mu.Unlock()
	var parsed map[string]any
	if err := yaml.Unmarshal(lastYAML, &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if parsed["mode"] != "rule" || parsed["ipv6"] != true {
		t.Errorf("mode/ipv6 = %v/%v, want rule/true", parsed["mode"], parsed["ipv6"])
	}
	proxies, _ := parsed["proxies"].([]any)
	if len(proxies) != 1 || proxies[0].(map[string]any)["name"] != "__chain_out__:chain-sh" {
		t.Errorf("restored proxies = %#v", proxies)
	}
	rules, _ := parsed["rules"].([]any)
	wantRules := []any{"IN-NAME,chain-sh,__chain_out__:chain-sh", "MATCH,DIRECT"}
	if !reflect.DeepEqual(rules, wantRules) {
		t.Errorf("restored rules = %#v, want %#v", rules, wantRules)
	}
}

func TestTwoLayerIsolation_UserEventAdd_And_SetInboundAccessTags(t *testing.T) {
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	// Layer 1 node group is "vip".
	um.EnableClusterSync("default", &memNodeGroupsStore{groups: []string{"vip"}})

	// One public direct inbound and one private chain inbound tagged "vip".
	if err := c.FastAddInbound("direct-pub", map[string]any{
		"protocol": "vmess",
		"port":     10201,
		"uuid":     "u-pub",
	}); err != nil {
		t.Fatalf("FastAddInbound direct-pub: %v", err)
	}
	if err := c.FastAddInbound("chain-vip", map[string]any{
		"protocol":     "vless",
		"port":         10202,
		"uuid":         "u-vip",
		"inbound_type": "chain",
		"chain_uri":    "socks5://100.64.0.2:1080#HomeVIP",
		"access_tags":  []string{"vip"},
	}); err != nil {
		t.Fatalf("FastAddInbound chain-vip: %v", err)
	}

	// Users:
	// - alice: group="vip", inbound_tags=[] -> Layer 1 OK, Layer 2 DENIED on chain-vip (same-name "vip" must NOT cross layers!)
	// - bob:   group="basic", inbound_tags=["vip"] -> Layer 1 DENIED on BOTH inbounds!
	// - carol: group="vip", inbound_tags=["vip"] -> BOTH layers OK on BOTH inbounds!
	// - dave:  group="vip", inbound_tags=["extra"] -> Layer 1 OK, initially denied on chain-vip
	for _, req := range []usermanager.AddUserRequest{
		{Username: "alice", Password: "pw", TargetGroup: "vip"},
		{Username: "bob", Password: "pw", TargetGroup: "basic", InboundTags: []string{"vip"}},
		{Username: "carol", Password: "pw", TargetGroup: "vip", InboundTags: []string{"vip"}},
		{Username: "dave", Password: "pw", TargetGroup: "vip", InboundTags: []string{"extra"}},
	} {
		if err := um.AddUser(req); err != nil {
			t.Fatalf("AddUser(%s): %v", req.Username, err)
		}
		u := um.GetUserIncludingDeleting(req.Username)
		c.handleUserEvent(usermanager.UserEvent{
			Type:     usermanager.UserEventAdd,
			Username: req.Username,
			User:     u.Clone(),
		})
	}

	// Verify initial forward rule isolation from UserEventAdd.
	assertRule := func(tag, user string, want bool) {
		t.Helper()
		rule := fwdMgr.GetRule("mihomo:" + tag + ":" + user)
		if want && rule == nil {
			t.Errorf("expected forward rule for (%s, %s)", tag, user)
		}
		if !want && rule != nil {
			t.Errorf("unexpected forward rule for (%s, %s): %+v", tag, user, rule)
		}
	}

	assertRule("direct-pub", "alice", true)
	assertRule("chain-vip", "alice", false) // same-name "vip" isolation!
	assertRule("direct-pub", "bob", false)  // UserEventAdd must NOT allocate for non-visible user!
	assertRule("chain-vip", "bob", false)
	assertRule("direct-pub", "carol", true)
	assertRule("chain-vip", "carol", true)
	assertRule("direct-pub", "dave", true)
	assertRule("chain-vip", "dave", false)

	// SetInboundAccessTags must NOT push REST config / reload mihomo.
	restCountBefore := srv.bodyCount()

	// Grant "extra" on chain-vip -> dave immediately gets forward port, alice still denied, carol preserved.
	if err := c.SetInboundAccessTags("chain-vip", []string{"vip", "extra", "vip"}); err != nil {
		t.Fatalf("SetInboundAccessTags add extra: %v", err)
	}
	if srv.bodyCount() != restCountBefore {
		t.Errorf("SetInboundAccessTags must NOT trigger REST PUT /configs (count %d -> %d)", restCountBefore, srv.bodyCount())
	}
	assertRule("chain-vip", "dave", true)
	assertRule("chain-vip", "carol", true)
	assertRule("chain-vip", "alice", false)
	assertRule("chain-vip", "bob", false)

	// Restrict to ["other-only"] -> carol and dave immediately revoked.
	if err := c.SetInboundAccessTags("chain-vip", []string{"other-only"}); err != nil {
		t.Fatalf("SetInboundAccessTags restrict: %v", err)
	}
	if srv.bodyCount() != restCountBefore {
		t.Errorf("SetInboundAccessTags must NOT trigger REST PUT /configs")
	}
	assertRule("chain-vip", "carol", false)
	assertRule("chain-vip", "dave", false)

	// Clear tags -> all Layer-1 visible users (alice, carol, dave) get ports; bob remains denied.
	if err := c.SetInboundAccessTags("chain-vip", []string{}); err != nil {
		t.Fatalf("SetInboundAccessTags clear: %v", err)
	}
	assertRule("chain-vip", "alice", true)
	assertRule("chain-vip", "carol", true)
	assertRule("chain-vip", "dave", true)
	assertRule("chain-vip", "bob", false)

	// Verify DB persistence of updated access_tags.
	rec, err := c.storeMgr.InboundStore().Get("chain-vip")
	if err != nil {
		t.Fatalf("InboundStore.Get: %v", err)
	}
	if len(rec.AccessTags) != 0 {
		t.Errorf("persisted AccessTags = %v, want empty", rec.AccessTags)
	}

	// Verify SetInboundAccessTags error paths: missing tag, invalid tag, store rollback.
	if err := c.SetInboundAccessTags("missing", []string{"vip"}); err == nil {
		t.Error("expected error for missing inbound tag")
	}
	if err := c.SetInboundAccessTags("chain-vip", []string{"bad,tag"}); err == nil {
		t.Error("expected error for invalid access tag")
	}

	// Seed a baseline tag, test UserEventUpdate Layer 2 grant/revoke, then test store rollback.
	if err := c.SetInboundAccessTags("chain-vip", []string{"vip"}); err != nil {
		t.Fatalf("SetInboundAccessTags baseline: %v", err)
	}
	assertRule("chain-vip", "alice", false)
	if err := um.SetUserInboundTags("alice", []string{"vip"}); err != nil {
		t.Fatalf("SetUserInboundTags grant alice: %v", err)
	}
	c.handleUserEvent(usermanager.UserEvent{
		Type:     usermanager.UserEventUpdate,
		Username: "alice",
		User:     um.GetUserIncludingDeleting("alice").Clone(),
	})
	assertRule("chain-vip", "alice", true)

	if err := um.SetUserInboundTags("alice", nil); err != nil {
		t.Fatalf("SetUserInboundTags revoke alice: %v", err)
	}
	c.handleUserEvent(usermanager.UserEvent{
		Type:     usermanager.UserEventUpdate,
		Username: "alice",
		User:     um.GetUserIncludingDeleting("alice").Clone(),
	})
	assertRule("chain-vip", "alice", false)

	_ = c.storeMgr.Close()
	if err := c.SetInboundAccessTags("chain-vip", []string{"rolled-back"}); err == nil {
		t.Error("expected error when store is closed")
	}
	meta, _ := c.GetInboundMeta("chain-vip")
	if !reflect.DeepEqual(meta.AccessTags, []string{"vip"}) {
		t.Errorf("AccessTags not rolled back on store failure: got %v, want [vip]", meta.AccessTags)
	}
}

func TestReconcileUsers_EnforcesLayer2AndRemovesUnauthorized(t *testing.T) {
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	um.EnableClusterSync("default", &memNodeGroupsStore{groups: []string{"vip"}})

	if err := c.FastAddInbound("chain-vip", map[string]any{
		"protocol":     "vmess",
		"port":         10301,
		"uuid":         "u-vip",
		"inbound_type": "chain",
		"chain_uri":    "socks5://100.64.0.2:1080#VIP",
		"access_tags":  []string{"vip"},
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	_ = um.AddUser(usermanager.AddUserRequest{Username: "alice", Password: "pw", TargetGroup: "vip"})
	_ = um.AddUser(usermanager.AddUserRequest{Username: "carol", Password: "pw", TargetGroup: "vip", InboundTags: []string{"vip"}})

	// Simulate stale/leaked tracking for alice (who has Layer 1 visibility but lacks Layer 2 "vip" tag).
	inb, _ := c.GetInboundConfig("chain-vip")
	mi := inb.(*MihomoInbound)
	if _, err := mi.AddUser("alice", nil); err != nil {
		t.Fatalf("seed alice: %v", err)
	}

	c.reconcileUsers()

	if mi.hasAddedUser("alice") || fwdMgr.GetRule("mihomo:chain-vip:alice") != nil {
		t.Error("reconcileUsers must remove Layer-2 unauthorized user alice")
	}
	if !mi.hasAddedUser("carol") || fwdMgr.GetRule("mihomo:chain-vip:carol") == nil {
		t.Error("reconcileUsers must add authorized user carol")
	}
}

func TestGetUserSubscriptions_IsolationAndNodeNaming(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	um.EnableClusterSync("default", &memNodeGroupsStore{groups: []string{"vip"}})

	// 1. Direct without remark (legacy naming preserved)
	if err := c.FastAddInbound("direct-plain", map[string]any{
		"protocol": "trojan",
		"port":     10401,
		"password": "pw1",
	}); err != nil {
		t.Fatalf("FastAddInbound direct-plain: %v", err)
	}
	// 2. Direct with remark
	if err := c.FastAddInbound("direct-remark", map[string]any{
		"protocol": "trojan",
		"port":     10402,
		"password": "pw2",
		"remark":   "DirectVIP",
	}); err != nil {
		t.Fatalf("FastAddInbound direct-remark: %v", err)
	}
	// 3. Chain with remark from URI fragment (plus access_tags)
	if err := c.FastAddInbound("chain-remark", map[string]any{
		"protocol":     "vless",
		"port":         10403,
		"uuid":         "u-3",
		"inbound_type": "chain",
		"chain_uri":    "socks5://100.64.0.2:1080#SH-Home",
		"access_tags":  []string{"vip"},
	}); err != nil {
		t.Fatalf("FastAddInbound chain-remark: %v", err)
	}
	// 4. Chain without remark -> falls back to inbound tag suffix
	if err := c.FastAddInbound("chain-notag-remark", map[string]any{
		"protocol":     "vless",
		"port":         10404,
		"uuid":         "u-4",
		"inbound_type": "chain",
		"chain_uri":    "socks5://100.64.0.3:1080",
		"access_tags":  []string{"vip"},
	}); err != nil {
		t.Fatalf("FastAddInbound chain-notag-remark: %v", err)
	}

	_ = um.AddUser(usermanager.AddUserRequest{Username: "alice", Password: "pw", TargetGroup: "vip"})
	_ = um.AddUser(usermanager.AddUserRequest{Username: "bob", Password: "pw", TargetGroup: "basic", InboundTags: []string{"vip"}})
	_ = um.AddUser(usermanager.AddUserRequest{Username: "carol", Password: "pw", TargetGroup: "vip", InboundTags: []string{"vip"}})

	for _, name := range []string{"alice", "bob", "carol"} {
		u := um.GetUserIncludingDeleting(name)
		c.handleUserEvent(usermanager.UserEvent{Type: usermanager.UserEventAdd, Username: name, User: u.Clone()})
	}

	// Pass sparse UserSpec{Username: ...} (like RPC GetSub does) to verify
	// GetUserSubscriptions consults UserManager.CanUserAccessInboundByUsername.
	bobSpecs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     contracts.UserSpec{Username: "bob"},
		Host:     "edge.example.com",
		NodeName: "hk-01",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions(bob): %v", err)
	}
	if len(bobSpecs) != 0 {
		t.Errorf("bob (Layer 1 fail) must receive 0 specs, got %d", len(bobSpecs))
	}

	aliceSpecs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     contracts.UserSpec{Username: "alice"},
		Host:     "edge.example.com",
		NodeName: "hk-01",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions(alice): %v", err)
	}
	if len(aliceSpecs) != 2 {
		t.Fatalf("alice must receive 2 direct specs only, got %d", len(aliceSpecs))
	}

	carolSpecs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     contracts.UserSpec{Username: "carol"},
		Host:     "edge.example.com",
		NodeName: "hk-01",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions(carol): %v", err)
	}
	if len(carolSpecs) != 4 {
		t.Fatalf("carol must receive 4 specs, got %d", len(carolSpecs))
	}

	gotNames := map[string]string{}
	for _, s := range carolSpecs {
		gotNames[s.InboundTag] = s.NodeName
		u, parseErr := url.Parse(s.URI)
		if parseErr != nil {
			t.Fatalf("parse URI for %s: %v", s.InboundTag, parseErr)
		}
		if u.Fragment != s.NodeName {
			t.Errorf("URI fragment = %q, want %q", u.Fragment, s.NodeName)
		}
	}
	wantNames := map[string]string{
		"direct-plain":       "hk-01",
		"direct-remark":      "hk-01-DirectVIP",
		"chain-remark":       "hk-01-SH-Home",
		"chain-notag-remark": "hk-01-chain-notag-remark",
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("NodeNames with prefix = %v, want %v", gotNames, wantNames)
	}

	// Empty req.NodeName case
	carolNoNode, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "carol"},
		Host: "edge.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions(carol, empty NodeName): %v", err)
	}
	gotNoNode := map[string]string{}
	for _, s := range carolNoNode {
		gotNoNode[s.InboundTag] = s.NodeName
	}
	wantNoNode := map[string]string{
		"direct-plain":       "direct-plain",
		"direct-remark":      "DirectVIP",
		"chain-remark":       "SH-Home",
		"chain-notag-remark": "chain-notag-remark",
	}
	if !reflect.DeepEqual(gotNoNode, wantNoNode) {
		t.Errorf("NodeNames without prefix = %v, want %v", gotNoNode, wantNoNode)
	}
}

func TestSetInboundAccessTags_ConcurrentAccessSafe(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := c.FastAddInbound("chain-conc", map[string]any{
		"protocol":     "vless",
		"port":         10501,
		"uuid":         "u-conc",
		"inbound_type": "chain",
		"chain_uri":    "socks5://100.64.0.2:1080#Conc",
		"access_tags":  []string{"t1"},
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	_ = um.AddUser(usermanager.AddUserRequest{Username: "u1", Password: "pw", InboundTags: []string{"t1"}})
	_ = um.AddUser(usermanager.AddUserRequest{Username: "u2", Password: "pw", InboundTags: []string{"t2"}})

	var wg sync.WaitGroup
	tagSets := [][]string{{"t1"}, {"t2"}, {"t1", "t2"}, nil}
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				_ = c.SetInboundAccessTags("chain-conc", tagSets[(id+i)%len(tagSets)])
				meta, ok := c.GetInboundMeta("chain-conc")
				if !ok || meta.InboundType != contracts.InboundTypeChain {
					t.Errorf("unexpected meta during concurrent read: %+v, ok=%v", meta, ok)
				}
				_ = slices.Clone(meta.AccessTags)
				_, _ = c.GetUserSubscriptions(contracts.SubscriptionRequest{
					User: contracts.UserSpec{Username: "u1"},
					Host: "edge.example.com",
				})
				c.reconcileUsers()
			}
		}(g)
	}
	wg.Wait()
}
