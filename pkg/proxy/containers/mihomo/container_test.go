package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
	"gopkg.in/yaml.v3"
)

// ----- test helpers ---------------------------------------------------------

// newTestContainer builds a MihomoContainer with a fresh SQLite store in
// a temp dir. Process isn't started; REST client is unset by default.
func newTestContainer(t *testing.T) *MihomoContainer {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	sm, err := store.NewStoreManager(dsn, migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })

	cfg := MihomoConfig{
		BinaryPath:         "/unused",
		ConfigFilePath:     "/unused",
		DataDir:            "/unused",
		ExternalController: "127.0.0.1:9090",
		Secret:             "",
		ReleaseTag:         "Prerelease-Alpha",
	}
	c, err := NewMihomoContainer(cfg, WithStoreMgr(sm))
	if err != nil {
		t.Fatalf("NewMihomoContainer: %v", err)
	}
	return c
}

// capturingRESTServer starts an httptest server that records every
// `PUT /configs` body for later assertions. All 2xx responses; callers
// override via the handler param when they need to test failure paths.
type capturingRESTServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	bodies [][]byte
}

func newCapturingRESTServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *capturingRESTServer {
	t.Helper()
	c := &capturingRESTServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			// Lock the wire format: rest_client serialises the yaml
			// payload as a JSON body {"payload": "..."}. A future switch
			// to raw application/yaml would require intentional test
			// updates — this assertion makes that visible.
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("PUT /configs Content-Type = %q, want application/json", ct)
			}
			var body struct {
				Payload string `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				c.mu.Lock()
				c.bodies = append(c.bodies, []byte(body.Payload))
				c.mu.Unlock()
			}
		}
		if handler != nil {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	c.srv = httptest.NewServer(mux)
	t.Cleanup(c.srv.Close)
	return c
}

func (c *capturingRESTServer) URL() string { return c.srv.URL }

func (c *capturingRESTServer) bodyCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

func (c *capturingRESTServer) latestListeners(t *testing.T) []map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) == 0 {
		t.Fatal("no PUT /configs body captured")
	}
	last := c.bodies[len(c.bodies)-1]
	var cfg map[string]any
	if err := yaml.Unmarshal(last, &cfg); err != nil {
		t.Fatalf("unmarshal yaml: %v", err)
	}
	raw, _ := cfg["listeners"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		// yaml.v3 always decodes nested maps as map[string]any. If a
		// future yaml.v4 changes this, we want to know loudly rather
		// than silently lose listener entries.
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("listener entry is not map[string]any: %T %#v", e, e)
		}
		out = append(out, m)
	}
	return out
}

// attachRunningREST simulates a running container by wiring the REST client
// against the captured server. Bypasses the process lifecycle since tests
// don't need a real mihomo binary.
func attachRunningREST(c *MihomoContainer, baseURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.restClient = NewRESTClient(baseURL, "")
}

// newTestContainerWithUserMgr is a variant of newTestContainer that wires a
// UserManager (backed by a real in-process ForwardManager) through
// WithUserManager. Use for tests that exercise user events, forward rule
// CRUD, or reconcile. The ForwardManager is returned so tests can assert
// against its rule state directly (via GetRule); both it and the store
// manager are closed automatically via t.Cleanup.
func newTestContainerWithUserMgr(t *testing.T) (*MihomoContainer, *usermanager.UserManager, forward.ForwardManager) {
	t.Helper()
	um, fwdMgr := newTestUserManager(t) // declared in inbound_test.go
	dsn := filepath.Join(t.TempDir(), "test.db")
	sm, err := store.NewStoreManager(dsn, migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })

	cfg := MihomoConfig{
		BinaryPath:         "/unused",
		ConfigFilePath:     "/unused",
		DataDir:            "/unused",
		ExternalController: "127.0.0.1:9090",
		Secret:             "",
		ReleaseTag:         "Prerelease-Alpha",
	}
	c, err := NewMihomoContainer(cfg, WithStoreMgr(sm), WithUserManager(um))
	if err != nil {
		t.Fatalf("NewMihomoContainer: %v", err)
	}
	// Critical: WithUserManager spawns a goroutine + Subscribe at
	// construction. Without Close() the goroutine + subscriber leak for
	// the rest of the test process — under -count=N or -race the
	// accumulation is observable.
	t.Cleanup(c.Close)
	return c, um, fwdMgr
}

// ----- factory (stage 1) ----------------------------------------------------

// TestMihomoFactoryLoadable verifies the stage-1 acceptance criterion:
// ContainerMgr loads an enabled mihomo entry without error and the returned
// Container reports the expected ContainerType. Start() is intentionally not
// invoked — the skeleton runner returns "not implemented" by design.
func TestMihomoFactoryLoadable(t *testing.T) {
	cfg := container.ContainerMgrConfig{
		Containers: []container.ContainerEntry{
			{
				Type:    contracts.ContainerMihomo,
				Enabled: true,
				Config:  map[string]any{},
			},
		},
	}

	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	if err := mgr.LoadFromConfig(cfg); err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}

	c, ok := mgr.Get(contracts.ContainerMihomo)
	if !ok {
		t.Fatal("mihomo container not loaded")
	}
	if c.Type() != contracts.ContainerMihomo {
		t.Errorf("Type() = %q, want %q", c.Type(), contracts.ContainerMihomo)
	}
}

// ----- FastAddInbound / Get / List / Remove (stage 4) -----------------------

func TestFastAddInbound_PreStart_PersistsMapAndStore(t *testing.T) {
	// With no REST client attached, the push is a no-op. Map + store must
	// still reflect the new inbound so a later Start replays it.
	c := newTestContainer(t)

	err := c.FastAddInbound("vmess-a", map[string]any{
		"protocol": "vmess",
		"port":     10001,
		"uuid":     "uuid-a",
	})
	if err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	// Map
	inb, err := c.GetInboundConfig("vmess-a")
	if err != nil {
		t.Fatalf("GetInboundConfig: %v", err)
	}
	if inb.Tag() != "vmess-a" || inb.Port() != 10001 {
		t.Errorf("got inbound %+v", inb)
	}

	// Store
	recs, err := c.storeMgr.InboundStore().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(recs) != 1 || recs[0].Tag != "vmess-a" || recs[0].ContainerType != "mihomo" {
		t.Errorf("unexpected records: %+v", recs)
	}
}

func TestFastAddInbound_DuplicateTag(t *testing.T) {
	c := newTestContainer(t)
	params := map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"}

	if err := c.FastAddInbound("dup", params); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := c.FastAddInbound("dup", params)
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	// Ensure the first inbound is intact.
	if _, err := c.GetInboundConfig("dup"); err != nil {
		t.Errorf("first inbound lost after duplicate rejection: %v", err)
	}
	recs, _ := c.storeMgr.InboundStore().Load()
	if len(recs) != 1 {
		t.Errorf("store records = %d, want 1", len(recs))
	}
}

func TestFastAddInbound_UnknownProtocolRejected(t *testing.T) {
	c := newTestContainer(t)
	err := c.FastAddInbound("x", map[string]any{
		"protocol": "tuic",
		"port":     10001,
		"password": "p",
	})
	if !errors.Is(err, ErrProtocolNotSupported) {
		t.Fatalf("expected ErrProtocolNotSupported, got %v", err)
	}
	// Nothing persisted.
	recs, _ := c.storeMgr.InboundStore().Load()
	if len(recs) != 0 {
		t.Errorf("store should be empty, got: %+v", recs)
	}
}

func TestFastAddInbound_Running_PushesListener(t *testing.T) {
	srv := newCapturingRESTServer(t, nil)
	c := newTestContainer(t)
	attachRunningREST(c, srv.URL())

	err := c.FastAddInbound("vmess-a", map[string]any{
		"protocol": "vmess",
		"port":     10001,
		"uuid":     "uuid-a",
	})
	if err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	if srv.bodyCount() != 1 {
		t.Fatalf("expected 1 PUT /configs, got %d", srv.bodyCount())
	}

	listeners := srv.latestListeners(t)
	if len(listeners) != 1 {
		t.Fatalf("expected 1 listener, got %d", len(listeners))
	}
	if listeners[0]["name"] != "vmess-a" || listeners[0]["type"] != "vmess" {
		t.Errorf("listener fields wrong: %+v", listeners[0])
	}
}

func TestFastAddInbound_RESTFailure_RollsBack(t *testing.T) {
	// REST server returns 400: the map and store should both remain empty
	// after FastAddInbound returns the error. A subsequent Start replay
	// must NOT resurrect the rejected listener.
	srv := newCapturingRESTServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bogus"}`))
	})
	c := newTestContainer(t)
	attachRunningREST(c, srv.URL())

	err := c.FastAddInbound("bad", map[string]any{
		"protocol": "vmess",
		"port":     10001,
		"uuid":     "u",
	})
	if err == nil {
		t.Fatal("expected error from REST")
	}

	if _, err := c.GetInboundConfig("bad"); err == nil {
		t.Error("inbound should have been rolled back from map")
	}
	recs, _ := c.storeMgr.InboundStore().Load()
	if len(recs) != 0 {
		t.Errorf("store should be empty after rollback, got: %+v", recs)
	}
}

