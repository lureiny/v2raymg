package usermanager

import (
	"reflect"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

func TestCanUserAccessInbound_TwoLayerIsolationAndOrder(t *testing.T) {
	ngs := &memNodeGroupsStore{groups: []string{"vip"}}
	um := NewUserManager(newMockForwardManager(), "test-node")
	um.EnableClusterSync("default", ngs)

	// Nil / unknown user
	if um.CanUserAccessInbound(nil, nil) {
		t.Error("nil user must not access inbound")
	}
	if um.CanUserAccessInboundByUsername("ghost", nil) {
		t.Error("unknown user must not access inbound")
	}

	// Deleting user
	deletingUser := &contracts.User{
		Username:      "del",
		TargetGroup:   "vip",
		InboundTags:   []string{"vip"},
		DeletionState: contracts.DeletionStateDeleting,
	}
	if um.CanUserAccessInbound(deletingUser, []string{"vip"}) {
		t.Error("deleting user must not access inbound")
	}

	// Expired user
	expiredUser := &contracts.User{
		Username:    "exp",
		TargetGroup: "vip",
		InboundTags: []string{"vip"},
		ExpiryTime:  time.Now().Add(-time.Hour),
	}
	if um.CanUserAccessInbound(expiredUser, []string{"vip"}) {
		t.Error("expired user must not access inbound")
	}

	// Bob: group=basic (fails Layer 1), inbound_tags=["vip"] -> MUST BE DENIED even on inbound with access_tags=["vip"] or public []
	if err := um.AddUser(AddUserRequest{
		Username:    "bob",
		Password:    "pw",
		TargetGroup: "basic",
		InboundTags: []string{"vip"},
	}); err != nil {
		t.Fatalf("AddUser bob: %v", err)
	}
	if um.CanUserAccessInboundByUsername("bob", nil) {
		t.Error("bob failing Layer 1 must be denied even for untagged inbound")
	}
	if um.CanUserAccessInboundByUsername("bob", []string{"vip"}) {
		t.Error("bob failing Layer 1 must be denied even when Layer 2 tag matches")
	}

	// Alice: group=vip (passes Layer 1), inbound_tags=[] -> MUST BE DENIED for inbound with access_tags=["vip"]
	// (same-name "vip" must NOT cross between Node layer and Inbound layer),
	// and ALLOWED for untagged public inbound.
	if err := um.AddUser(AddUserRequest{
		Username:    "alice",
		Password:    "pw",
		TargetGroup: "vip",
	}); err != nil {
		t.Fatalf("AddUser alice: %v", err)
	}
	if !um.CanUserAccessInboundByUsername("alice", nil) {
		t.Error("alice passing Layer 1 should access untagged inbound")
	}
	if !um.CanUserAccessInboundByUsername("alice", []string{}) {
		t.Error("alice passing Layer 1 should access empty access_tags inbound")
	}
	if um.CanUserAccessInboundByUsername("alice", []string{"vip"}) {
		t.Error("alice with empty inbound_tags must NOT access access_tags=[vip] inbound (same-name namespace isolation)")
	}

	// Carol: group=vip, inbound_tags=["home-sh", "vip"] -> matches ["other", "vip"], denied on ["home-bj"]
	if err := um.AddUser(AddUserRequest{
		Username:    "carol",
		Password:    "pw",
		TargetGroup: "vip",
		InboundTags: []string{"home-sh", "vip"},
	}); err != nil {
		t.Fatalf("AddUser carol: %v", err)
	}
	if !um.CanUserAccessInboundByUsername("carol", []string{"other", "vip"}) {
		t.Error("carol should access inbound when access_tags intersects inbound_tags")
	}
	if um.CanUserAccessInboundByUsername("carol", []string{"home-bj"}) {
		t.Error("carol should be denied when access_tags does not intersect inbound_tags")
	}
}

func TestAddUserRequest_AtomicRoleGroupInboundTags(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	um, err := NewUserManagerWithStore(nil, storeMgr, "test-node")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}

	sub := um.Subscribe()
	defer um.Unsubscribe(sub)

	// Invalid inbound tag should fail cleanly without adding user
	err = um.AddUser(AddUserRequest{
		Username:    "bad",
		Password:    "pw",
		InboundTags: []string{"bad,tag"},
	})
	if err == nil {
		t.Fatal("expected error for invalid inbound_tags in AddUser")
	}
	if _, getErr := um.GetUser("bad"); getErr == nil {
		t.Fatal("user should not exist after failed AddUser")
	}

	// Valid atomic creation
	err = um.AddUser(AddUserRequest{
		Username:    "alice",
		Password:    "pw",
		Role:        "admin",
		TargetGroup: "vip",
		InboundTags: []string{" vip ", "home-sh", "vip", ""},
	})
	if err != nil {
		t.Fatalf("AddUser alice: %v", err)
	}

	// Expect exactly one UserEventAdd with normalized tags
	select {
	case ev := <-sub:
		if ev.Type != UserEventAdd {
			t.Fatalf("expected UserEventAdd, got %v", ev.Type)
		}
		if ev.User.Role != "admin" || ev.User.TargetGroup != "vip" {
			t.Errorf("unexpected event user fields: role=%q group=%q", ev.User.Role, ev.User.TargetGroup)
		}
		if !reflect.DeepEqual(ev.User.InboundTags, []string{"home-sh", "vip"}) {
			t.Errorf("event InboundTags = %v, want [home-sh vip]", ev.User.InboundTags)
		}
	default:
		t.Fatal("expected UserEventAdd")
	}
	select {
	case extra := <-sub:
		t.Fatalf("expected only 1 event, got extra %+v", extra)
	default:
	}

	// Verify persisted to SQLite
	persisted, err := storeMgr.UserStore().Get("alice")
	if err != nil {
		t.Fatalf("UserStore.Get: %v", err)
	}
	if persisted.Role != "admin" || persisted.TargetGroup != "vip" || !reflect.DeepEqual(persisted.InboundTags, []string{"home-sh", "vip"}) {
		t.Errorf("persisted user mismatch: %+v", persisted)
	}
}

