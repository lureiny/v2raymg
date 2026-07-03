package forward

import (
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/pkg/log"
)

// relayChild is one member of a multiRelay together with its criticality.
type relayChild struct {
	relay    Relay
	optional bool // best-effort: a Start failure is logged and skipped, not fatal
}

// multiRelay fans a single logical forward rule across multiple bound sockets
// (e.g. an IPv4 wildcard plus an IPv6 wildcard for dual-stack listening). All
// child relays share the rule's TrafficCounter and user-level limiters, so
// counting and client limits behave as one rule regardless of which socket a
// connection arrived on.
//
// It satisfies the Relay interface so DefaultForwardManager can store it in the
// same single-relay slot as a plain TCP/UDP relay.
type multiRelay struct {
	children []relayChild
	started  []Relay // children that started successfully (populated by Start)
}

func newMultiRelay(children []relayChild) *multiRelay {
	return &multiRelay{children: children}
}

// Start starts each child in order. A required child failing rolls back every
// already-started child and returns the error. An optional child failing is
// logged and skipped (e.g. binding [::] on an IPv6-disabled host). Start
// succeeds as long as at least one child came up.
func (m *multiRelay) Start() error {
	for _, c := range m.children {
		if err := c.relay.Start(); err != nil {
			if c.optional {
				log.Warn("[ForwardManager] optional listener unavailable, skipping",
					"addr", c.relay.ListenAddr(), "err", err)
				continue
			}
			for _, s := range m.started {
				s.Stop()
			}
			m.started = nil
			return err
		}
		m.started = append(m.started, c.relay)
	}
	if len(m.started) == 0 {
		return fmt.Errorf("no listener could be started")
	}
	return nil
}

// Stop stops every successfully-started child. Idempotent per child.
func (m *multiRelay) Stop() {
	for _, s := range m.started {
		s.Stop()
	}
}

// ListenAddr returns the comma-joined bound addresses of the started children.
func (m *multiRelay) ListenAddr() string {
	addrs := make([]string, 0, len(m.started))
	for _, s := range m.started {
		addrs = append(addrs, s.ListenAddr())
	}
	return strings.Join(addrs, ",")
}

// Compile-time interface conformance check.
var _ Relay = (*multiRelay)(nil)