func TestRemoveInboundConfig_Success(t *testing.T) {
	srv := newCapturingRESTServer(t, nil)
	c := newTestContainer(t)
	attachRunningREST(c, srv.URL())

	_ = c.FastAddInbound("vmess-a", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"})
	_ = c.FastAddInbound("trojan-b", map[string]any{"protocol": "trojan", "port": 10002, "password": "p"})

	if err := c.RemoveInboundConfig("vmess-a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := c.GetInboundConfig("vmess-a"); err == nil {
		t.Error("removed inbound still in map")
	}
	if _, err := c.GetInboundConfig("trojan-b"); err != nil {
		t.Errorf("sibling inbound lost: %v", err)
	}
	recs, _ := c.storeMgr.InboundStore().Load()
	if len(recs) != 1 || recs[0].Tag != "trojan-b" {
		t.Errorf("store should have only trojan-b: %+v", recs)
	}

	// Latest PUT should contain only trojan-b.
	listeners := srv.latestListeners(t)
	if len(listeners) != 1 || listeners[0]["name"] != "trojan-b" {
		t.Errorf("remaining listener wrong: %+v", listeners)
	}
}

func TestRemoveInboundConfig_MissingTag(t *testing.T) {
	c := newTestContainer(t)
	err := c.RemoveInboundConfig("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing tag")
	}
}

