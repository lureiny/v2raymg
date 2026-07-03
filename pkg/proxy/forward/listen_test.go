package forward

import (
	"net"
	"reflect"
	"strings"
	"testing"
)

// assertUDPRelay asserts that r forwards over UDP, transparently unwrapping the
// dual-stack *multiRelay (each child must itself be a *UDPRelay).
func assertUDPRelay(t *testing.T, r Relay) {
	t.Helper()
	switch v := r.(type) {
	case *UDPRelay:
		return
	case *multiRelay:
		if len(v.children) == 0 {
			t.Fatal("multiRelay has no children")
		}
		for _, c := range v.children {
			if _, ok := c.relay.(*UDPRelay); !ok {
				t.Fatalf("expected *UDPRelay child, got %T", c.relay)
			}
		}
	default:
		t.Fatalf("expected *UDPRelay or *multiRelay, got %T", r)
	}
}

// ipv6WildcardBindable reports whether the host can bind an IPv6 wildcard
// listener. Some CI/sandbox network namespaces cannot, so IPv6-dependent tests
// skip when this is false.
func ipv6WildcardBindable() bool {
	l, err := net.Listen("tcp6", "[::]:0")
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func TestNormalizeListenStack(t *testing.T) {
	cases := map[string]string{
		"":      "",
		"  ":    "",
		"dual":  ListenStackDual,
		"DUAL":  ListenStackDual,
		"both":  ListenStackDual,
		"ipv4":  ListenStackIPv4,
		"v4":    ListenStackIPv4,
		"IPv6":  ListenStackIPv6,
		"v6":    ListenStackIPv6,
		"bogus": "",
		"ipv4x": "",
	}
	for in, want := range cases {
		if got := normalizeListenStack(in); got != want {
			t.Errorf("normalizeListenStack(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstListenStack(t *testing.T) {
	if got := firstListenStack("", ""); got != ListenStackDual {
		t.Errorf("all-empty => %q, want dual", got)
	}
	if got := firstListenStack("", "ipv4"); got != ListenStackIPv4 {
		t.Errorf("fallback to default => %q, want ipv4", got)
	}
	if got := firstListenStack("ipv6", "ipv4"); got != ListenStackIPv6 {
		t.Errorf("rule overrides default => %q, want ipv6", got)
	}
	if got := firstListenStack("bogus", "ipv4"); got != ListenStackIPv4 {
		t.Errorf("unrecognized rule falls through => %q, want ipv4", got)
	}
}

func TestResolveListenEndpoints(t *testing.T) {
	cases := []struct {
		name         string
		listenAddr   string
		ruleStack    string
		defaultStack string
		port         uint32
		want         []listenEndpoint
		wantErr      bool
	}{
		{
			name:       "specific ipv4",
			listenAddr: "10.0.0.5",
			port:       8080,
			want:       []listenEndpoint{{address: "10.0.0.5:8080"}},
		},
		{
			name:       "specific ipv6 is bracketed",
			listenAddr: "2001:db8::1",
			port:       8080,
			want:       []listenEndpoint{{address: "[2001:db8::1]:8080"}},
		},
		{
			name:       "explicit v4 wildcard literal is pinned to family 4",
			listenAddr: "0.0.0.0",
			port:       8080,
			want:       []listenEndpoint{{address: "0.0.0.0:8080", family: "4"}},
		},
		{
			name:       "explicit v6 wildcard literal is pinned to family 6",
			listenAddr: "::",
			port:       8080,
			want:       []listenEndpoint{{address: "[::]:8080", family: "6"}},
		},
		{
			name: "empty inherits default dual, both best-effort",
			port: 9000,
			want: []listenEndpoint{
				{address: "0.0.0.0:9000", family: "4", optional: true},
				{address: "[::]:9000", family: "6", optional: true},
			},
		},
		{
			name:      "rule ipv4 only",
			ruleStack: ListenStackIPv4,
			port:      9000,
			want:      []listenEndpoint{{address: "0.0.0.0:9000", family: "4"}},
		},
		{
			name:      "rule ipv6 only is family 6",
			ruleStack: ListenStackIPv6,
			port:      9000,
			want:      []listenEndpoint{{address: "[::]:9000", family: "6"}},
		},
		{
			name:         "default ipv4 applies when rule unset",
			defaultStack: ListenStackIPv4,
			port:         9000,
			want:         []listenEndpoint{{address: "0.0.0.0:9000", family: "4"}},
		},
		{
			name:         "rule overrides default",
			ruleStack:    ListenStackIPv6,
			defaultStack: ListenStackIPv4,
			port:         9000,
			want:         []listenEndpoint{{address: "[::]:9000", family: "6"}},
		},
		{
			name:       "specific address ignores stack",
			listenAddr: "127.0.0.1",
			ruleStack:  ListenStackIPv6,
			port:       9000,
			want:       []listenEndpoint{{address: "127.0.0.1:9000"}},
		},
		{
			name:       "invalid listen addr errors",
			listenAddr: "not-an-ip",
			port:       9000,
			wantErr:    true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveListenEndpoints(c.listenAddr, c.ruleStack, c.defaultStack, c.port)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("endpoints = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestValidate_ListenFields(t *testing.T) {
	base := func() ForwardRule {
		return ForwardRule{Username: "u@t.com", TargetAddr: "127.0.0.1:443"}
	}

	valid := []ForwardRule{
		func() ForwardRule { r := base(); return r }(),
		func() ForwardRule { r := base(); r.ListenStack = "ipv4"; return r }(),
		func() ForwardRule { r := base(); r.ListenStack = "IPv6"; return r }(),
		func() ForwardRule { r := base(); r.ListenStack = "dual"; return r }(),
		func() ForwardRule { r := base(); r.ListenAddr = "192.168.1.1"; return r }(),
		func() ForwardRule { r := base(); r.ListenAddr = "2001:db8::1"; return r }(),
	}
	for i, r := range valid {
		if err := r.Validate(); err != nil {
			t.Errorf("valid[%d] unexpected error: %v", i, err)
		}
	}

	invalid := []ForwardRule{
		func() ForwardRule { r := base(); r.ListenStack = "ipv5"; return r }(),
		func() ForwardRule { r := base(); r.ListenAddr = "example.com"; return r }(),
		func() ForwardRule { r := base(); r.ListenAddr = "999.1.1.1"; return r }(),
	}
	for i, r := range invalid {
		if err := r.Validate(); err == nil {
			t.Errorf("invalid[%d] expected error, got nil", i)
		}
	}
}

// TestAddRule_IPv4Only_SingleRelay verifies an explicit ipv4-only rule binds a
// single TCP relay on the IPv4 wildcard (no multiRelay wrapper).
func TestAddRule_IPv4Only_SingleRelay(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	created, err := m.AddRule(ForwardRule{
		Username:    "v4@test.com",
		TargetAddr:  "127.0.0.1:9",
		ListenStack: ListenStackIPv4,
	})
	if err != nil {
		t.Fatalf("AddRule ipv4: %v", err)
	}
	mr := m.rules[created.RuleKey()]
	if _, ok := mr.relay.(*TCPRelay); !ok {
		t.Fatalf("expected *TCPRelay, got %T", mr.relay)
	}
	// tcp4 forces a real AF_INET socket, so the bound address is 0.0.0.0
	// (NOT promoted to a dual-stack [::] socket the way a bare "tcp" listen is).
	if !strings.HasPrefix(mr.relay.ListenAddr(), "0.0.0.0:") {
		t.Errorf("listen addr = %q, want 0.0.0.0:* (real AF_INET, not dual-stack)", mr.relay.ListenAddr())
	}
}

// TestAddRule_Dual_Default_MultiRelay verifies the default (unset) rule uses a
// dual-stack multiRelay whose IPv4 half is always up; on IPv6-capable hosts the
// IPv6 half comes up too, as a separate socket.
func TestAddRule_Dual_Default_MultiRelay(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	created, err := m.AddRule(ForwardRule{
		Username:   "dual@test.com",
		TargetAddr: "127.0.0.1:9",
	})
	if err != nil {
		t.Fatalf("AddRule dual: %v", err)
	}
	mr := m.rules[created.RuleKey()]
	if _, ok := mr.relay.(*multiRelay); !ok {
		t.Fatalf("expected *multiRelay, got %T", mr.relay)
	}
	addr := mr.relay.ListenAddr()
	if !strings.Contains(addr, "0.0.0.0:") {
		t.Errorf("dual listen addr = %q, want it to include the IPv4 wildcard", addr)
	}
	if ipv6WildcardBindable() && !strings.Contains(addr, "[::]:") {
		t.Errorf("dual listen addr = %q, want it to include the IPv6 wildcard on an IPv6-capable host", addr)
	}
}

// TestAddRule_IPv6Only binds an explicit ipv6-only rule. Skipped on hosts that
// cannot bind an IPv6 wildcard.
func TestAddRule_IPv6Only(t *testing.T) {
	if !ipv6WildcardBindable() {
		t.Skip("host cannot bind an IPv6 wildcard listener")
	}
	m := newTestManager(t)
	defer m.Close()

	created, err := m.AddRule(ForwardRule{
		Username:    "v6@test.com",
		TargetAddr:  "127.0.0.1:9",
		ListenStack: ListenStackIPv6,
	})
	if err != nil {
		t.Fatalf("AddRule ipv6: %v", err)
	}
	mr := m.rules[created.RuleKey()]
	if _, ok := mr.relay.(*TCPRelay); !ok {
		t.Fatalf("expected *TCPRelay, got %T", mr.relay)
	}
	if !strings.HasPrefix(mr.relay.ListenAddr(), "[::]:") {
		t.Errorf("listen addr = %q, want [::]:* (tcp6, IPV6_V6ONLY)", mr.relay.ListenAddr())
	}
}
