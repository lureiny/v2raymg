package cmd

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

func newTestStore(t *testing.T) *store.StoreManager {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	sm, err := store.NewStoreManager(dsn, migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })
	return sm
}

func newTestForwardManager(t *testing.T, min, max uint32, reserved ...uint32) *forward.DefaultForwardManager {
	t.Helper()
	fm, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort:  min,
		MaxPort:  max,
		Reserved: reserved,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	t.Cleanup(func() { _ = fm.Close() })
	return fm
}

func saveInbound(t *testing.T, sm *store.StoreManager, tag, containerType, nativeJSON string) {
	t.Helper()
	err := sm.InboundStore().Save(&store.InboundRecord{
		Tag:           tag,
		ContainerType: containerType,
		CertSource:    "none",
		NativeJSON:    []byte(nativeJSON),
	})
	if err != nil {
		t.Fatalf("save inbound %q: %v", tag, err)
	}
}

// This is the regression the whole pre-claim pass exists for. Containers start
// one at a time, and each one's Start allocates its users' forward ports. A
// container that starts later restores inbound ports pinned by a previous run,
// and a pinned port cannot move out of the way — so without a pass that claims
// every persisted inbound port up front, an earlier container's user can be
// handed a port a later container is about to try to bind.
func TestPreClaim_ForwardCannotTakeAPersistedInboundPort(t *testing.T) {
	sm := newTestStore(t)
	// mihomo's persisted listener sits at the bottom of the forward pool.
	saveInbound(t, sm, "ss-1", "mihomo",
		`{"tag":"ss-1","protocol":"shadowsocks","port":21000,"listen_addr":"127.0.0.1"}`)

	// A two-port pool, so an unclaimed 21000 would be drawn almost immediately.
	fm := newTestForwardManager(t, 21000, 21001)
	cfg := &appconfig.AppConfig{}

	report := preClaimPersistedInboundPorts(sm, fm, cfg)
	if report.claimed != 1 {
		t.Fatalf("claimed = %d, want 1 (report: %+v)", report.claimed, report)
	}
	if len(report.conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", report.conflicts)
	}

	// Drain the pool the way container startup would; 21000 must never appear.
	got, err := fm.AllocatePort()
	if err != nil {
		t.Fatalf("AllocatePort: %v", err)
	}
	if got == 21000 {
		t.Fatal("forward allocation took the persisted inbound port 21000")
	}
	if _, err := fm.AllocatePort(); err == nil {
		t.Fatal("expected exhaustion: 21000 is spoken for by the inbound")
	}
}

// Every container writes its port at the top level of native_json, so one pass
// covers all of them without going through each container's decoder.
func TestPreClaim_CoversEveryContainerShape(t *testing.T) {
	sm := newTestStore(t)
	saveInbound(t, sm, "xray-1", "xray",
		`{"tag":"xray-1","protocol":"vmess","port":"21100","listen":"127.0.0.1"}`)
	saveInbound(t, sm, "mihomo-1", "mihomo",
		`{"tag":"mihomo-1","protocol":"shadowsocks","port":21101,"listen_addr":"127.0.0.1"}`)
	saveInbound(t, sm, "hysteria", "hysteria",
		`{"tag":"hysteria","protocol":"hysteria2","port":21102,"listen":"127.0.0.1","enabled":true}`)
	saveInbound(t, sm, "snell", "snell",
		`{"tag":"snell","protocol":"snell","port":21103,"listen":"127.0.0.1","psk":"x"}`)

	fm := newTestForwardManager(t, 21100, 21110)
	report := preClaimPersistedInboundPorts(sm, fm, &appconfig.AppConfig{})

	if report.claimed != 4 {
		t.Fatalf("claimed = %d, want 4 (unreadable: %v)", report.claimed, report.unreadable)
	}
	for _, p := range []uint32{21100, 21101, 21102, 21103} {
		if err := fm.AllocateSpecificPort(p); err == nil {
			t.Fatalf("port %d should already be claimed", p)
		}
	}
}

