package hysteria

import (
	"context"
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
		MinPort: 24000,
		MaxPort: 24999,
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

func TestHysteria_ChainCapable(t *testing.T) {
	hc, err := NewHysteriaContainer(HysteriaConfig{Listen: "127.0.0.1", Port: 9443})
	if err != nil {
		t.Fatalf("NewHysteriaContainer: %v", err)
	}

	var c container.Container = hc
	cc, ok := c.(container.ChainCapable)
	if !ok {
		t.Fatal("HysteriaContainer must implement container.ChainCapable")
	}
	if cc.SupportsChainOutbound() {
		t.Fatal("SupportsChainOutbound() = true, want false")
	}
	err = cc.ValidateChainURI("socks5://1.2.3.4:1080")
	if err == nil {
		t.Fatal("ValidateChainURI should return error")
	}
	if !strings.Contains(err.Error(), "Mode A") || !strings.Contains(err.Error(), "mihomo") {
		t.Fatalf("unexpected ValidateChainURI error: %v", err)
	}
}

func TestHysteria_FastAddInbound_RejectsChainAndPersistsMeta(t *testing.T) {
	sm, _, _ := newIsolationTestDeps(t, nil)
	hc, err := NewHysteriaContainer(
		HysteriaConfig{Listen: "127.0.0.1", Port: 9443},
		WithStoreMgr(sm),
	)
	if err != nil {
		t.Fatalf("NewHysteriaContainer: %v", err)
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
			params: map[string]any{"chain_uri": "hy2://pass@1.2.3.4:443"},
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := hc.FastAddInbound(tc.tag, tc.params); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}

	// Valid direct FastAddInbound with access_tags and remark.
	if err := hc.FastAddInbound(defaultInboundTag, map[string]any{
		"inbound_type": "direct",
		"access_tags":  []string{" vip ", "home-sh", "vip", ""},
		"remark":       "  HK Hy2  ",
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	var mp container.InboundMetaProvider = hc
	meta, ok := mp.GetInboundMeta(defaultInboundTag)
	if !ok {
		t.Fatal("GetInboundMeta returned false")
	}
	if meta.InboundType != contracts.InboundTypeDirect {
		t.Errorf("InboundType = %q, want direct", meta.InboundType)
	}
	if !reflect.DeepEqual(meta.AccessTags, []string{"home-sh", "vip"}) {
		t.Errorf("AccessTags = %v, want [home-sh vip]", meta.AccessTags)
	}
	if meta.Remark != "HK Hy2" {
		t.Errorf("Remark = %q, want HK Hy2", meta.Remark)
	}

	// Mutating returned slice must not affect internal state.
	meta.AccessTags[0] = "tampered"
	meta2, _ := mp.GetInboundMeta(defaultInboundTag)
	if !reflect.DeepEqual(meta2.AccessTags, []string{"home-sh", "vip"}) {
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
	if rec.InboundType != "direct" || rec.Remark != "HK Hy2" || !reflect.DeepEqual(rec.AccessTags, []string{"home-sh", "vip"}) {
		t.Errorf("unexpected persisted record: %+v", rec)
	}

	// Missing tag returns false.
	if _, ok := mp.GetInboundMeta("other"); ok {
		t.Error("GetInboundMeta(other) should return false")
	}

	// Disabled inbound hides meta and rejects SetInboundAccessTags.
	if err := hc.RemoveInboundConfig(defaultInboundTag); err != nil {
		t.Fatalf("RemoveInboundConfig: %v", err)
	}
	if _, ok := mp.GetInboundMeta(defaultInboundTag); ok {
		t.Error("GetInboundMeta should return false when disabled")
	}
	if err := hc.SetInboundAccessTags(defaultInboundTag, []string{"vip"}); err == nil {
		t.Error("SetInboundAccessTags should fail when disabled")
	}
}

func TestHysteria_UserEventAddAndTwoLayerIsolation(t *testing.T) {
	dir := t.TempDir()
	bin, pidFile := writeFakeHysteriaBinary(t, dir)
	sm, fm, um := newIsolationTestDeps(t, []string{"vip"})

	cfg := newLifecycleTestConfig(dir, bin)
	cfg.CertFile = filepath.Join(dir, "cert.pem")
	cfg.KeyFile = filepath.Join(dir, "key.pem")

	hc, err := NewHysteriaContainer(cfg, WithStoreMgr(sm), WithUserManager(um))
	if err != nil {
		t.Fatalf("NewHysteriaContainer: %v", err)
	}
	if err := hc.FastAddInbound(defaultInboundTag, map[string]any{
		"access_tags": []string{"vip"},
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	if err := hc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = hc.Stop() }()
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

	carolKey := string(contracts.ContainerHysteria) + ":" + defaultInboundTag + ":carol"
	aliceKey := string(contracts.ContainerHysteria) + ":" + defaultInboundTag + ":alice"
	bobKey := string(contracts.ContainerHysteria) + ":" + defaultInboundTag + ":bob"

	waitRuleState(t, fm, carolKey, true)
	// Give event loop time to process bob/alice if any leak existed, then verify zero ports.
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

func TestHysteria_SetInboundAccessTags_DynamicNoRestartAndRollback(t *testing.T) {
	dir := t.TempDir()
	bin, pidFile := writeFakeHysteriaBinary(t, dir)
	sm, fm, um := newIsolationTestDeps(t, []string{"vip"})

	cfg := newLifecycleTestConfig(dir, bin)
	cfg.CertFile = filepath.Join(dir, "cert.pem")
	cfg.KeyFile = filepath.Join(dir, "key.pem")

	hc, err := NewHysteriaContainer(cfg, WithStoreMgr(sm), WithUserManager(um))
	if err != nil {
		t.Fatalf("NewHysteriaContainer: %v", err)
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

	if err := hc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = hc.Stop() }()
	pid1 := waitPidAlive(t, pidFile, 5*time.Second)

	if err := hc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	aliceKey := string(contracts.ContainerHysteria) + ":" + defaultInboundTag + ":alice"
	bobKey := string(contracts.ContainerHysteria) + ":" + defaultInboundTag + ":bob"
	carolKey := string(contracts.ContainerHysteria) + ":" + defaultInboundTag + ":carol"

	// Initially untagged: alice & carol allocated, bob denied.
	if fm.GetRule(aliceKey) == nil || fm.GetRule(carolKey) == nil {
		t.Fatalf("expected alice and carol rules initially")
	}
	if fm.GetRule(bobKey) != nil {
		t.Fatalf("bob must not have a rule initially")
	}
	carolPort := fm.GetRule(carolKey).ListenPort

	// Narrow to ["vip"]: alice immediately revoked, carol preserved, NO process restart.
	if err := hc.SetInboundAccessTags(defaultInboundTag, []string{" vip ", "home", "vip"}); err != nil {
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
	if err := hc.SetInboundAccessTags(defaultInboundTag, nil); err != nil {
		t.Fatalf("SetInboundAccessTags clear: %v", err)
	}
	if fm.GetRule(aliceKey) == nil {
		t.Fatal("alice rule should be re-allocated immediately after clearing tags")
	}
	if fm.GetRule(bobKey) != nil {
		t.Fatal("bob rule must remain absent (fails Layer 1)")
	}

	// Validation errors.
	if err := hc.SetInboundAccessTags("wrong-tag", []string{"vip"}); err == nil {
		t.Error("expected error for wrong tag")
	}
	if err := hc.SetInboundAccessTags(defaultInboundTag, []string{"bad/tag"}); err == nil {
		t.Error("expected error for invalid tag")
	}

	// Store failure rollback: set ["vip"], close DB, attempt update -> error & rollback to ["vip"].
	if err := hc.SetInboundAccessTags(defaultInboundTag, []string{"vip"}); err != nil {
		t.Fatalf("SetInboundAccessTags [vip]: %v", err)
	}
	_ = sm.Close()
	if err := hc.SetInboundAccessTags(defaultInboundTag, []string{"other"}); err == nil {
		t.Fatal("expected error when store Save fails")
	}
	meta, ok := hc.GetInboundMeta(defaultInboundTag)
	if !ok || !reflect.DeepEqual(meta.AccessTags, []string{"vip"}) {
		t.Fatalf("AccessTags did not roll back on store error: %+v (ok=%v)", meta, ok)
	}
}

func TestHysteria_GetUserSubscriptions_GatingAndRemark(t *testing.T) {
	// nil userMgr must return nil, nil cleanly.
	noMgr, err := NewHysteriaContainer(HysteriaConfig{Listen: "127.0.0.1", Port: 9443})
	if err != nil {
		t.Fatalf("NewHysteriaContainer: %v", err)
	}
	if subs, err := noMgr.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "u1"},
		Host: "1.2.3.4",
	}); err != nil || subs != nil {
		t.Fatalf("nil userMgr GetUserSubscriptions = (%v, %v), want (nil, nil)", subs, err)
	}

	sm, _, um := newIsolationTestDeps(t, []string{"vip"})
	hc, err := NewHysteriaContainer(
		HysteriaConfig{Listen: "127.0.0.1", Port: 9443},
		WithStoreMgr(sm),
		WithUserManager(um),
	)
	if err != nil {
		t.Fatalf("NewHysteriaContainer: %v", err)
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

	if err := hc.FastAddInbound(defaultInboundTag, map[string]any{
		"access_tags": []string{"vip"},
		"remark":      "SH-Home",
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	// Even if a port mapping existed and caller forged req.User tags,
	// CanUserAccessInboundByUsername must reject alice and bob.
	um.SetPortMappingForTest("alice", 9443, 24111)
	um.SetPortMappingForTest("bob", 9443, 24112)

	for _, uname := range []string{"alice", "bob", "ghost"} {
		subs, err := hc.GetUserSubscriptions(contracts.SubscriptionRequest{
			User: contracts.UserSpec{
				Username:    uname,
				AuthToken:   "tok",
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

	carolUser, _ := um.GetUser("carol")
	subs, err := hc.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     contracts.UserSpec{Username: "carol", AuthToken: carolUser.AuthToken},
		Host:     "example.com",
		NodeName: "Tokyo",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions(carol): %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription for carol, got %d", len(subs))
	}
	if subs[0].NodeName != "Tokyo-SH-Home" {
		t.Errorf("NodeName = %q, want Tokyo-SH-Home", subs[0].NodeName)
	}
	if !strings.HasSuffix(subs[0].URI, "#Tokyo-SH-Home") {
		t.Errorf("URI = %q, expected #Tokyo-SH-Home suffix", subs[0].URI)
	}

	// Empty remark keeps req.NodeName unchanged.
	if err := hc.FastAddInbound(defaultInboundTag, map[string]any{
		"access_tags": []string{"vip"},
	}); err != nil {
		t.Fatalf("FastAddInbound empty remark: %v", err)
	}
	subs, err = hc.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     contracts.UserSpec{Username: "carol", AuthToken: carolUser.AuthToken},
		Host:     "example.com",
		NodeName: "Tokyo",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions(carol, no remark): %v", err)
	}
	if len(subs) != 1 || subs[0].NodeName != "Tokyo" {
		t.Fatalf("unexpected subs without remark: %+v", subs)
	}
}

func TestHysteria_StorePersistenceAndRestore(t *testing.T) {
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
	hc1, err := NewHysteriaContainer(
		HysteriaConfig{Listen: "127.0.0.1", Port: 9443},
		WithStoreMgr(sm),
	)
	if err != nil {
		t.Fatalf("NewHysteriaContainer 1: %v", err)
	}
	if err := hc1.FastAddInbound(defaultInboundTag, map[string]any{
		"access_tags": []string{"alpha", "vip"},
		"remark":      "Edge-HY",
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	if err := hc1.SetInboundAccessTags(defaultInboundTag, []string{"vip"}); err != nil {
		t.Fatalf("SetInboundAccessTags: %v", err)
	}

	// Container 2 simulates restart reading from same StoreManager.
	hc2, err := NewHysteriaContainer(
		HysteriaConfig{Listen: "127.0.0.1", Port: 9443},
		WithStoreMgr(sm),
		WithUserManager(um),
	)
	if err != nil {
		t.Fatalf("NewHysteriaContainer 2: %v", err)
	}
	if err := hc2.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	meta, ok := hc2.GetInboundMeta(defaultInboundTag)
	if !ok {
		t.Fatal("GetInboundMeta on restored container returned false")
	}
	if meta.InboundType != contracts.InboundTypeDirect || !reflect.DeepEqual(meta.AccessTags, []string{"vip"}) || meta.Remark != "Edge-HY" {
		t.Fatalf("restored meta mismatch: %+v", meta)
	}

	aliceKey := string(contracts.ContainerHysteria) + ":" + defaultInboundTag + ":alice"
	carolKey := string(contracts.ContainerHysteria) + ":" + defaultInboundTag + ":carol"
	if fm.GetRule(aliceKey) != nil {
		t.Fatal("restored container must not allocate rule for alice")
	}
	if fm.GetRule(carolKey) == nil {
		t.Fatal("restored container must allocate rule for carol")
	}

	// Persist disabled state across restart.
	if err := hc2.RemoveInboundConfig(defaultInboundTag); err != nil {
		t.Fatalf("RemoveInboundConfig: %v", err)
	}
	hc3, err := NewHysteriaContainer(
		HysteriaConfig{Listen: "127.0.0.1", Port: 9443},
		WithStoreMgr(sm),
		WithUserManager(um),
	)
	if err != nil {
		t.Fatalf("NewHysteriaContainer 3: %v", err)
	}
	if err := hc3.Restore(context.Background()); err != nil {
		t.Fatalf("Restore 3: %v", err)
	}
	if _, ok := hc3.GetInboundMeta(defaultInboundTag); ok {
		t.Fatal("disabled inbound should remain disabled after restore")
	}
	if len(hc3.ListInboundConfigs()) != 0 {
		t.Fatal("expected empty ListInboundConfigs for disabled inbound after restore")
	}
	if fm.GetRule(carolKey) != nil {
		t.Fatal("disabled inbound must not allocate rules after restore")
	}
}
