package mihomo

import (
	"path/filepath"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	errs "github.com/lureiny/v2raymg/pkg/proxy/errors"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

// newClaimingContainer builds a container wired to a real port authority over a
// small range, so tests can assert which ports were actually taken.
func newClaimingContainer(t *testing.T, minPort, maxPort uint32) (*MihomoContainer, *forward.DefaultForwardManager) {
	t.Helper()

	dsn := filepath.Join(t.TempDir(), "test.db")
	sm, err := store.NewStoreManager(dsn, migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })

	fm, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort: minPort,
		MaxPort: maxPort,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	t.Cleanup(func() { _ = fm.Close() })

	cfg := MihomoConfig{
		BinaryPath:         "/unused",
		ConfigFilePath:     "/unused",
		DataDir:            t.TempDir(),
		ExternalController: "127.0.0.1:9090",
		ReleaseTag:         "Prerelease-Alpha",
	}
	c, err := NewMihomoContainer(cfg, WithStoreMgr(sm), WithPortClaimer(fm))
	if err != nil {
		t.Fatalf("NewMihomoContainer: %v", err)
	}
	return c, fm
}

func ssParams(port any) map[string]any {
	p := map[string]any{
		"protocol": "shadowsocks",
		"cipher":   "2022-blake3-aes-256-gcm",
		"password": "9JfKQmXHnTbVQm9zXtQd1cV0fZ3vJ8hLmNpQrStUvWx=",
	}
	if port != nil {
		p["port"] = port
	}
	return p
}

// An omitted port must be drawn from the shared allocator and recorded there.
// Before this, port 0 became a hardcoded 10000 for every such inbound, so the
// second one silently failed to bind.
func TestFastAddInbound_AutoPortComesFromTheAllocator(t *testing.T) {
	c, fm := newClaimingContainer(t, 25000, 25001)

	if err := c.FastAddInbound("ss-a", ssParams(uint32(0))); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	inb, err := c.GetInboundConfig("ss-a")
	if err != nil {
		t.Fatalf("GetInboundConfig: %v", err)
	}
	if inb.Port() < 25000 || inb.Port() > 25001 {
		t.Fatalf("port %d is outside the allocator range; something is picking ports on its own", inb.Port())
	}
	if err := fm.AllocateSpecificPort(inb.Port()); err == nil {
		t.Fatalf("port %d was handed out but not recorded", inb.Port())
	}

	// A second auto-port inbound must land somewhere else — the exact failure
	// that produced the production EADDRINUSE.
	if err := c.FastAddInbound("ss-b", ssParams(uint32(0))); err != nil {
		t.Fatalf("second FastAddInbound: %v", err)
	}
	other, err := c.GetInboundConfig("ss-b")
	if err != nil {
		t.Fatalf("GetInboundConfig: %v", err)
	}
	if other.Port() == inb.Port() {
		t.Fatalf("two auto-port inbounds got the same port %d", inb.Port())
	}
}

func TestFastAddInbound_ExplicitPortConflictIsRejectedAndNotPersisted(t *testing.T) {
	c, _ := newClaimingContainer(t, 25100, 25199)

	if err := c.FastAddInbound("ss-a", ssParams(uint32(25150))); err != nil {
		t.Fatalf("first FastAddInbound: %v", err)
	}

	err := c.FastAddInbound("ss-b", ssParams(uint32(25150)))
	if err == nil {
		t.Fatal("expected the second inbound on the same explicit port to be rejected")
	}
	if !errs.HasCode(err, errs.ErrPortInUse) {
		t.Fatalf("want ErrPortInUse, got %v", err)
	}

	// Rejected means rejected: no map entry, and nothing left in the store to
	// be replayed into a doomed listener on the next start.
	if _, err := c.GetInboundConfig("ss-b"); err == nil {
		t.Fatal("the rejected inbound must not be registered")
	}
	records, err := c.storeMgr.InboundStore().Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, rec := range records {
		if rec.Tag == "ss-b" {
			t.Fatal("the rejected inbound must not be persisted")
		}
	}
}

// A failed add must give its port back, or a run of failures slowly drains the
// pool for the lifetime of the process.
func TestFastAddInbound_ReleasesThePortWhenTheAddFails(t *testing.T) {
	c, fm := newClaimingContainer(t, 25200, 25299)

	// Duplicate tag: the claim happens first, then addInboundLocked rejects.
	if err := c.FastAddInbound("dup", ssParams(uint32(25250))); err != nil {
		t.Fatalf("first FastAddInbound: %v", err)
	}
	if err := c.FastAddInbound("dup", ssParams(uint32(25251))); err == nil {
		t.Fatal("expected duplicate tag to be rejected")
	}
	if err := fm.AllocateSpecificPort(25251); err != nil {
		t.Fatalf("port 25251 should have been released after the failed add: %v", err)
	}

	// A build failure (unknown protocol) must release too.
	fm.ReleasePort(25251)
	if err := c.FastAddInbound("bad", map[string]any{"protocol": "nope", "port": uint32(25252)}); err == nil {
		t.Fatal("expected an invalid protocol to be rejected")
	}
	if err := fm.AllocateSpecificPort(25252); err != nil {
		t.Fatalf("port 25252 should have been released after the failed build: %v", err)
	}
}

func TestRemoveInboundConfig_ReleasesThePort(t *testing.T) {
	c, fm := newClaimingContainer(t, 25300, 25399)

	if err := c.FastAddInbound("ss-a", ssParams(uint32(25350))); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	if err := fm.AllocateSpecificPort(25350); err == nil {
		t.Fatal("port should be claimed while the inbound exists")
	}

	if err := c.RemoveInboundConfig("ss-a"); err != nil {
		t.Fatalf("RemoveInboundConfig: %v", err)
	}
	if err := fm.AllocateSpecificPort(25350); err != nil {
		t.Fatalf("port should be released after removal: %v", err)
	}
}

// The params map belongs to the caller; resolving a port must not write back
// into it, or a failed add leaves the caller holding a port it never got.
func TestFastAddInbound_DoesNotMutateCallerParams(t *testing.T) {
	c, _ := newClaimingContainer(t, 25400, 25499)

	params := ssParams(uint32(0))
	if err := c.FastAddInbound("ss-a", params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	if got := params["port"]; got != uint32(0) {
		t.Fatalf("caller's params were mutated: port = %v, want 0", got)
	}
}

// Without a claimer, an explicit port still works (isolated unit tests), but
// "you pick" has nothing to pick from and must say so rather than inventing a
// constant.
func TestFastAddInbound_NoClaimerRejectsAutoPort(t *testing.T) {
	c := newTestContainer(t) // no WithPortClaimer

	if err := c.FastAddInbound("ss-explicit", ssParams(uint32(25500))); err != nil {
		t.Fatalf("explicit port without a claimer should still work: %v", err)
	}

	err := c.FastAddInbound("ss-auto", ssParams(uint32(0)))
	if err == nil {
		t.Fatal("expected an error: no authority to draw a port from")
	}
	if !errs.HasCode(err, errs.ErrPortAllocationFail) {
		t.Fatalf("want ErrPortAllocationFail, got %v", err)
	}
}

var _ container.PortClaimer = (*forward.DefaultForwardManager)(nil)