func TestSetUserInboundTags(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	um, err := NewUserManagerWithStore(nil, storeMgr, "test-node")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}
	if err := um.AddUser(AddUserRequest{Username: "alice", Password: "pw"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	sub := um.Subscribe()
	defer um.Unsubscribe(sub)

	if err := um.SetUserInboundTags("alice", []string{" b ", "a", "b"}); err != nil {
		t.Fatalf("SetUserInboundTags: %v", err)
	}
	u, err := um.GetUser("alice")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !reflect.DeepEqual(u.InboundTags, []string{"a", "b"}) {
		t.Errorf("InboundTags = %v, want [a b]", u.InboundTags)
	}

	select {
	case ev := <-sub:
		if ev.Type != UserEventUpdate || !reflect.DeepEqual(ev.User.InboundTags, []string{"a", "b"}) {
			t.Errorf("unexpected event: %+v", ev)
		}
	default:
		t.Fatal("expected UserEventUpdate")
	}

	// Invalid tag does not overwrite
	if err := um.SetUserInboundTags("alice", []string{"bad:tag"}); err == nil {
		t.Fatal("expected error for invalid tag")
	}
	u, _ = um.GetUser("alice")
	if !reflect.DeepEqual(u.InboundTags, []string{"a", "b"}) {
		t.Errorf("InboundTags mutated on failed update: %v", u.InboundTags)
	}

	// Clear tags
	if err := um.SetUserInboundTags("alice", []string{}); err != nil {
		t.Fatalf("SetUserInboundTags clear: %v", err)
	}
	persisted, _ := storeMgr.UserStore().Get("alice")
	if len(persisted.InboundTags) != 0 {
		t.Errorf("expected empty persisted InboundTags, got %v", persisted.InboundTags)
	}
}

func TestSyncUpsertUser_SyncsAndDeepCopiesInboundTags(t *testing.T) {
	um := NewUserManager(newMockForwardManager(), "test-node")

	inTags := []string{"home-sh", "vip"}
	incoming := &contracts.User{
		Username:    "alice",
		AuthToken:   "11111111-1111-4111-8111-111111111111",
		InboundTags: inTags,
		UpdatedAtUs: 100,
		OriginNode:  "node-b",
	}
	applied, err := um.SyncUpsertUser(incoming)
	if err != nil || !applied {
		t.Fatalf("SyncUpsertUser new: applied=%v err=%v", applied, err)
	}
	// Mutate caller slice after sync; stored user must remain isolated.
	inTags[0] = "tampered"
	got, _ := um.GetUser("alice")
	if !reflect.DeepEqual(got.InboundTags, []string{"home-sh", "vip"}) {
		t.Fatalf("new user InboundTags aliased caller slice: got %v", got.InboundTags)
	}

	// Update existing user with new InboundTags
	updTags := []string{"home-bj"}
	updated := &contracts.User{
		Username:    "alice",
		AuthToken:   "11111111-1111-4111-8111-111111111111",
		InboundTags: updTags,
		UpdatedAtUs: 200,
		OriginNode:  "node-b",
	}
	applied, err = um.SyncUpsertUser(updated)
	if err != nil || !applied {
		t.Fatalf("SyncUpsertUser update: applied=%v err=%v", applied, err)
	}
	updTags[0] = "tampered-update"
	got, _ = um.GetUser("alice")
	if !reflect.DeepEqual(got.InboundTags, []string{"home-bj"}) {
		t.Fatalf("updated user InboundTags aliased caller slice: got %v", got.InboundTags)
	}

	// Clear InboundTags via sync
	cleared := &contracts.User{
		Username:    "alice",
		AuthToken:   "11111111-1111-4111-8111-111111111111",
		InboundTags: nil,
		UpdatedAtUs: 300,
		OriginNode:  "node-b",
	}
	applied, err = um.SyncUpsertUser(cleared)
	if err != nil || !applied {
		t.Fatalf("SyncUpsertUser clear: applied=%v err=%v", applied, err)
	}
	got, _ = um.GetUser("alice")
	if len(got.InboundTags) != 0 {
		t.Fatalf("expected empty InboundTags after sync clear, got %v", got.InboundTags)
	}
}

func TestSetNodeGroups_EmitsUpdateForActiveUsers(t *testing.T) {
	ngs := &memNodeGroupsStore{groups: []string{"asia"}}
	um := NewUserManager(newMockForwardManager(), "test-node")
	um.EnableClusterSync("default", ngs)

	if err := um.AddUser(AddUserRequest{Username: "active-u", Password: "pw", TargetGroup: "us"}); err != nil {
		t.Fatalf("AddUser active: %v", err)
	}
	if err := um.AddUser(AddUserRequest{Username: "del-u", Password: "pw", TargetGroup: "us"}); err != nil {
		t.Fatalf("AddUser del: %v", err)
	}
	if err := um.RemoveUser(RemoveUserRequest{Username: "del-u"}); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}

	sub := um.Subscribe()
	defer um.Unsubscribe(sub)

	if err := um.SetNodeGroups([]string{"asia", "us"}); err != nil {
		t.Fatalf("SetNodeGroups: %v", err)
	}

	var events []UserEvent
	for {
		select {
		case ev := <-sub:
			events = append(events, ev)
		default:
			goto done
		}
	}
done:
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 UserEventUpdate for active user, got %d: %+v", len(events), events)
	}
	if events[0].Type != UserEventUpdate || events[0].Username != "active-u" || events[0].User == nil {
		t.Errorf("unexpected event: %+v", events[0])
	}
}

