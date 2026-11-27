package snell

import (
	"context"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

type memNodeGroupsStore struct {
	mu     sync.Mutex
	groups []string
}

func (s *memNodeGroupsStore) List() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.groups))
	copy(out, s.groups)
	return out, nil
}

func (s *memNodeGroupsStore) Set(groups []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups = make([]string, len(groups))
	copy(s.groups, groups)
	return nil
}

func newIsolationTestDeps(t *testing.T, groups []string) (*store.StoreManager, *forward.DefaultForwardManager, *usermanager.UserManager) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	sm, err := store.NewStoreManager(dsn, migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })

	fm, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort: 25000,
		MaxPort: 25999,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	t.Cleanup(func() { _ = fm.Close() })

	um, err := usermanager.NewUserManagerWithStore(fm, sm, "test-node")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}
	if len(groups) > 0 {
		um.EnableClusterSync("default", &memNodeGroupsStore{groups: groups})
	}
	return sm, fm, um
}

func waitRuleState(t *testing.T, fm forward.ForwardManager, key string, wantPresent bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		present := fm.GetRule(key) != nil
		if present == wantPresent {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("rule %q presence = %v, want %v", key, !wantPresent, wantPresent)
}

func TestSnell_ChainCapable(t *testing.T) {
	sc, err := NewSnellContainer(SnellConfig{Listen: "127.0.0.1", Port: 16160, PSK: "testpsk"})
	if err != nil {
		t.Fatalf("NewSnellContainer: %v", err)
	}

	var c container.Container = sc
	cc, ok := c.(container.ChainCapable)
	if !ok {
		t.Fatal("SnellContainer must implement container.ChainCapable")
	}
	if cc.SupportsChainOutbound() {
		t.Fatal("SupportsChainOutbound() = true, want false")
	}
	err = cc.ValidateChainURI("socks5://1.2.3.4:1080")
	if err == nil {
		t.Fatal("ValidateChainURI should return error")
	}
	if !strings.Contains(err.Error(), "Mode A") {
		t.Fatalf("unexpected ValidateChainURI error: %v", err)
	}
}

func TestSnell_FastAddInbound_RejectsChainAndPersistsMeta(t *testing.T) {
	sm, _, _ := newIsolationTestDeps(t, nil)
	sc, err := NewSnellContainer(
		SnellConfig{Listen: "127.0.0.1", Port: 16160, PSK: "testpsk"},
		WithStoreMgr(sm),
	)
	if err != nil {
		t.Fatalf("NewSnellContainer: %v", err)
	}

	// Reject chain requests across tag/param variants.
	for _, tc := range []struct {
		name   string
		tag    string
		params map[string]any
	}{
		{
			name:   "chain inbound_type on default tag",
			tag:    defaultInboundTag,
			params: map[string]any{"inbound_type": "chain", "chain_uri": "socks5://1.2.3.4:1080"},
		},
		{
			name:   "chain inbound_type on custom tag",
			tag:    "custom-chain",
			params: map[string]any{"inbound_type": "chain", "chain_uri": "socks5://1.2.3.4:1080"},
		},
		{
			name:   "direct inbound_type with non-empty chain_uri",
			tag:    defaultInboundTag,
			params: map[string]any{"inbound_type": "direct", "chain_uri": "socks5://1.2.3.4:1080"},
		},
		{
			name:   "empty inbound_type with non-empty chain_uri",
			tag:    defaultInboundTag,
			params: map[string]any{"chain_uri": "trojan://pass@1.2.3.4:443"},
		},
		{
			name:   "invalid inbound_type",
			tag:    defaultInboundTag,
			params: map[string]any{"inbound_type": "unknown"},
		},
		{
			name:   "invalid access_tags",
			tag:    defaultInboundTag,
			params: map[string]any{"access_tags": []string{"bad tag!"}},
		},
		{
			name:   "non-default tag",
			tag:    "snell-other",
			params: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := sc.FastAddInbound(tc.tag, tc.params); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}

	// Valid direct FastAddInbound with access_tags and remark.
	if err := sc.FastAddInbound(defaultInboundTag, map[string]any{
		"inbound_type": "direct",
		"access_tags":  []string{" vip ", "home-bj", "vip", ""},
		"remark":       "  BJ Snell  ",
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	var mp container.InboundMetaProvider = sc
	meta, ok := mp.GetInboundMeta(defaultInboundTag)
	if !ok {
		t.Fatal("GetInboundMeta returned false")
	}
	if meta.InboundType != contracts.InboundTypeDirect {
		t.Errorf("InboundType = %q, want direct", meta.InboundType)
	}
	if !reflect.DeepEqual(meta.AccessTags, []string{"home-bj", "vip"}) {
		t.Errorf("AccessTags = %v, want [home-bj vip]", meta.AccessTags)
	}
	if meta.Remark != "BJ Snell" {
		t.Errorf("Remark = %q, want BJ Snell", meta.Remark)
	}

	// Mutating returned slice must not affect internal state.
	meta.AccessTags[0] = "tampered"
	meta2, _ := mp.GetInboundMeta(defaultInboundTag)
	if !reflect.DeepEqual(meta2.AccessTags, []string{"home-bj", "vip"}) {
		t.Errorf("internal AccessTags mutated: %v", meta2.AccessTags)
	}

	// Verify DB persistence.
	rec, err := sm.InboundStore().Get(defaultInboundTag)
	if err != nil {
		t.Fatalf("InboundStore.Get: %v", err)
	}
	if rec == nil {
		t.Fatal("expected persisted record")
	}
	if rec.InboundType != "direct" || rec.Remark != "BJ Snell" || !reflect.DeepEqual(rec.AccessTags, []string{"home-bj", "vip"}) {
		t.Errorf("unexpected persisted record: %+v", rec)
	}

	// Missing tag returns false.
	if _, ok := mp.GetInboundMeta("other"); ok {
		t.Error("GetInboundMeta(other) should return false")
	}

	// Disabled inbound hides meta and rejects SetInboundAccessTags.
	if err := sc.RemoveInboundConfig(defaultInboundTag); err != nil {
		t.Fatalf("RemoveInboundConfig: %v", err)
	}
	if _, ok := mp.GetInboundMeta(defaultInboundTag); ok {
		t.Error("GetInboundMeta should return false when disabled")
	}
	if err := sc.SetInboundAccessTags(defaultInboundTag, []string{"vip"}); err == nil {
		t.Error("SetInboundAccessTags should fail when disabled")
	}
}

func TestSnell_UserEventAddAndTwoLayerIsolation(t *testing.T) {
	dir := t.TempDir()
	bin, pidFile := writeFakeSnellBinary(t, dir)
	sm, fm, um := newIsolationTestDeps(t, []string{"vip"})

	cfg := newLifecycleTestConfig(dir, bin)
	sc, err := NewSnellContainer(cfg, WithStoreMgr(sm), WithUserManager(um))
	if err != nil {
		t.Fatalf("NewSnellContainer: %v", err)
	}
	if err := sc.FastAddInbound(defaultInboundTag, map[string]any{
		"access_tags": []string{"vip"},
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	if err := sc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sc.Stop() }()
	waitPidAlive(t, pidFile, 5*time.Second)

	// 1. Bob: group="basic" (fails Layer 1), inbound_tags=["vip"] -> MUST BE REJECTED on UserEventAdd!
	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "bob",
		Password:    "pw",
		TargetGroup: "basic",
		InboundTags: []string{"vip"},
	}); err != nil {
		t.Fatalf("AddUser bob: %v", err)
	}

	// 2. Alice: group="vip" (passes Layer 1), inbound_tags=[] -> MUST BE REJECTED (same-name "vip" isolation)!
	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "alice",
		Password:    "pw",
		TargetGroup: "vip",
	}); err != nil {
		t.Fatalf("AddUser alice: %v", err)
	}

	// 3. Carol: group="vip", inbound_tags=["vip"] -> MUST BE ALLOCATED on UserEventAdd!
	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "carol",
		Password:    "pw",
		TargetGroup: "vip",
		InboundTags: []string{"vip"},
	}); err != nil {
		t.Fatalf("AddUser carol: %v", err)
	}

	carolKey := string(contracts.ContainerSnell) + ":" + defaultInboundTag + ":carol"
	aliceKey := string(contracts.ContainerSnell) + ":" + defaultInboundTag + ":alice"
	bobKey := string(contracts.ContainerSnell) + ":" + defaultInboundTag + ":bob"

	waitRuleState(t, fm, carolKey, true)
	time.Sleep(80 * time.Millisecond)
	if fm.GetRule(bobKey) != nil {
		t.Fatalf("bob (Layer 1 denied) leaked forward rule on UserEventAdd")
	}
	if fm.GetRule(aliceKey) != nil {
		t.Fatalf("alice (Layer 2 denied despite same-name vip group) leaked forward rule on UserEventAdd")
	}

	// Dynamic UserEventUpdate: grant alice ["vip"] -> rule allocated; revoke carol -> rule released.
	if err := um.SetUserInboundTags("alice", []string{"vip"}); err != nil {
		t.Fatalf("SetUserInboundTags alice: %v", err)
	}
	waitRuleState(t, fm, aliceKey, true)

	if err := um.SetUserInboundTags("carol", nil); err != nil {
		t.Fatalf("SetUserInboundTags carol: %v", err)
	}
	waitRuleState(t, fm, carolKey, false)

	// Dynamic UserEventUpdate: move alice to group="basic" -> fails Layer 1 -> rule released.
	if err := um.SetUserGroup("alice", "basic"); err != nil {
		t.Fatalf("SetUserGroup alice: %v", err)
	}
	waitRuleState(t, fm, aliceKey, false)
}