func TestRemoveInboundConfig_RESTFailure_LeavesStateIntact(t *testing.T) {
	// When REST rejects the new (post-delete) config, the local map/store
	// stay intact — user can retry. Contrast with FastAddInbound where REST
	// failure rolls back both (because the inbound was brand new).
	failAfterN := 1 // succeed for the Add, fail for the Remove
	count := 0
	srv := newCapturingRESTServer(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		if count > failAfterN {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	c := newTestContainer(t)
	attachRunningREST(c, srv.URL())

	_ = c.FastAddInbound("t", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u-original"})

	err := c.RemoveInboundConfig("t")
	if err == nil {
		t.Fatal("expected error from REST")
	}
	// Inbound should still exist in the map, and the store record must be
	// the exact original — not a partially-mutated version. Decode the
	// NativeJSON back so we detect a hypothetical future regression where
	// Remove starts wiping fields before the REST push.
	if _, err := c.GetInboundConfig("t"); err != nil {
		t.Errorf("inbound gone after push failure: %v", err)
	}
	recs, _ := c.storeMgr.InboundStore().Load()
	if len(recs) != 1 {
		t.Fatalf("store should still have record after failed Remove: %+v", recs)
	}
	restored, err := FromNative(recs[0].NativeJSON)
	if err != nil {
		t.Fatalf("stored record no longer round-trips: %v", err)
	}
	if restored.Tag() != "t" ||
		restored.Protocol() != contracts.ProtocolVMess ||
		restored.Port() != 10001 ||
		restored.SharedCred.UUID != "u-original" {
		t.Errorf("stored record mutated during failed Remove: %+v", restored)
	}
}

func TestListInboundConfigs(t *testing.T) {
	c := newTestContainer(t)
	_ = c.FastAddInbound("a", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"})
	_ = c.FastAddInbound("b", map[string]any{"protocol": "trojan", "port": 10002, "password": "p"})

	got := c.ListInboundConfigs()
	if len(got) != 2 {
		t.Fatalf("List returned %d, want 2", len(got))
	}
	seen := map[string]bool{}
	for _, inb := range got {
		seen[inb.Tag()] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Errorf("missing tags in list: %+v", seen)
	}
}

// ----- restoreAndPushInbounds (Start hook) ----------------------------------

func TestRestoreAndPushInbounds_ReplaysStoredRecords(t *testing.T) {
	srv := newCapturingRESTServer(t, nil)
	c := newTestContainer(t)

	// Pre-seed the store as if a previous run had persisted these inbounds.
	inbs := []*MihomoInbound{
		NewMihomoInbound("a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u-a"}),
		NewMihomoInbound("b", contracts.ProtocolTrojan, 10002, MihomoSharedCred{Password: "p-b"}),
	}
	for _, inb := range inbs {
		if err := c.persistInbound(inb); err != nil {
			t.Fatalf("persistInbound: %v", err)
		}
	}

	// Simulate "process ready".
	attachRunningREST(c, srv.URL())

	if err := c.restoreAndPushInbounds(); err != nil {
		t.Fatalf("restoreAndPushInbounds: %v", err)
	}

	// Map populated.
	if len(c.ListInboundConfigs()) != 2 {
		t.Errorf("expected 2 inbounds in map, got %d", len(c.ListInboundConfigs()))
	}
	// REST received one PUT with both listeners.
	if srv.bodyCount() != 1 {
		t.Fatalf("expected 1 PUT, got %d", srv.bodyCount())
	}
	listeners := srv.latestListeners(t)
	if len(listeners) != 2 {
		t.Errorf("expected 2 listeners in PUT body, got %d", len(listeners))
	}
}

func TestRestoreAndPushInbounds_SkipsCorruptRecords(t *testing.T) {
	// A corrupt record (bad JSON, or missing credential) shouldn't block
	// startup — log+skip so one bad row doesn't lose every healthy inbound.
	c := newTestContainer(t)

	// Good record
	good := NewMihomoInbound("good", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	if err := c.persistInbound(good); err != nil {
		t.Fatalf("persistInbound: %v", err)
	}

	// Corrupt record injected directly.
	bad := &store.InboundRecord{
		Tag:           "corrupt",
		ContainerType: "mihomo",
		NativeJSON:    []byte(`{"tag":"corrupt","protocol":"tuic","port":10002,"shared_cred":{}}`),
	}
	if err := c.storeMgr.InboundStore().Save(bad); err != nil {
		t.Fatalf("Save corrupt: %v", err)
	}

	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := c.restoreAndPushInbounds(); err != nil {
		t.Fatalf("restoreAndPushInbounds: %v", err)
	}

	// Only the good inbound made it into the map.
	inbounds := c.ListInboundConfigs()
	if len(inbounds) != 1 || inbounds[0].Tag() != "good" {
		t.Errorf("expected only 'good' inbound, got: %+v", inbounds)
	}
}

func TestRestoreAndPushInbounds_EmptyStore(t *testing.T) {
	// Fresh container, empty store, running REST. Restore should
	// succeed with an empty listeners array rather than error out —
	// this is the first-boot path.
	c := newTestContainer(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := c.restoreAndPushInbounds(); err != nil {
		t.Fatalf("restoreAndPushInbounds: %v", err)
	}
	if len(c.ListInboundConfigs()) != 0 {
		t.Errorf("map should be empty: %+v", c.ListInboundConfigs())
	}
	if srv.bodyCount() != 1 {
		t.Fatalf("expected 1 PUT with empty listeners, got %d", srv.bodyCount())
	}
	if listeners := srv.latestListeners(t); len(listeners) != 0 {
		t.Errorf("listeners should be empty, got: %+v", listeners)
	}
}

func TestRestoreAndPushInbounds_NilStoreMgr(t *testing.T) {
	// Containers constructed without WithStoreMgr must still survive
	// restore (acceptable in tests per the persistInbound doc). The
	// "nil-store" guard in restoreAndPushInbounds should skip load and
	// still push an empty listeners array via REST.
	cfg := MihomoConfig{
		BinaryPath:         "/unused",
		ConfigFilePath:     "/unused",
		DataDir:            "/unused",
		ExternalController: "127.0.0.1:9090",
		Secret:             "",
		ReleaseTag:         "Prerelease-Alpha",
	}
	c, err := NewMihomoContainer(cfg) // no WithStoreMgr
	if err != nil {
		t.Fatalf("NewMihomoContainer: %v", err)
	}

	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := c.restoreAndPushInbounds(); err != nil {
		t.Fatalf("restoreAndPushInbounds: %v", err)
	}
	if srv.bodyCount() != 1 {
		t.Errorf("expected 1 PUT, got %d", srv.bodyCount())
	}
}

func TestRestoreAndPushInbounds_ClearsStaleMapOnRetry(t *testing.T) {
	// Simulate the "Start attempted, hook failed, retried" scenario:
	// c.inbounds has a stale entry from the previous attempt that was
	// since removed from the store. Restore must drop the stale entry
	// rather than let it resurrect the listener.
	c := newTestContainer(t)

	// Seed the map with an entry that has NO corresponding store record.
	stale := NewMihomoInbound("stale", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u-stale"})
	c.inboundsMu.Lock()
	c.inbounds["stale"] = stale
	c.inboundsMu.Unlock()

	// Seed a legit record in the store.
	good := NewMihomoInbound("good", contracts.ProtocolTrojan, 10002, MihomoSharedCred{Password: "p"})
	if err := c.persistInbound(good); err != nil {
		t.Fatalf("persistInbound: %v", err)
	}

	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := c.restoreAndPushInbounds(); err != nil {
		t.Fatalf("restoreAndPushInbounds: %v", err)
	}

	tags := map[string]bool{}
	for _, inb := range c.ListInboundConfigs() {
		tags[inb.Tag()] = true
	}
	if tags["stale"] {
		t.Errorf("stale inbound leaked through restore: %+v", c.ListInboundConfigs())
	}
	if !tags["good"] {
		t.Errorf("good inbound missing after restore: %+v", c.ListInboundConfigs())
	}
	listeners := srv.latestListeners(t)
	if len(listeners) != 1 || listeners[0]["name"] != "good" {
		t.Errorf("push should carry only the 'good' listener: %+v", listeners)
	}
}

func TestRestoreAndPushInbounds_IgnoresOtherContainerRecords(t *testing.T) {
	// InboundStore is shared across xray/hysteria/snell/mihomo. The restore
	// path must filter on ContainerType so it doesn't try to decode
	// non-mihomo records as MihomoInbound.
	c := newTestContainer(t)

	xrayRec := &store.InboundRecord{
		Tag:           "some-xray",
		ContainerType: string(contracts.ContainerXray),
		NativeJSON:    []byte(`{"this":"is-not-mihomo"}`),
	}
	if err := c.storeMgr.InboundStore().Save(xrayRec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := c.restoreAndPushInbounds(); err != nil {
		t.Fatalf("restoreAndPushInbounds: %v", err)
	}
	if len(c.ListInboundConfigs()) != 0 {
		t.Errorf("non-mihomo records leaked into mihomo map: %+v", c.ListInboundConfigs())
	}
}

// ----- Reload --------------------------------------------------------------

func TestReload_NotRunning_NoOp(t *testing.T) {
	// Reload on a container that never ran (no restClient attached)
	// must not error out — pushConfig short-circuits when restClient is
	// nil. Locks in that "Reload before Start" is a safe no-op rather
	// than a panic or error.
	c := newTestContainer(t)
	_ = c.FastAddInbound("t", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"})

	// restClient is unset — simulates Reload between NewMihomoContainer
	// and Start.
	if err := c.Reload(); err != nil {
		t.Errorf("Reload on non-running container should be no-op, got: %v", err)
	}
}

func TestReload_RepushesCurrentState(t *testing.T) {
	// Reload is the "cert/secret hot-update" path: no map mutation, just a
	// fresh PUT /configs. Verify it builds and sends the current state.
	srv := newCapturingRESTServer(t, nil)
	c := newTestContainer(t)
	attachRunningREST(c, srv.URL())

	_ = c.FastAddInbound("t", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"})
	beforeCount := srv.bodyCount()

	if err := c.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if srv.bodyCount() != beforeCount+1 {
		t.Errorf("expected exactly one additional PUT from Reload, got %d → %d", beforeCount, srv.bodyCount())
	}
	listeners := srv.latestListeners(t)
	if len(listeners) != 1 || listeners[0]["name"] != "t" {
		t.Errorf("Reload PUT body wrong: %+v", listeners)
	}
}

// ----- handleUserEvent / reconcile (stage 5) --------------------------------

func TestHandleUserEvent_Add_FansOutToAllInbounds(t *testing.T) {
	// One AddUser event; all current inbounds must get a forward rule for
	// this user. Regression guard: a naive impl that only writes the FIRST
	// inbound's rule would silently break multi-inbound subscriptions.
	//
	// Asserts BOTH addedUsers tracking and a live forward rule in
	// fwdMgr — the former alone could pass on a broken impl that does
	// markAddedUser without calling GetBindPort.
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	_ = c.FastAddInbound("vmess-a", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"})
	_ = c.FastAddInbound("trojan-b", map[string]any{"protocol": "trojan", "port": 10002, "password": "p"})

	_ = um.AddUserForTest("alice", nil)
	user := um.GetUserIncludingDeleting("alice")
	c.handleUserEvent(usermanager.UserEvent{
		Type:     usermanager.UserEventAdd,
		Username: "alice",
		User:     user,
	})

	for _, tag := range []string{"vmess-a", "trojan-b"} {
		inb, err := c.GetInboundConfig(tag)
		if err != nil {
			t.Fatalf("Get %q: %v", tag, err)
		}
		if !inb.(*MihomoInbound).hasAddedUser("alice") {
			t.Errorf("inbound %q missing alice in addedUsers", tag)
		}
		if rule := fwdMgr.GetRule("mihomo:" + tag + ":alice"); rule == nil {
			t.Errorf("inbound %q missing forward rule for alice", tag)
		}
	}
}

func TestHandleUserEvent_Remove_FansOutToAllInbounds(t *testing.T) {
	// After AddUser wires alice to both inbounds, UserEventRemove should
	// tear down forward rules on BOTH and clear addedUsers on both.
	// Asserts via fwdMgr.GetRule = nil so "unmarked addedUsers but didn't
	// actually release the forward rule" would fail.
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	_ = c.FastAddInbound("vmess-a", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"})
	_ = c.FastAddInbound("trojan-b", map[string]any{"protocol": "trojan", "port": 10002, "password": "p"})
	_ = um.AddUserForTest("alice", nil)
	user := um.GetUserIncludingDeleting("alice")

	c.handleUserEvent(usermanager.UserEvent{Type: usermanager.UserEventAdd, Username: "alice", User: user})
	// Sanity: both got added AND have live forward rules.
	for _, tag := range []string{"vmess-a", "trojan-b"} {
		inb, _ := c.GetInboundConfig(tag)
		if !inb.(*MihomoInbound).hasAddedUser("alice") {
			t.Fatalf("precondition: alice not tracked on %q", tag)
		}
		if fwdMgr.GetRule("mihomo:"+tag+":alice") == nil {
			t.Fatalf("precondition: no forward rule on %q", tag)
		}
	}

	c.handleUserEvent(usermanager.UserEvent{Type: usermanager.UserEventRemove, Username: "alice", User: user})

	for _, tag := range []string{"vmess-a", "trojan-b"} {
		inb, _ := c.GetInboundConfig(tag)
		if inb.(*MihomoInbound).hasAddedUser("alice") {
			t.Errorf("alice should be untracked from inbound %q after Remove", tag)
		}
		if rule := fwdMgr.GetRule("mihomo:" + tag + ":alice"); rule != nil {
			t.Errorf("alice's forward rule on %q should be gone after Remove, got %+v", tag, rule)
		}
	}
}

// memNodeGroupsStore is a minimal NodeGroupsStore mock for the visibility-
// flip test. Matches the helper in usermanager's own tests; we can't
// import that one (test-only). EnableClusterSync requires a live store
// to activate group-based filtering.
type memNodeGroupsStore struct {
	groups []string
}

func (s *memNodeGroupsStore) List() ([]string, error) { return s.groups, nil }
func (s *memNodeGroupsStore) Set(groups []string) error {
	s.groups = groups
	return nil
}

func TestHandleUserEvent_Update_VisibilityFlip(t *testing.T) {
	// UserEventUpdate where the user is visible → syncUserToInbound;
	// where the user is not visible → removeUserFromInbounds. Visibility
	// is driven by cluster sync + node group membership: we enable
	// cluster sync with node groups = ["alpha"], then toggle the user's
	// TargetGroup between "alpha" (visible) and "gamma" (invisible).
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	_ = fwdMgr
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	um.EnableClusterSync("default", &memNodeGroupsStore{groups: []string{"alpha"}})

	_ = c.FastAddInbound("vmess-a", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"})
	_ = um.AddUserForTest("alice", nil)
	user := um.GetUserIncludingDeleting("alice")
	user.TargetGroup = "alpha" // visible under cluster + cached groups

	// Visible path: Update adds.
	c.handleUserEvent(usermanager.UserEvent{Type: usermanager.UserEventUpdate, Username: "alice", User: user})
	inb, _ := c.GetInboundConfig("vmess-a")
	if !inb.(*MihomoInbound).hasAddedUser("alice") {
		t.Fatal("Update (visible) should add alice")
	}

	// Flip group to one the node isn't in → IsUserVisible=false.
	user.TargetGroup = "gamma"
	c.handleUserEvent(usermanager.UserEvent{Type: usermanager.UserEventUpdate, Username: "alice", User: user})
	if inb.(*MihomoInbound).hasAddedUser("alice") {
		t.Error("Update (now invisible) should remove alice")
	}
}

func TestFastAddInbound_ReconcilesExistingUsers(t *testing.T) {
	// Users exist BEFORE the inbound is added. FastAddInbound must wire
	// forward rules for each existing user after the REST push succeeds.
	// Regression guard: a stage-4-only impl that skipped this step would
	// require AddUser re-triggers or the next reconcile tick to converge.
	// Checks live forward rules, not just addedUsers tracking.
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	_ = um.AddUserForTest("alice", nil)
	_ = um.AddUserForTest("bob", nil)

	if err := c.FastAddInbound("vmess-a", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	inb, _ := c.GetInboundConfig("vmess-a")
	for _, u := range []string{"alice", "bob"} {
		if !inb.(*MihomoInbound).hasAddedUser(u) {
			t.Errorf("existing user %q not reconciled onto new inbound", u)
		}
		if rule := fwdMgr.GetRule("mihomo:vmess-a:" + u); rule == nil {
			t.Errorf("existing user %q has no forward rule on new inbound", u)
		}
	}
}

func TestRemoveInboundConfig_ReleasesAllUserPortsOnTag(t *testing.T) {
	// Two inbounds, each with alice+bob. Remove one inbound. Its forward
	// rules must be gone; the other inbound's rules must be intact.
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	_ = um.AddUserForTest("alice", nil)
	_ = um.AddUserForTest("bob", nil)

	_ = c.FastAddInbound("vmess-a", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"})
	_ = c.FastAddInbound("trojan-b", map[string]any{"protocol": "trojan", "port": 10002, "password": "p"})

	// Snapshot ForwardManager rules before removal.
	for _, user := range []string{"alice", "bob"} {
		if rule := fwdMgr.GetRule("mihomo:vmess-a:" + user); rule == nil {
			t.Fatalf("precondition: missing vmess-a rule for %q", user)
		}
		if rule := fwdMgr.GetRule("mihomo:trojan-b:" + user); rule == nil {
			t.Fatalf("precondition: missing trojan-b rule for %q", user)
		}
	}

	if err := c.RemoveInboundConfig("vmess-a"); err != nil {
		t.Fatalf("RemoveInboundConfig: %v", err)
	}

	for _, user := range []string{"alice", "bob"} {
		if rule := fwdMgr.GetRule("mihomo:vmess-a:" + user); rule != nil {
			t.Errorf("vmess-a rule for %q should be gone, got %+v", user, rule)
		}
		if rule := fwdMgr.GetRule("mihomo:trojan-b:" + user); rule == nil {
			t.Errorf("trojan-b rule for %q should survive", user)
		}
	}
}

func TestReconcileUsers_AddsMissingRemovesStale(t *testing.T) {
	// Drift scenario: alice is tracked on inbound but no longer visible
	// in UserManager; bob is visible but not tracked. One reconcileUsers
	// pass should converge — alice removed, bob added.
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	_ = fwdMgr // not all tests need it; silence unused in callers that don't
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	_ = c.FastAddInbound("vmess-a", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"})

	// Poison: mark alice as tracked without her existing in UM.
	inb, _ := c.GetInboundConfig("vmess-a")
	inb.(*MihomoInbound).markAddedUser("alice")

	// Add bob to UM but not to the inbound's addedUsers — reconcile
	// should find and add him.
	_ = um.AddUserForTest("bob", nil)

	c.reconcileUsers()

	if inb.(*MihomoInbound).hasAddedUser("alice") {
		t.Error("reconcile should have removed stale alice")
	}
	if !inb.(*MihomoInbound).hasAddedUser("bob") {
		t.Error("reconcile should have added missing bob")
	}
}

func TestReconcileUsers_InitialSyncWiresEveryUserOnEveryInbound(t *testing.T) {
	// Simulates the Start path: inbounds restored into the map and then
	// reconcileUsers is called to hydrate forward rules. Verifies every
	// (user, inbound) pair gets tracked.
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	// Build inbounds by hand (skip FastAddInbound's auto-reconcile path
	// so we can observe reconcileUsers doing the work in isolation).
	for _, spec := range []struct {
		tag, proto string
		port       uint32
		cred       MihomoSharedCred
	}{
		{"vmess-a", "vmess", 10001, MihomoSharedCred{UUID: "u"}},
		{"trojan-b", "trojan", 10002, MihomoSharedCred{Password: "p"}},
	} {
		inb := NewMihomoInbound(spec.tag, contracts.Protocol(spec.proto), spec.port, spec.cred)
		inb.SetUserManager(um)
		c.inboundsMu.Lock()
		c.inbounds[spec.tag] = inb
		c.inboundsMu.Unlock()
	}

	for _, user := range []string{"alice", "bob", "carol"} {
		_ = um.AddUserForTest(user, nil)
	}

	c.reconcileUsers()

	for _, tag := range []string{"vmess-a", "trojan-b"} {
		for _, user := range []string{"alice", "bob", "carol"} {
			if rule := fwdMgr.GetRule("mihomo:" + tag + ":" + user); rule == nil {
				t.Errorf("expected rule for (%s, %s)", tag, user)
			}
			inb, _ := c.GetInboundConfig(tag)
			if !inb.(*MihomoInbound).hasAddedUser(user) {
				t.Errorf("expected %q tracked on inbound %q", user, tag)
			}
		}
	}
}

// ----- user event pipeline end-to-end --------------------------------------

// TestUserEventPipeline_EndToEnd drives a REAL UserManager.AddUser (which
// emits UserEventAdd) through the full pipeline:
//
//	UserManager.emitEvent  → Subscribe channel
//	                       → forwardUserEvents goroutine
//	                       → container.userEventCh
//	                       → startUserEventHandler goroutine
//	                       → handleUserEvent
//	                       → syncUserToInbound
//	                       → inb.AddUser
//	                       → fwdMgr forward rule
//
// All other stage-5 event tests call `c.handleUserEvent` directly, so a
// regression in forwardUserEvents (e.g. broken drop-on-full, wrong
// select case) or startUserEventHandler (e.g. handler goroutine never
// spawned) would not be caught by them. This test guards those edges.
//
// startUserEventHandler is invoked directly rather than via Start(), since
// Start would require a real mihomo binary. That is acceptable because
// we're testing the event-delivery wiring, not the process lifecycle.
func TestUserEventPipeline_EndToEnd(t *testing.T) {
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := c.FastAddInbound("vmess-a", map[string]any{
		"protocol": "vmess",
		"port":     10001,
		"uuid":     "u",
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	// Spawn the handler goroutine — in production this runs as part of
	// Start(), but we bypass process lifecycle here.
	c.startUserEventHandler()

	// Trigger a real UserEventAdd via the regular AddUser path.
	if err := um.AddUser(usermanager.AddUserRequest{
		Username: "alice",
		Password: "pw",
	}); err != nil {
		t.Fatalf("UserManager.AddUser: %v", err)
	}

	// Event delivery is asynchronous across two goroutines. Poll for
	// convergence with a generous deadline — on a quiet host the whole
	// pipeline finishes in well under 100ms.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		inb, err := c.GetInboundConfig("vmess-a")
		if err == nil && inb.(*MihomoInbound).hasAddedUser("alice") &&
			fwdMgr.GetRule("mihomo:vmess-a:alice") != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Failure: dump the state so the reason is obvious.
	inb, _ := c.GetInboundConfig("vmess-a")
	t.Fatalf("pipeline did not converge within 2s: tracked=%v rule=%v",
		inb != nil && inb.(*MihomoInbound).hasAddedUser("alice"),
		fwdMgr.GetRule("mihomo:vmess-a:alice") != nil)
}

// ----- Restore / reconcileDrift (stage 6) -----------------------------------

// TestRestore_ReloadsStoreAndConvergesUsers exercises the full Restorable
// flow: store has two pre-seeded inbounds; a user already exists in
// UserManager. Restore must (1) load both inbounds into c.inbounds, (2)
// PUT /configs with both listeners, (3) wire forward rules for the
// existing user onto each inbound. Regression guard against a split where
// restoreAndPushInbounds is called but reconcileUsers is skipped.
func TestRestore_ReloadsStoreAndConvergesUsers(t *testing.T) {
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	// Pre-seed store with two inbounds; pre-seed UserManager with alice.
	inbs := []*MihomoInbound{
		NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"}),
		NewMihomoInbound("trojan-b", contracts.ProtocolTrojan, 10002, MihomoSharedCred{Password: "p"}),
	}
	for _, inb := range inbs {
		if err := c.persistInbound(inb); err != nil {
			t.Fatalf("persistInbound: %v", err)
		}
	}
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}

	if err := c.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// (1) Map populated.
	if got := len(c.ListInboundConfigs()); got != 2 {
		t.Errorf("inbounds in map = %d, want 2", got)
	}
	// (2) REST push happened with both listeners.
	listeners := srv.latestListeners(t)
	if len(listeners) != 2 {
		t.Errorf("listeners in PUT body = %d, want 2", len(listeners))
	}
	// (3) Forward rules for alice on both inbounds.
	for _, tag := range []string{"vmess-a", "trojan-b"} {
		if rule := fwdMgr.GetRule("mihomo:" + tag + ":alice"); rule == nil {
			t.Errorf("missing forward rule for alice on %q", tag)
		}
	}
}

// TestRestore_Idempotent verifies that calling Restore twice produces the
// same observable state with no additional side effects beyond a second
// PUT and a second reconcile sweep. Matches ContainerMgr.StartAll's call
// pattern: Start runs the hooks, then StartAll invokes Restore — the
// second pass should be a no-op as far as state goes.
func TestRestore_Idempotent(t *testing.T) {
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	if err := c.persistInbound(inb); err != nil {
		t.Fatalf("persistInbound: %v", err)
	}
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}

	if err := c.Restore(context.Background()); err != nil {
		t.Fatalf("first Restore: %v", err)
	}
	portAfterFirst, ok := um.GetUserPortByDst("alice", inb.Port())
	if !ok {
		t.Fatal("alice has no forward port after first Restore")
	}
	// Delta-capture, not a global count. Valid because this test path does
	// not invoke the Start hook (no startReconcileLoop ticker in flight).
	// If a future refactor attaches a reconcile goroutine to Option /
	// construction, this assertion will fail loudly — which is the
	// intended behaviour: the test enforces "Restore pushes exactly
	// once per call" as a contract.
	pushesAfterFirst := srv.bodyCount()

	if err := c.Restore(context.Background()); err != nil {
		t.Fatalf("second Restore: %v", err)
	}

	// Port mapping preserved — a non-idempotent impl that released+reallocated
	// would produce a different port value.
	portAfterSecond, ok := um.GetUserPortByDst("alice", inb.Port())
	if !ok {
		t.Fatal("alice lost forward port on second Restore")
	}
	if portAfterFirst != portAfterSecond {
		t.Errorf("second Restore reallocated port: %d → %d", portAfterFirst, portAfterSecond)
	}
	if rule := fwdMgr.GetRule("mihomo:vmess-a:alice"); rule == nil {
		t.Error("forward rule gone after second Restore")
	}
	if got, want := srv.bodyCount()-pushesAfterFirst, 1; got != want {
		t.Errorf("PUT delta across second Restore = %d, want %d", got, want)
	}
}

// TestRestore_ReturnsRestoreError makes sure Restore surfaces a failure
// from restoreAndPushInbounds rather than swallowing it. Here the REST
// server rejects the PUT; Restore must return the wrapped error and NOT
// fall through to reconcileUsers (which would wire alice's forward rule
// against a mihomo state that never accepted the listener).
func TestRestore_ReturnsRestoreError(t *testing.T) {
	srv := newCapturingRESTServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	attachRunningREST(c, srv.URL())

	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	if err := c.persistInbound(inb); err != nil {
		t.Fatalf("persistInbound: %v", err)
	}
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}

	err := c.Restore(context.Background())
	if err == nil {
		t.Fatal("expected Restore to surface REST failure")
	}
	// Forward rule should NOT have been wired — reconcileUsers must not run
	// when restoreAndPushInbounds fails. Otherwise we'd have forward rules
	// pointing at a listener mihomo doesn't actually have.
	if rule := fwdMgr.GetRule("mihomo:vmess-a:alice"); rule != nil {
		t.Errorf("forward rule created despite REST failure: %+v", rule)
	}
}

// TestRestore_NilStoreMgr is the "no persistence" path: a container
// constructed without WithStoreMgr (unit-test shape) should still be able
// to serve Restore without panicking or erroring. restoreAndPushInbounds
// skips the store load and pushes an empty listeners set; reconcileUsers
// then sees no inbounds and converges trivially.
func TestRestore_NilStoreMgr(t *testing.T) {
	um, _ := newTestUserManager(t)
	cfg := MihomoConfig{
		BinaryPath:         "/unused",
		ConfigFilePath:     "/unused",
		DataDir:            "/unused",
		ExternalController: "127.0.0.1:9090",
		Secret:             "",
		ReleaseTag:         "Prerelease-Alpha",
	}
	c, err := NewMihomoContainer(cfg, WithUserManager(um))
	if err != nil {
		t.Fatalf("NewMihomoContainer: %v", err)
	}
	t.Cleanup(c.Close)

	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := c.Restore(context.Background()); err != nil {
		t.Errorf("Restore without store should succeed, got: %v", err)
	}
	if got := len(c.ListInboundConfigs()); got != 0 {
		t.Errorf("inbounds map should be empty, got %d", got)
	}
}

// TestReconcileDrift_PushesConfigAndReconcilesUsers is the drift-correction
// contract: a reconcile tick PUT /configs the current map state AND does a
// user sweep. The PUT is what catches "mihomo state out of sync with map"
// (external /configs POST, mihomo restart without our replay); the user
// sweep is what catches "forward rules missing vs UserManager" (channel
// drop, transient error).
func TestReconcileDrift_PushesConfigAndReconcilesUsers(t *testing.T) {
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	_ = c.FastAddInbound("vmess-a", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"})
	// Add a user AFTER the inbound to simulate drift: if somehow the
	// UserEventAdd was dropped, the inbound wouldn't have tracked them.
	// Skip the usual handleUserEvent to manufacture the drift.
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}

	pushBefore := srv.bodyCount()

	c.reconcileDrift()

	// PUT happened (drift correction, even if mihomo state is unchanged
	// — name-keyed diff makes the no-op case cheap).
	if got, want := srv.bodyCount(), pushBefore+1; got != want {
		t.Errorf("PUT count after reconcileDrift = %d, want %d", got, want)
	}
	// alice wired onto the inbound (user drift corrected).
	if rule := fwdMgr.GetRule("mihomo:vmess-a:alice"); rule == nil {
		t.Error("reconcileDrift didn't wire alice's forward rule")
	}
}

// TestReconcileDrift_ContinuesAfterPushFailure locks in the "concerns are
// independent" guarantee: if mihomo is unreachable (or returns an error),
// the user reconcile still runs. forward-layer state is the traffic source
// of truth, so it converging despite a transient mihomo outage matters.
func TestReconcileDrift_ContinuesAfterPushFailure(t *testing.T) {
	var pushFails atomic.Bool
	srv := newCapturingRESTServer(t, func(w http.ResponseWriter, r *http.Request) {
		if pushFails.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	attachRunningREST(c, srv.URL())

	// Add inbound successfully (push OK at this point).
	_ = c.FastAddInbound("vmess-a", map[string]any{"protocol": "vmess", "port": 10001, "uuid": "u"})
	// Seed user without triggering handleUserEvent.
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}

	// Capture PUT count BEFORE flipping failure mode so the post-tick
	// delta reflects only reconcileDrift's push attempt — not the
	// preceding FastAddInbound's successful push.
	putsBeforeTick := srv.bodyCount()

	// Flip REST to fail, then tick. User reconcile should still wire alice.
	pushFails.Store(true)
	c.reconcileDrift()

	// PUT was attempted (and rejected with 500). A regression that
	// short-circuits pushConfig on any prior error state would produce
	// delta=0 and silently skip the drift correction — catching that is
	// the point of this assertion.
	if got := srv.bodyCount() - putsBeforeTick; got < 1 {
		t.Errorf("reconcileDrift should attempt pushConfig even when it'll 500, PUT delta = %d", got)
	}
	if rule := fwdMgr.GetRule("mihomo:vmess-a:alice"); rule == nil {
		t.Error("user reconcile should run even when config push fails")
	}
}

// TestRestore_AfterStartHook_IsNoOp encodes the ContainerMgr.StartAll
// interaction: the Start hook already replays store + reconciles users
// once the process is up. When StartAll then invokes Restore, the second
// pass must not undo anything. In particular:
//
//   - existing forward rules stay (no release-then-realloc churn)
//   - inbounds map stays the same
//   - an additional PUT fires (Restore pushes once per call) but it's a
//     name-keyed no-op on mihomo's side
//
// We simulate the Start hook by calling restoreAndPushInbounds +
// reconcileUsers directly, then calling Restore.
func TestRestore_AfterStartHook_IsNoOp(t *testing.T) {
	c, um, fwdMgr := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	inb := NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "u"})
	if err := c.persistInbound(inb); err != nil {
		t.Fatalf("persistInbound: %v", err)
	}
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}

	// Simulate Start hook.
	if err := c.restoreAndPushInbounds(); err != nil {
		t.Fatalf("restoreAndPushInbounds: %v", err)
	}
	c.reconcileUsers()

	portBefore, ok := um.GetUserPortByDst("alice", inb.Port())
	if !ok {
		t.Fatal("alice has no forward port after Start-hook simulation")
	}

	// Now the ContainerMgr-invoked Restore.
	if err := c.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	portAfter, ok := um.GetUserPortByDst("alice", inb.Port())
	if !ok {
		t.Fatal("alice's port vanished after Restore")
	}
	if portBefore != portAfter {
		t.Errorf("port changed across Restore: %d → %d", portBefore, portAfter)
	}
	if rule := fwdMgr.GetRule("mihomo:vmess-a:alice"); rule == nil {
		t.Error("forward rule lost after Restore")
	}
	inbs := c.ListInboundConfigs()
	if len(inbs) != 1 || inbs[0].Tag() != "vmess-a" {
		t.Errorf("inbounds map mutated: %+v", inbs)
	}
}

// --- Stage 10a: MihomoContainer.WaitReady tests ---

// TestMihomoContainer_WaitReady_Success wires a real RESTClient pointed at an
// httptest server that answers /version, then calls WaitReady directly. This
// is the happy path: GET /version returns 200, WaitReady returns nil. Asserts
// the method is implemented against the same restClient snapshot used by the
// rest of the container.
func TestMihomoContainer_WaitReady_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"version":"1.19.24","meta":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestContainer(t)
	attachRunningREST(c, srv.URL)

	if err := c.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
}

// TestMihomoContainer_WaitReady_NilRestClient covers the case where the
// container's own startProcess has never wired a restClient (e.g. the Updater
// invoked Start/Stop/WaitReady on a brand-new container that only completed
// swap but not its own boot path). WaitReady must return an error rather than
// dereferencing nil — the Updater relies on that error to trigger rollback.
func TestMihomoContainer_WaitReady_NilRestClient(t *testing.T) {
	c := newTestContainer(t)
	// c.restClient stays nil

	err := c.WaitReady(context.Background())
	if err == nil {
		t.Fatal("WaitReady must error when restClient is nil")
	}
}

// TestMihomoContainer_WaitReady_CtxCancelled verifies WaitReady honours the
// caller's ctx — an already-cancelled ctx must produce an error without
// hanging. The stage 10a Updater passes a bounded ctx so a stuck probe can't
// hold the Update path open indefinitely.
func TestMihomoContainer_WaitReady_CtxCancelled(t *testing.T) {
	// Server that never returns — guarantees the probe can only finish via ctx.
	block := make(chan struct{})
	defer close(block)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()

	c := newTestContainer(t)
	attachRunningREST(c, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	err := c.WaitReady(ctx)
	if err == nil {
		t.Fatal("WaitReady with cancelled ctx must error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitReady error must chain context.Canceled, got %v", err)
	}
}
