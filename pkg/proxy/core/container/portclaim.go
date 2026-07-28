package container

import (
	"encoding/json"
	"strconv"
	"strings"

	errs "github.com/lureiny/v2raymg/pkg/proxy/errors"
	"github.com/lureiny/v2raymg/pkg/store"
)

// ClaimInboundPort resolves an inbound's backend port through the shared
// allocator. It is the only sanctioned way for a container to decide what port
// its internal listener binds.
//
// Note this port is for the container's own 127.0.0.1 listener only; the public
// relay in front of it is created separately via UserManager.GetBindPort ->
// forward.AddRule, and the two must never be conflated.
//
// Two cases, deliberately asymmetric:
//
//   - port == 0 — nobody chose, so we draw one. The returned port is already
//     recorded, so no other component can be handed it.
//   - port != 0 — an operator or a persisted record named this exact port. We
//     claim it or fail; we never substitute. A named port may already be in a
//     firewall rule or a client config, so silently moving it would break
//     things elsewhere while looking like success. This mirrors the forward
//     side's rule that a pinned ListenPort is never retried onto another port.
//
// A nil claimer means no authority was injected — the isolated-unit-test path.
// An explicit port is then returned unchanged, but port == 0 is an error: there
// is nothing to draw from, and inventing a fallback constant is exactly the bug
// this function exists to remove (every caller that omitted a port used to land
// on the same hardcoded 10000). Production always injects a claimer via
// BuildOptions, so this only ever fires on a wiring mistake.
func ClaimInboundPort(pc PortClaimer, port uint32) (uint32, error) {
	if pc == nil {
		if port == 0 {
			return 0, errs.New(errs.ErrPortAllocationFail,
				"cannot allocate an inbound port: no port authority is wired "+
					"(BuildOptions.PortClaimer is nil); pass an explicit port or inject one")
		}
		return port, nil
	}
	if port == 0 {
		allocated, err := pc.AllocatePort()
		if err != nil {
			return 0, errs.Wrap(errs.ErrPortAllocationFail,
				"no port available for inbound", err)
		}
		return allocated, nil
	}
	if err := pc.AllocateSpecificPort(port); err != nil {
		return 0, err
	}
	return port, nil
}

// ReleaseInboundPort returns a port claimed by ClaimInboundPort. Idempotent and
// nil-safe, so teardown paths can call it unconditionally.
func ReleaseInboundPort(pc PortClaimer, port uint32) {
	if pc == nil || port == 0 {
		return
	}
	pc.ReleasePort(port)
}

// ParsePersistedPort extracts the listen port from a stored inbound record
// without going through the owning container's decoder.
//
// This exists so the startup pre-claim pass can walk the whole inbounds table
// before any container is constructed — the ordering matters, because a
// container that starts early allocates its users' forward ports, and a
// container that starts later restores inbound ports that were pinned by a
// previous run and cannot move out of the way.
//
// It relies on a convention that holds across all four containers today: the
// port lives at the top level of native_json under "port". The JSON type
// differs — xray writes a string (its gRPC API demands one, and it accepts
// ranges like "1000-2000", of which we take the low end), everyone else writes
// a number.
//
// Returns false rather than guessing when the shape is unrecognised. The caller
// warns and skips: a port we cannot read is a port we cannot protect, and
// inventing one would be worse than admitting the gap.
func ParsePersistedPort(rec *store.InboundRecord) (uint32, bool) {
	if rec == nil || len(rec.NativeJSON) == 0 {
		return 0, false
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rec.NativeJSON, &fields); err != nil {
		return 0, false
	}
	raw, ok := fields["port"]
	if !ok {
		return 0, false
	}

	// Number form (mihomo / hysteria / snell, and xray's native path when the
	// caller supplied a numeric port).
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil {
		if num != float64(uint32(num)) || num <= 0 || num > 65535 {
			return 0, false
		}
		return uint32(num), true
	}

	// String form (xray), possibly a "lo-hi" range.
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return 0, false
	}
	if lo, _, found := strings.Cut(str, "-"); found {
		str = lo
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(str), 10, 32)
	if err != nil || parsed == 0 || parsed > 65535 {
		return 0, false
	}
	return uint32(parsed), true
}
