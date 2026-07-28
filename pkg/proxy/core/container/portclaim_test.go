package container

import (
	"fmt"
	"testing"

	errs "github.com/lureiny/v2raymg/pkg/proxy/errors"
	"github.com/lureiny/v2raymg/pkg/store"
)

// fakeClaimer is a minimal PortClaimer with a deterministic draw sequence, so
// tests can assert which branch ClaimInboundPort took.
type fakeClaimer struct {
	next      uint32
	claimed   map[uint32]bool
	allocErr  error
	drawCalls int
}

func newFakeClaimer(first uint32) *fakeClaimer {
	return &fakeClaimer{next: first, claimed: map[uint32]bool{}}
}

func (f *fakeClaimer) AllocatePort() (uint32, error) {
	f.drawCalls++
	if f.allocErr != nil {
		return 0, f.allocErr
	}
	for f.claimed[f.next] {
		f.next++
	}
	p := f.next
	f.claimed[p] = true
	f.next++
	return p, nil
}

func (f *fakeClaimer) AllocateSpecificPort(port uint32) error {
	if f.claimed[port] {
		return errs.Newf(errs.ErrPortInUse, "port %d already claimed", port)
	}
	f.claimed[port] = true
	return nil
}

func (f *fakeClaimer) ReleasePort(port uint32) { delete(f.claimed, port) }

func TestClaimInboundPort_ZeroDrawsAndRecords(t *testing.T) {
	fc := newFakeClaimer(20000)

	got, err := ClaimInboundPort(fc, 0)
	if err != nil {
		t.Fatalf("ClaimInboundPort(0): %v", err)
	}
	if got != 20000 {
		t.Fatalf("want drawn port 20000, got %d", got)
	}
	if !fc.claimed[got] {
		t.Fatal("a drawn port must be recorded, otherwise it can be handed out twice")
	}

	// A second draw must not repeat the first.
	second, err := ClaimInboundPort(fc, 0)
	if err != nil {
		t.Fatalf("second ClaimInboundPort(0): %v", err)
	}
	if second == got {
		t.Fatalf("draw repeated port %d", got)
	}
}

func TestClaimInboundPort_ExplicitIsClaimedVerbatim(t *testing.T) {
	fc := newFakeClaimer(20000)

	got, err := ClaimInboundPort(fc, 8443)
	if err != nil {
		t.Fatalf("ClaimInboundPort(8443): %v", err)
	}
	if got != 8443 {
		t.Fatalf("an explicit port must come back unchanged, got %d", got)
	}
	if fc.drawCalls != 0 {
		t.Fatal("an explicit port must not consult the random draw")
	}
}

// The central contract: a named port that is taken is an error, never a
// silent substitution. Whoever named it may already have it in a firewall rule
// or a client config.
func TestClaimInboundPort_ExplicitConflictIsRejectedNotSubstituted(t *testing.T) {
	fc := newFakeClaimer(20000)
	if err := fc.AllocateSpecificPort(8443); err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	got, err := ClaimInboundPort(fc, 8443)
	if err == nil {
		t.Fatalf("expected conflict error, got port %d", got)
	}
	if !errs.HasCode(err, errs.ErrPortInUse) {
		t.Fatalf("want ErrPortInUse, got %v", err)
	}
	if got != 0 {
		t.Fatalf("a failed claim must not return a usable port, got %d", got)
	}
	if fc.drawCalls != 0 {
		t.Fatal("a conflicting explicit port must NOT fall back to drawing another")
	}
}

func TestClaimInboundPort_ExhaustionIsWrapped(t *testing.T) {
	fc := newFakeClaimer(20000)
	fc.allocErr = fmt.Errorf("pool empty")

	if _, err := ClaimInboundPort(fc, 0); !errs.HasCode(err, errs.ErrPortAllocationFail) {
		t.Fatalf("want ErrPortAllocationFail, got %v", err)
	}
}

func TestClaimInboundPort_NilClaimer(t *testing.T) {
	// An explicit port needs no authority to resolve — pass it through so
	// isolated unit tests can construct containers without wiring one.
	got, err := ClaimInboundPort(nil, 1234)
	if err != nil || got != 1234 {
		t.Fatalf("nil claimer with an explicit port should pass through: got %d, %v", got, err)
	}

	// "You pick" with nothing to pick from must fail loudly. Returning 0, or
	// substituting some constant, is precisely the bug this replaced: every
	// caller that omitted a port used to land on the same hardcoded 10000.
	if _, err := ClaimInboundPort(nil, 0); !errs.HasCode(err, errs.ErrPortAllocationFail) {
		t.Fatalf("nil claimer with port 0 must fail with ErrPortAllocationFail, got %v", err)
	}

	ReleaseInboundPort(nil, 1234) // must not panic
}

func TestReleaseInboundPort(t *testing.T) {
	fc := newFakeClaimer(20000)
	if err := fc.AllocateSpecificPort(9443); err != nil {
		t.Fatalf("claim: %v", err)
	}

	ReleaseInboundPort(fc, 0) // no-op, must not touch anything
	if !fc.claimed[9443] {
		t.Fatal("releasing port 0 must not release anything else")
	}

	ReleaseInboundPort(fc, 9443)
	if fc.claimed[9443] {
		t.Fatal("port should be released")
	}
}

// ParsePersistedPort must handle every shape actually written to the inbounds
// table today. If a container ever changes its native_json layout, this is the
// test that says so before the startup pre-claim pass silently stops
// protecting that container's ports.
func TestParsePersistedPort_RealShapes(t *testing.T) {
	cases := []struct {
		name string
		json string
		want uint32
		ok   bool
	}{
		{
			name: "xray string port",
			json: `{"tag":"vmess-tcp","protocol":"vmess","port":"443","listen":"127.0.0.1"}`,
			want: 443, ok: true,
		},
		{
			name: "xray range string takes the low end",
			json: `{"tag":"r","protocol":"vmess","port":"1000-2000"}`,
			want: 1000, ok: true,
		},
		{
			name: "mihomo number port",
			json: `{"tag":"ss-1","protocol":"shadowsocks","port":10000,"listen_addr":"127.0.0.1"}`,
			want: 10000, ok: true,
		},
		{
			name: "hysteria number port",
			json: `{"tag":"hysteria","protocol":"hysteria2","port":9443,"listen":"127.0.0.1","enabled":true}`,
			want: 9443, ok: true,
		},
		{
			name: "snell number port",
			json: `{"tag":"snell","protocol":"snell","port":16160,"listen":"127.0.0.1","psk":"x"}`,
			want: 16160, ok: true,
		},
		{name: "missing key", json: `{"tag":"x","protocol":"vmess"}`},
		{name: "null port", json: `{"port":null}`},
		{name: "zero port", json: `{"port":0}`},
		{name: "out of range", json: `{"port":70000}`},
		{name: "non-numeric string", json: `{"port":"auto"}`},
		{name: "object port", json: `{"port":{"from":1,"to":2}}`},
		{name: "malformed json", json: `{"port":`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParsePersistedPort(&store.InboundRecord{
				Tag:        "t",
				NativeJSON: []byte(tc.json),
			})
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (port %d)", ok, tc.ok, got)
			}
			if ok && got != tc.want {
				t.Fatalf("port = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestParsePersistedPort_NilAndEmpty(t *testing.T) {
	if _, ok := ParsePersistedPort(nil); ok {
		t.Fatal("nil record must not yield a port")
	}
	if _, ok := ParsePersistedPort(&store.InboundRecord{Tag: "t"}); ok {
		t.Fatal("empty native_json must not yield a port")
	}
}