// An existing database very likely already contains this: two inbounds created
// without an explicit port, both silently rewritten to the old hardcoded 10000.
// Refusing to boot would strand the node on data it cannot repair without the
// node running, so the first record wins and the second is reported.
func TestPreClaim_DuplicatePortsReportedButBootContinues(t *testing.T) {
	sm := newTestStore(t)
	saveInbound(t, sm, "ss-a", "mihomo", `{"tag":"ss-a","protocol":"shadowsocks","port":10000}`)
	saveInbound(t, sm, "ss-b", "mihomo", `{"tag":"ss-b","protocol":"shadowsocks","port":10000}`)

	fm := newTestForwardManager(t, 21200, 21210)
	report := preClaimPersistedInboundPorts(sm, fm, &appconfig.AppConfig{})

	if report.claimed != 1 {
		t.Fatalf("claimed = %d, want 1 (first record wins)", report.claimed)
	}
	if len(report.conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want exactly 1", report.conflicts)
	}
	c := report.conflicts[0]
	if c.port != 10000 || c.holderTag == "" || c.tag == c.holderTag {
		t.Fatalf("conflict should name both tags and the port, got %+v", c)
	}
}

// A record whose port we cannot read is reported and skipped. Guessing would be
// worse than admitting the gap: a wrong guess claims a port some other inbound
// legitimately owns.
func TestPreClaim_UnreadablePortIsSkippedNotGuessed(t *testing.T) {
	sm := newTestStore(t)
	saveInbound(t, sm, "broken", "xray", `{"tag":"broken","protocol":"vmess","port":"auto"}`)
	saveInbound(t, sm, "fine", "mihomo", `{"tag":"fine","protocol":"shadowsocks","port":21300}`)

	fm := newTestForwardManager(t, 21300, 21310)
	report := preClaimPersistedInboundPorts(sm, fm, &appconfig.AppConfig{})

	if report.claimed != 1 {
		t.Fatalf("claimed = %d, want 1", report.claimed)
	}
	if len(report.unreadable) != 1 || report.unreadable[0] != "broken" {
		t.Fatalf("unreadable = %v, want [broken]", report.unreadable)
	}
	if len(report.conflicts) != 0 {
		t.Fatalf("an unreadable port is not a conflict, got %+v", report.conflicts)
	}
}

// hysteria and snell get their port from container config, not the API, so a
// fresh install has no record to claim from. The configured value must still be
// protected, and must not be double-counted when a record does exist.
func TestPreClaim_SingleInboundContainerConfigPorts(t *testing.T) {
	sm := newTestStore(t)
	// snell already has a record; hysteria does not (fresh install).
	//
	// The tag here must be the REAL one these containers use
	// ("<name>-default", see their defaultInboundTag constants), not the
	// container type. Deduping by tag would silently work with a made-up tag
	// and then log a spurious conflict on every real boot.
	saveInbound(t, sm, "snell-default", "snell",
		`{"tag":"snell-default","protocol":"snell","port":16160}`)

	fm := newTestForwardManager(t, 16000, 16200)
	cfg := &appconfig.AppConfig{}
	cfg.Containers.Containers = []container.ContainerEntry{
		{Type: contracts.ContainerHysteria, Enabled: true, Config: map[string]any{"port": 9443}},
		{Type: contracts.ContainerSnell, Enabled: true, Config: map[string]any{"port": 16160}},
	}

	report := preClaimPersistedInboundPorts(sm, fm, cfg)

	// snell's record and snell's config are the same listener: claimed once,
	// and NOT reported as a conflict with itself.
	if len(report.conflicts) != 0 {
		t.Fatalf("a container's config port matching its own record is not a conflict: %+v", report.conflicts)
	}
	if err := fm.AllocateSpecificPort(16160); err == nil {
		t.Fatal("snell's port should be claimed")
	}
	// hysteria's 9443 is outside the draw range but must still be recorded —
	// this is exactly why AllocateSpecific accepts the wider bookkeeping range.
	if err := fm.AllocateSpecificPort(9443); err == nil {
		t.Fatal("hysteria's configured port should be claimed even though it is outside the draw range")
	}
}