func TestSnell_SetInboundAccessTags_DynamicNoRestartAndRollback(t *testing.T) {
	dir := t.TempDir()
	bin, pidFile := writeFakeSnellBinary(t, dir)
	sm, fm, um := newIsolationTestDeps(t, []string{"vip"})

	cfg := newLifecycleTestConfig(dir, bin)
	sc, err := NewSnellContainer(cfg, WithStoreMgr(sm), WithUserManager(um))
	if err != nil {
		t.Fatalf("NewSnellContainer: %v", err)
	}

	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "alice",
		Password:    "pw",
		TargetGroup: "vip",
	}); err != nil {
		t.Fatalf("AddUser alice: %v", err)
	}
	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "bob",
		Password:    "pw",
		TargetGroup: "basic",
		InboundTags: []string{"vip"},
	}); err != nil {
		t.Fatalf("AddUser bob: %v", err)
	}
	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "carol",
		Password:    "pw",
		TargetGroup: "vip",
		InboundTags: []string{"vip"},
	}); err != nil {
		t.Fatalf("AddUser carol: %v", err)
	}

	if err := sc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sc.Stop() }()
	pid1 := waitPidAlive(t, pidFile, 5*time.Second)

	if err := sc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	aliceKey := string(contracts.ContainerSnell) + ":" + defaultInboundTag + ":alice"
	bobKey := string(contracts.ContainerSnell) + ":" + defaultInboundTag + ":bob"
	carolKey := string(contracts.ContainerSnell) + ":" + defaultInboundTag + ":carol"

	// Initially untagged: alice & carol allocated, bob denied.
	if fm.GetRule(aliceKey) == nil || fm.GetRule(carolKey) == nil {
		t.Fatalf("expected alice and carol rules initially")
	}
	if fm.GetRule(bobKey) != nil {
		t.Fatalf("bob must not have a rule initially")
	}
	carolPort := fm.GetRule(carolKey).ListenPort

	// Narrow to ["vip"]: alice immediately revoked, carol preserved, NO process restart.
	if err := sc.SetInboundAccessTags(defaultInboundTag, []string{" vip ", "home", "vip"}); err != nil {
		t.Fatalf("SetInboundAccessTags narrow: %v", err)
	}
	if fm.GetRule(aliceKey) != nil {
		t.Fatal("alice rule should be revoked immediately after SetInboundAccessTags")
	}
	if fm.GetRule(bobKey) != nil {
		t.Fatal("bob rule must remain absent")
	}
	if r := fm.GetRule(carolKey); r == nil || r.ListenPort != carolPort {
		t.Fatalf("carol rule/port changed unexpectedly: %+v (want port %d)", r, carolPort)
	}
	pidAfter, ok := readPidFile(pidFile)
	if !ok || pidAfter != pid1 || !processAlive(pid1) {
		t.Fatalf("process restarted across SetInboundAccessTags: pid1=%d pidAfter=%d alive=%v", pid1, pidAfter, processAlive(pid1))
	}

	// Clear tags back to public: alice immediately gets a port again.
	if err := sc.SetInboundAccessTags(defaultInboundTag, nil); err != nil {
		t.Fatalf("SetInboundAccessTags clear: %v", err)
	}
	if fm.GetRule(aliceKey) == nil {
		t.Fatal("alice rule should be re-allocated immediately after clearing tags")
	}
	if fm.GetRule(bobKey) != nil {
		t.Fatal("bob rule must remain absent (fails Layer 1)")
	}

	// Validation errors.
	if err := sc.SetInboundAccessTags("wrong-tag", []string{"vip"}); err == nil {
		t.Error("expected error for wrong tag")
	}
	if err := sc.SetInboundAccessTags(defaultInboundTag, []string{"bad/tag"}); err == nil {
		t.Error("expected error for invalid tag")
	}

	// Store failure rollback: set ["vip"], close DB, attempt update -> error & rollback to ["vip"].
	if err := sc.SetInboundAccessTags(defaultInboundTag, []string{"vip"}); err != nil {
		t.Fatalf("SetInboundAccessTags [vip]: %v", err)
	}
	_ = sm.Close()
	if err := sc.SetInboundAccessTags(defaultInboundTag, []string{"other"}); err == nil {
		t.Fatal("expected error when store Save fails")
	}
	meta, ok := sc.GetInboundMeta(defaultInboundTag)
	if !ok || !reflect.DeepEqual(meta.AccessTags, []string{"vip"}) {
		t.Fatalf("AccessTags did not roll back on store error: %+v (ok=%v)", meta, ok)
	}
}