func TestReleaseInboundPorts_CleansBindPortsAndPortMappingsAndPersists(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	fwdMgr, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort: 22000,
		MaxPort: 22999,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	t.Cleanup(func() { _ = fwdMgr.Close() })

	um, err := NewUserManagerWithStore(fwdMgr, storeMgr, "test-node")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}

	if err := um.AddUser(AddUserRequest{Username: "alice", Password: "pw"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	portA, err := um.GetBindPort(GetBindPortRequest{
		Username:      "alice",
		TargetPort:    10001,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "in-a",
	})
	if err != nil {
		t.Fatalf("GetBindPort in-a: %v", err)
	}
	portB, err := um.GetBindPort(GetBindPortRequest{
		Username:      "alice",
		TargetPort:    10002,
		ContainerType: contracts.ContainerMihomo,
		InboundTag:    "in-b",
	})
	if err != nil {
		t.Fatalf("GetBindPort in-b: %v", err)
	}

	if err := um.ReleaseInboundPorts("in-a"); err != nil {
		t.Fatalf("ReleaseInboundPorts: %v", err)
	}

	// Forward rule for in-a gone, in-b preserved
	if fwdMgr.GetRule("xray:in-a:alice") != nil {
		t.Error("expected in-a forward rule to be removed")
	}
	if fwdMgr.GetRule("mihomo:in-b:alice") == nil {
		t.Error("expected in-b forward rule to remain")
	}

	// PortMappings and BindPorts cleaned for in-a only
	if _, ok := um.GetUserPortByDst("alice", 10001); ok {
		t.Error("expected PortMappings[10001] to be removed after ReleaseInboundPorts")
	}
	if gotB, ok := um.GetUserPortByDst("alice", 10002); !ok || gotB != portB {
		t.Errorf("expected PortMappings[10002]=%d to remain, got (%d, %v)", portB, gotB, ok)
	}

	alice, _ := um.GetUser("alice")
	for _, p := range alice.BindPorts {
		if p == portA {
			t.Errorf("BindPorts still contains released port %d: %v", portA, alice.BindPorts)
		}
	}
	if !reflect.DeepEqual(alice.BindPorts, []uint32{portB}) {
		t.Errorf("BindPorts = %v, want [%d]", alice.BindPorts, portB)
	}

	// Persisted to store
	persisted, err := storeMgr.UserStore().Get("alice")
	if err != nil {
		t.Fatalf("UserStore.Get: %v", err)
	}
	if _, ok := persisted.PortMappings[10001]; ok {
		t.Errorf("persisted PortMappings still has 10001: %+v", persisted.PortMappings)
	}
	if persisted.PortMappings[10002] != portB {
		t.Errorf("persisted PortMappings[10002] = %d, want %d", persisted.PortMappings[10002], portB)
	}
}

func TestRotateUserPortForInbound_PreservesNetworkListenStackUDPIdle(t *testing.T) {
	um, fwdMgr := newTestManagerWithForward(t)
	if err := um.AddUser(AddUserRequest{Username: "udpuser", Password: "pw"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	created, err := fwdMgr.AddRule(forward.ForwardRule{
		Username:          "udpuser",
		ContainerType:     contracts.ContainerMihomo,
		InboundTag:        "hy2-chain",
		Protocol:          contracts.ProtocolHysteria2,
		ListenStack:       forward.ListenStackIPv4,
		TargetAddr:        "127.0.0.1:10005",
		Network:           string(forward.NetworkUDP),
		UDPSessionIdleSec: 120,
	})
	if err != nil {
		t.Fatalf("AddRule udp: %v", err)
	}
	um.SetPortMappingForTest("udpuser", 10005, created.ListenPort)

	newPort, err := um.RotateUserPortForInbound("udpuser", contracts.ContainerMihomo, "hy2-chain", 0)
	if err != nil {
		t.Fatalf("RotateUserPortForInbound: %v", err)
	}
	if newPort == created.ListenPort {
		t.Errorf("expected port change from %d", created.ListenPort)
	}

	rotated := fwdMgr.GetRule("mihomo:hy2-chain:udpuser")
	if rotated == nil {
		t.Fatal("expected rotated rule to exist")
	}
	if rotated.Network != string(forward.NetworkUDP) {
		t.Errorf("Network = %q, want %q", rotated.Network, forward.NetworkUDP)
	}
	if rotated.ListenStack != forward.ListenStackIPv4 {
		t.Errorf("ListenStack = %q, want %q", rotated.ListenStack, forward.ListenStackIPv4)
	}
	if rotated.UDPSessionIdleSec != 120 {
		t.Errorf("UDPSessionIdleSec = %d, want 120", rotated.UDPSessionIdleSec)
	}
}

func TestReleaseInboundPorts_ClearsPersistedPortMappingsWithoutActiveRule(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	fwdMgr, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort: 23000,
		MaxPort: 23999,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	t.Cleanup(func() { _ = fwdMgr.Close() })

	um, err := NewUserManagerWithStore(fwdMgr, storeMgr, "test-node")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}

	if err := um.AddUser(AddUserRequest{Username: "alice", Password: "pw"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	portA, err := um.GetBindPort(GetBindPortRequest{
		Username:      "alice",
		TargetPort:    11001,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "in-stale",
	})
	if err != nil {
		t.Fatalf("GetBindPort in-stale: %v", err)
	}
	portB, err := um.GetBindPort(GetBindPortRequest{
		Username:      "alice",
		TargetPort:    11002,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "in-keep",
	})
	if err != nil {
		t.Fatalf("GetBindPort in-keep: %v", err)
	}

	// Simulate active forward rule gone (e.g. after process restart before reconcile),
	// while persisted PortMappings[11001] and BindPorts still hold portA.
	if err := fwdMgr.RemoveRulesByInbound("in-stale"); err != nil {
		t.Fatalf("RemoveRulesByInbound: %v", err)
	}
	if fwdMgr.GetRule("xray:in-stale:alice") != nil {
		t.Fatal("expected no active forward rule for in-stale")
	}

	// Passing explicit dstPort cleans persisted PortMappings[11001] & BindPorts[portA]
	// even with zero active forward rules.
	if err := um.ReleaseInboundPorts("in-stale", 11001); err != nil {
		t.Fatalf("ReleaseInboundPorts: %v", err)
	}

	if _, ok := um.GetUserPortByDst("alice", 11001); ok {
		t.Error("expected PortMappings[11001] cleared despite absent active forward rule")
	}
	if gotB, ok := um.GetUserPortByDst("alice", 11002); !ok || gotB != portB {
		t.Errorf("expected PortMappings[11002]=%d preserved, got (%d, %v)", portB, gotB, ok)
	}
	alice, _ := um.GetUser("alice")
	for _, p := range alice.BindPorts {
		if p == portA {
			t.Errorf("BindPorts still contains stale port %d: %v", portA, alice.BindPorts)
		}
	}

	persisted, err := storeMgr.UserStore().Get("alice")
	if err != nil {
		t.Fatalf("UserStore.Get: %v", err)
	}
	if _, ok := persisted.PortMappings[11001]; ok {
		t.Errorf("persisted PortMappings still contains 11001: %+v", persisted.PortMappings)
	}
	if persisted.PortMappings[11002] != portB {
		t.Errorf("persisted PortMappings[11002] = %d, want %d", persisted.PortMappings[11002], portB)
	}
}

func TestSyncUpsertUser_TombstoneSyncsInboundTags(t *testing.T) {
	um := NewUserManager(nil, "node-b")
	if err := um.AddUser(AddUserRequest{
		Username:    "alice",
		Password:    "pw",
		TargetGroup: "vip",
		InboundTags: []string{"old-tag"},
	}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	u, _ := um.GetUser("alice")

	tombstone := u.Clone()
	tombstone.MarkDeleting()
	tombstone.InboundTags = []string{"tombstone-tag"}
	tombstone.UpdatedAtUs = u.UpdatedAtUs + 1000
	tombstone.OriginNode = "node-a"
	tombstone.Hash = "remote-hash"

	applied, err := um.SyncUpsertUser(tombstone)
	if err != nil || !applied {
		t.Fatalf("SyncUpsertUser tombstone: applied=%v err=%v", applied, err)
	}
	// Mutating incoming slice afterwards must not alias stored tombstone
	tombstone.InboundTags[0] = "mutated"

	got := um.GetUserIncludingDeleting("alice")
	if got == nil {
		t.Fatal("GetUserIncludingDeleting(alice) returned nil")
	}
	if !got.IsDeleting() {
		t.Fatal("expected user marked deleting")
	}
	if !reflect.DeepEqual(got.InboundTags, []string{"tombstone-tag"}) {
		t.Errorf("tombstone InboundTags = %v, want [tombstone-tag]", got.InboundTags)
	}

	// Also verify inserting a brand-new tombstone preserves deep-copied InboundTags.
	newTombstone := u.Clone()
	newTombstone.Username = "bob"
	newTombstone.MarkDeleting()
	newTombstone.InboundTags = []string{"new-tomb-tag"}
	newTombstone.UpdatedAtUs = u.UpdatedAtUs + 2000
	newTombstone.OriginNode = "node-a"
	newTombstone.Hash = "remote-hash-bob"

	applied, err = um.SyncUpsertUser(newTombstone)
	if err != nil || !applied {
		t.Fatalf("SyncUpsertUser new tombstone: applied=%v err=%v", applied, err)
	}
	newTombstone.InboundTags[0] = "mutated"
	gotBob := um.GetUserIncludingDeleting("bob")
	if gotBob == nil || !gotBob.IsDeleting() {
		t.Fatalf("expected bob tombstone stored, got %+v", gotBob)
	}
	if !reflect.DeepEqual(gotBob.InboundTags, []string{"new-tomb-tag"}) {
		t.Errorf("bob tombstone InboundTags = %v, want [new-tomb-tag]", gotBob.InboundTags)
	}
}