// The single-inbound exemption must not swallow a real cross-container clash.
func TestPreClaim_SingleInboundConfigStillConflictsWithAnotherContainer(t *testing.T) {
	sm := newTestStore(t)
	// A mihomo listener already sits on the port snell is configured for.
	saveInbound(t, sm, "ss-1", "mihomo", `{"tag":"ss-1","protocol":"shadowsocks","port":16160}`)

	fm := newTestForwardManager(t, 16000, 16200)
	cfg := &appconfig.AppConfig{}
	cfg.Containers.Containers = []container.ContainerEntry{
		{Type: contracts.ContainerSnell, Enabled: true, Config: map[string]any{"port": 16160}},
	}

	report := preClaimPersistedInboundPorts(sm, fm, cfg)
	if len(report.conflicts) != 1 {
		t.Fatalf("a snell config port clashing with a mihomo inbound is a real conflict, got %+v",
			report.conflicts)
	}
	if report.conflicts[0].holderTag != "ss-1" {
		t.Fatalf("conflict should name the mihomo holder, got %+v", report.conflicts[0])
	}
}

// xray renders its gRPC API as an inbound tagged "api" on the reserved API
// port, and that record can end up persisted. It is already protected by the
// reserved set, so it must not be reported as a conflict on every boot.
func TestPreClaim_RecordOnAReservedPortIsNotAConflict(t *testing.T) {
	sm := newTestStore(t)
	saveInbound(t, sm, "api", "xray",
		`{"tag":"api","protocol":"dokodemo-door","port":"62789","listen":"127.0.0.1"}`)

	fm := newTestForwardManager(t, 62700, 62800, 62789)
	cfg := &appconfig.AppConfig{}
	cfg.Containers.Containers = []container.ContainerEntry{
		{Type: contracts.ContainerXray, Config: map[string]any{}},
	}

	report := preClaimPersistedInboundPorts(sm, fm, cfg)
	if len(report.conflicts) != 0 {
		t.Fatalf("a record on an already-reserved port is not a conflict: %+v", report.conflicts)
	}
	// Still protected — via the reserved set rather than a claim.
	if err := fm.AllocateSpecificPort(62789); err == nil {
		t.Fatal("the reserved API port must not be allocatable")
	}
}

func TestReservedManagementPorts(t *testing.T) {
	cfg := &appconfig.AppConfig{}
	cfg.EndNode.RpcPort = 9090
	cfg.EndNode.HttpPort = 8080
	cfg.Containers.Containers = []container.ContainerEntry{
		{Type: contracts.ContainerXray, Config: map[string]any{}},
		{Type: contracts.ContainerHysteria, Config: map[string]any{"traffic_stats_port": 9998}},
		{Type: contracts.ContainerMihomo, Config: map[string]any{"external_controller": "127.0.0.1:9099"}},
	}

	got := reservedManagementPorts(cfg)
	want := map[uint32]string{
		9090: "rpc",
		8080: "http",
		// xray's grpc_port is unset, so the package default applies. Missing it
		// is the dangerous case: killProcessOnPort SIGKILLs whatever holds it.
		62789: "xray grpc default",
		9998:  "hysteria stats override",
		9099:  "mihomo external controller",
	}
	seen := map[uint32]bool{}
	for _, p := range got {
		seen[p] = true
	}
	for p, why := range want {
		if !seen[p] {
			t.Errorf("port %d (%s) not reserved; got %v", p, why, got)
		}
	}
}

func TestPortFromHostPortValue(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{"127.0.0.1:9090", 9090},
		{"0.0.0.0:1234", 1234},
		{"[::1]:9091", 9091},
		{"", 7777},           // empty -> fallback
		{"garbage", 7777},    // no port -> fallback
		{"host:abc", 7777},   // non-numeric -> fallback
		{"host:99999", 7777}, // out of range -> fallback
		{42, 7777},           // wrong type -> fallback
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.in), func(t *testing.T) {
			if got := portFromHostPortValue(tc.in, 7777); got != tc.want {
				t.Fatalf("portFromHostPortValue(%v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