func TestSnell_GetUserSubscriptions_GatingAndRemark(t *testing.T) {
	// nil userMgr must return nil, nil without panicking.
	noMgr, err := NewSnellContainer(SnellConfig{Listen: "127.0.0.1", Port: 16160, PSK: "testpsk"})
	if err != nil {
		t.Fatalf("NewSnellContainer: %v", err)
	}
	if subs, err := noMgr.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "u1"},
		Host: "1.2.3.4",
	}); err != nil || subs != nil {
		t.Fatalf("nil userMgr GetUserSubscriptions = (%v, %v), want (nil, nil)", subs, err)
	}

	sm, _, um := newIsolationTestDeps(t, []string{"vip"})
	sc, err := NewSnellContainer(
		SnellConfig{Listen: "127.0.0.1", Port: 16160, PSK: "testpsk"},
		WithStoreMgr(sm),
		WithUserManager(um),
	)
	if err != nil {
		t.Fatalf("NewSnellContainer: %v", err)
	}
	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "alice",
		Password:    "pw",
		TargetGroup: "vip",
	}); err != nil {
		t.Fatalf("AddUser alice: %v", err)
	}
	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "bob",
		Password:    "pw",
		TargetGroup: "basic",
		InboundTags: []string{"vip"},
	}); err != nil {
		t.Fatalf("AddUser bob: %v", err)
	}
	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "carol",
		Password:    "pw",
		TargetGroup: "vip",
		InboundTags: []string{"vip"},
	}); err != nil {
		t.Fatalf("AddUser carol: %v", err)
	}

	if err := sc.FastAddInbound(defaultInboundTag, map[string]any{
		"access_tags": []string{"vip"},
		"remark":      "BJ-Home",
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	// Even if a port mapping existed and caller forged req.User tags,
	// CanUserAccessInboundByUsername must reject alice and bob.
	um.SetPortMappingForTest("alice", 16160, 25111)
	um.SetPortMappingForTest("bob", 16160, 25112)

	for _, uname := range []string{"alice", "bob", "ghost"} {
		subs, err := sc.GetUserSubscriptions(contracts.SubscriptionRequest{
			User: contracts.UserSpec{
				Username:    uname,
				TargetGroup: "vip",
				InboundTags: []string{"vip"},
			},
			Host:     "example.com",
			NodeName: "Tokyo",
		})
		if err != nil {
			t.Fatalf("GetUserSubscriptions(%s): %v", uname, err)
		}
		if len(subs) != 0 {
			t.Fatalf("expected 0 subscriptions for denied user %s, got %+v", uname, subs)
		}
	}

	subs, err := sc.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     contracts.UserSpec{Username: "carol"},
		Host:     "example.com",
		NodeName: "Tokyo",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions(carol): %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription for carol, got %d", len(subs))
	}
	if subs[0].NodeName != "Tokyo-BJ-Home" {
		t.Errorf("NodeName = %q, want Tokyo-BJ-Home", subs[0].NodeName)
	}
	if !strings.HasSuffix(subs[0].URI, "#Tokyo-BJ-Home") {
		t.Errorf("URI = %q, expected #Tokyo-BJ-Home suffix", subs[0].URI)
	}

	// Empty remark keeps req.NodeName unchanged.
	if err := sc.FastAddInbound(defaultInboundTag, map[string]any{
		"access_tags": []string{"vip"},
	}); err != nil {
		t.Fatalf("FastAddInbound empty remark: %v", err)
	}
	subs, err = sc.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     contracts.UserSpec{Username: "carol"},
		Host:     "example.com",
		NodeName: "Tokyo",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions(carol, no remark): %v", err)
	}
	if len(subs) != 1 || subs[0].NodeName != "Tokyo" {
		t.Fatalf("unexpected subs without remark: %+v", subs)
	}

	// Remark with spaces must use %20 (RFC 3986 fragment encoding, not '+') and round-trip via url.Parse.
	if err := sc.FastAddInbound(defaultInboundTag, map[string]any{
		"remark":      "BJ Home 01",
		"access_tags": []string{"vip"},
	}); err != nil {
		t.Fatalf("FastAddInbound spaced remark: %v", err)
	}
	subs, err = sc.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     contracts.UserSpec{Username: "carol"},
		Host:     "example.com",
		NodeName: "Edge 1",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions(carol, spaced): %v", err)
	}
	if len(subs) != 1 || subs[0].NodeName != "Edge 1-BJ Home 01" {
		t.Fatalf("unexpected spaced subs: %+v", subs)
	}
	if strings.Contains(subs[0].URI, "+") {
		t.Errorf("URI fragment must not contain '+' for spaces: %q", subs[0].URI)
	}
	parsed, parseErr := url.Parse(subs[0].URI)
	if parseErr != nil {
		t.Fatalf("url.Parse(%q): %v", subs[0].URI, parseErr)
	}
	if parsed.Fragment != subs[0].NodeName {
		t.Errorf("parsed.Fragment = %q, want %q", parsed.Fragment, subs[0].NodeName)
	}
}

func TestSnell_StorePersistenceAndRestore(t *testing.T) {
	sm, fm, um := newIsolationTestDeps(t, []string{"vip"})
	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "alice",
		Password:    "pw",
		TargetGroup: "vip",
	}); err != nil {
		t.Fatalf("AddUser alice: %v", err)
	}
	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "carol",
		Password:    "pw",
		TargetGroup: "vip",
		InboundTags: []string{"vip"},
	}); err != nil {
		t.Fatalf("AddUser carol: %v", err)
	}

	// Container 1 saves metadata via FastAddInbound + SetInboundAccessTags.
	sc1, err := NewSnellContainer(
		SnellConfig{Listen: "127.0.0.1", Port: 16160, PSK: "testpsk"},
		WithStoreMgr(sm),
	)
	if err != nil {
		t.Fatalf("NewSnellContainer 1: %v", err)
	}
	if err := sc1.FastAddInbound(defaultInboundTag, map[string]any{
		"access_tags": []string{"alpha", "vip"},
		"remark":      "Edge-Snell",
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	if err := sc1.SetInboundAccessTags(defaultInboundTag, []string{"vip"}); err != nil {
		t.Fatalf("SetInboundAccessTags: %v", err)
	}

	// Container 2 simulates restart reading from same StoreManager.
	sc2, err := NewSnellContainer(
		SnellConfig{Listen: "127.0.0.1", Port: 16160, PSK: "testpsk"},
		WithStoreMgr(sm),
		WithUserManager(um),
	)
	if err != nil {
		t.Fatalf("NewSnellContainer 2: %v", err)
	}
	if err := sc2.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	meta, ok := sc2.GetInboundMeta(defaultInboundTag)
	if !ok {
		t.Fatal("GetInboundMeta on restored container returned false")
	}
	if meta.InboundType != contracts.InboundTypeDirect || !reflect.DeepEqual(meta.AccessTags, []string{"vip"}) || meta.Remark != "Edge-Snell" {
		t.Fatalf("restored meta mismatch: %+v", meta)
	}

	aliceKey := string(contracts.ContainerSnell) + ":" + defaultInboundTag + ":alice"
	carolKey := string(contracts.ContainerSnell) + ":" + defaultInboundTag + ":carol"
	if fm.GetRule(aliceKey) != nil {
		t.Fatal("restored container must not allocate rule for alice")
	}
	if fm.GetRule(carolKey) == nil {
		t.Fatal("restored container must allocate rule for carol")
	}

	// Persist disabled state across restart.
	if err := sc2.RemoveInboundConfig(defaultInboundTag); err != nil {
		t.Fatalf("RemoveInboundConfig: %v", err)
	}
	sc3, err := NewSnellContainer(
		SnellConfig{Listen: "127.0.0.1", Port: 16160, PSK: "testpsk"},
		WithStoreMgr(sm),
		WithUserManager(um),
	)
	if err != nil {
		t.Fatalf("NewSnellContainer 3: %v", err)
	}
	if err := sc3.Restore(context.Background()); err != nil {
		t.Fatalf("Restore 3: %v", err)
	}
	if _, ok := sc3.GetInboundMeta(defaultInboundTag); ok {
		t.Fatal("disabled inbound should remain disabled after restore")
	}
	if len(sc3.ListInboundConfigs()) != 0 {
		t.Fatal("expected empty ListInboundConfigs for disabled inbound after restore")
	}
	if fm.GetRule(carolKey) != nil {
		t.Fatal("disabled inbound must not allocate rules after restore")
	}
}

func TestSnell_CorruptStoreFailClosed(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{
			name:  "corrupt access_tags JSON",
			query: `UPDATE inbounds SET access_tags = '{not-json' WHERE tag = ?`,
		},
		{
			name:  "invalid access_tags value",
			query: `UPDATE inbounds SET access_tags = '["bad,tag"]' WHERE tag = ?`,
		},
		{
			name:  "corrupt native_json",
			query: `UPDATE inbounds SET native_json = '{not-json' WHERE tag = ?`,
		},
		{
			name:  "corrupt non-boolean enabled field",
			query: `UPDATE inbounds SET native_json = '{"tag":"snell-default","enabled":"false","port":16160}' WHERE tag = ?`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sm, fm, um := newIsolationTestDeps(t, []string{"vip"})
			if err := um.AddUser(usermanager.AddUserRequest{
				Username:    "carol",
				Password:    "pw",
				TargetGroup: "vip",
				InboundTags: []string{"vip"},
			}); err != nil {
				t.Fatalf("AddUser: %v", err)
			}

			sc, err := NewSnellContainer(
				SnellConfig{Listen: "127.0.0.1", Port: 16160, PSK: "testpsk"},
				WithStoreMgr(sm),
				WithUserManager(um),
			)
			if err != nil {
				t.Fatalf("NewSnellContainer: %v", err)
			}
			if err := sc.FastAddInbound(defaultInboundTag, map[string]any{
				"access_tags": []string{"vip"},
			}); err != nil {
				t.Fatalf("FastAddInbound: %v", err)
			}
			if err := sc.Restore(context.Background()); err != nil {
				t.Fatalf("initial Restore: %v", err)
			}
			carolKey := string(contracts.ContainerSnell) + ":" + defaultInboundTag + ":carol"
			if fm.GetRule(carolKey) == nil {
				t.Fatal("expected initial rule for carol")
			}

			if _, err := sm.DB().DB().Exec(tc.query, defaultInboundTag); err != nil {
				t.Fatalf("corrupt SQL: %v", err)
			}

			if err := sc.Restore(context.Background()); err == nil {
				t.Fatalf("expected Restore error for %s", tc.name)
			}
			if _, ok := sc.GetInboundMeta(defaultInboundTag); ok {
				t.Fatal("GetInboundMeta must return false (Fail-Closed) on corrupt persisted record")
			}
			if len(sc.ListInboundConfigs()) != 0 {
				t.Fatal("ListInboundConfigs must be empty (Fail-Closed)")
			}
			if fm.GetRule(carolKey) != nil {
				t.Fatal("existing forward rules must be revoked when Fail-Closed")
			}
			subs, err := sc.GetUserSubscriptions(contracts.SubscriptionRequest{
				User: contracts.UserSpec{Username: "carol"},
				Host: "example.com",
			})
			if err != nil {
				t.Fatalf("GetUserSubscriptions: %v", err)
			}
			if len(subs) != 0 {
				t.Fatalf("expected 0 subscriptions when Fail-Closed, got %+v", subs)
			}
		})
	}
}

func TestSnell_UserEventExpireRevokesRule(t *testing.T) {
	sm, fm, um := newIsolationTestDeps(t, []string{"vip"})
	sc, err := NewSnellContainer(
		SnellConfig{Listen: "127.0.0.1", Port: 16160, PSK: "testpsk"},
		WithStoreMgr(sm),
		WithUserManager(um),
	)
	if err != nil {
		t.Fatalf("NewSnellContainer: %v", err)
	}
	if err := um.AddUser(usermanager.AddUserRequest{
		Username:    "carol",
		Password:    "pw",
		TargetGroup: "vip",
	}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if err := sc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	carolKey := string(contracts.ContainerSnell) + ":" + defaultInboundTag + ":carol"
	if fm.GetRule(carolKey) == nil {
		t.Fatal("expected initial rule for carol")
	}

	sc.handleUserEvent(usermanager.UserEvent{
		Type:     usermanager.UserEventExpire,
		Username: "carol",
	})
	if fm.GetRule(carolKey) != nil {
		t.Fatal("UserEventExpire must immediately revoke carol's forward rule")
	}
}
