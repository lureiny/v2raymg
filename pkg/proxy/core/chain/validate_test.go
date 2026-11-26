package chain

import (
	"testing"
)

func TestValidateSpec_AddressPolicy_PlanA(t *testing.T) {
	basePolicy := AddressPolicy{
		AllowLoopback: false,
		LocalHosts:    []string{"203.0.113.10", "node1.example.com", "100.64.0.99", "[2001:db8::99]"},
		ReservedPorts: map[uint32]struct{}{
			8080:  {},
			9090:  {},
			62789: {},
		},
		IsPortAllocated: func(p uint32) bool {
			return p == 20001 || p == 20002
		},
	}

	t.Run("unconditionally rejected", func(t *testing.T) {
		policyWithLoopback := basePolicy
		policyWithLoopback.AllowLoopback = true

		badSpecs := []struct {
			name string
			spec *OutboundSpec
		}{
			{"nil spec", nil},
			{"empty host", &OutboundSpec{Host: "", Port: 443}},
			{"whitespace host", &OutboundSpec{Host: "   ", Port: 443}},
			{"port 0", &OutboundSpec{Host: "8.8.8.8", Port: 0}},
			{"port > 65535", &OutboundSpec{Host: "8.8.8.8", Port: 65536}},
			{"unspecified ipv4", &OutboundSpec{Host: "0.0.0.0", Port: 443}},
			{"this-network non-zero 0.0.0.1", &OutboundSpec{Host: "0.0.0.1", Port: 443}},
			{"this-network non-zero 0.1.2.3", &OutboundSpec{Host: "0.1.2.3", Port: 443}},
			{"v4-mapped this-network", &OutboundSpec{Host: "::ffff:0.1.2.3", Port: 443}},
			{"unspecified ipv6", &OutboundSpec{Host: "::", Port: 443}},
			{"v4-mapped unspecified", &OutboundSpec{Host: "::ffff:0.0.0.0", Port: 443}},
			{"short numeric ipv4 127.1", &OutboundSpec{Host: "127.1", Port: 443}},
			{"decimal integer ipv4 2130706433", &OutboundSpec{Host: "2130706433", Port: 443}},
			{"octal ipv4 0177.0.0.1", &OutboundSpec{Host: "0177.0.0.1", Port: 443}},
			{"hex integer ipv4 0x7f000001", &OutboundSpec{Host: "0x7f000001", Port: 443}},
			{"hex dotted ipv4 0x7f.0.0.1", &OutboundSpec{Host: "0x7f.0.0.1", Port: 443}},
			{"trailing dot loopback ipv4", &OutboundSpec{Host: "127.0.0.1.", Port: 443}},
			{"trailing dot metadata ipv4", &OutboundSpec{Host: "169.254.169.254.", Port: 80}},
			{"trailing dot ipv6", &OutboundSpec{Host: "::1.", Port: 443}},
			{"trailing dot bracketed ipv6", &OutboundSpec{Host: "[::1].", Port: 443}},
			{"trailing dot short ipv4", &OutboundSpec{Host: "127.1.", Port: 443}},
			{"double trailing dot localhost", &OutboundSpec{Host: "localhost..", Port: 7890}},
			{"double trailing dot local domain reserved port", &OutboundSpec{Host: "node1.example.com..", Port: 9090}},
			{"host containing colon port", &OutboundSpec{Host: "127.0.0.1:443", Port: 443}},
			{"ipv4 with zone suffix", &OutboundSpec{Host: "127.0.0.1%eth0", Port: 443}},
			{"multicast ipv4", &OutboundSpec{Host: "224.0.0.1", Port: 443}},
			{"multicast ipv6", &OutboundSpec{Host: "ff02::1", Port: 443}},
			{"broadcast", &OutboundSpec{Host: "255.255.255.255", Port: 443}},
			{"cloud metadata", &OutboundSpec{Host: "169.254.169.254", Port: 80}},
			{"link-local ipv4", &OutboundSpec{Host: "169.254.1.2", Port: 443}},
			{"v4-mapped metadata", &OutboundSpec{Host: "::ffff:169.254.169.254", Port: 80}},
			{"link-local ipv6", &OutboundSpec{Host: "fe80::1", Port: 443}},
			{"link-local ipv6 zoned", &OutboundSpec{Host: "fe80::1%eth0", Port: 443}},
			{"local host reserved port", &OutboundSpec{Host: "203.0.113.10", Port: 62789}},
			{"local host allocated port", &OutboundSpec{Host: "203.0.113.10", Port: 20001}},
			{"local domain reserved port case", &OutboundSpec{Host: "NODE1.EXAMPLE.COM.", Port: 9090}},
			{"local tailscale IP allocated port", &OutboundSpec{Host: "100.64.0.99", Port: 20002}},
			{"local ipv6 reserved port", &OutboundSpec{Host: "2001:db8::99", Port: 8080}},
			{"loopback reserved port even with AllowLoopback", &OutboundSpec{Host: "127.0.0.1", Port: 62789}},
			{"loopback allocated port even with AllowLoopback", &OutboundSpec{Host: "::1", Port: 20001}},
			{"localhost allocated port even with AllowLoopback", &OutboundSpec{Host: "localhost", Port: 20001}},
			{"invalid domain chars", &OutboundSpec{Host: "evil.com/path", Port: 443}},
		}
		for _, tc := range badSpecs {
			t.Run(tc.name, func(t *testing.T) {
				if err := ValidateSpec(tc.spec, policyWithLoopback); err == nil {
					t.Errorf("expected ValidateSpec(%+v) to reject, got nil", tc.spec)
				}
			})
		}
	})

	t.Run("loopback rejected by default and allowed when enabled on safe port", func(t *testing.T) {
		loopbacks := []*OutboundSpec{
			{Host: "127.0.0.1", Port: 7890},
			{Host: "127.0.1.5", Port: 7890},
			{Host: "::1", Port: 7890},
			{Host: "::ffff:127.0.0.1", Port: 7890},
			{Host: "localhost", Port: 7890},
			{Host: "sub.localhost", Port: 7890},
		}
		for _, spec := range loopbacks {
			if err := ValidateSpec(spec, basePolicy); err == nil {
				t.Errorf("expected loopback %s rejected when AllowLoopback=false", spec.Host)
			}
			allowPolicy := basePolicy
			allowPolicy.AllowLoopback = true
			if err := ValidateSpec(spec, allowPolicy); err != nil {
				t.Errorf("expected loopback %s:%d allowed when AllowLoopback=true, got %v", spec.Host, spec.Port, err)
			}
		}
	})

	t.Run("default allowed targets", func(t *testing.T) {
		allowed := []struct {
			name string
			spec *OutboundSpec
		}{
			{"public ipv4", &OutboundSpec{Host: "8.8.8.8", Port: 443}},
			{"public ipv6", &OutboundSpec{Host: "2606:4700:4700::1111", Port: 443}},
			{"ddns domain preserved", &OutboundSpec{Host: "sh-home.ddns.example", Port: 28443}},
			{"ddns domain on remote 9090 ok", &OutboundSpec{Host: "sh-home.ddns.example", Port: 9090}},
			{"tailscale cgnat peer", &OutboundSpec{Host: "100.64.0.12", Port: 1080}},
			{"tailscale peer on 62789 ok", &OutboundSpec{Host: "100.100.100.100", Port: 62789}},
			{"rfc1918 10/8", &OutboundSpec{Host: "10.0.0.5", Port: 1080}},
			{"rfc1918 172.16/12", &OutboundSpec{Host: "172.16.8.1", Port: 1080}},
			{"rfc1918 192.168/16", &OutboundSpec{Host: "192.168.1.1", Port: 7890}},
			{"ipv6 ula fc00", &OutboundSpec{Host: "fc00::1", Port: 443}},
			{"ipv6 ula fd00", &OutboundSpec{Host: "fd12:3456:789a::1", Port: 443}},
			{"local host non-protected port", &OutboundSpec{Host: "203.0.113.10", Port: 19999}},
		}
		for _, tc := range allowed {
			t.Run(tc.name, func(t *testing.T) {
				if err := ValidateSpec(tc.spec, basePolicy); err != nil {
					t.Errorf("expected %s (%s:%d) allowed, got %v", tc.name, tc.spec.Host, tc.spec.Port, err)
				}
			})
		}
	})
}

func TestParseAndValidate(t *testing.T) {
	policy := AddressPolicy{
		LocalHosts:    []string{"1.2.3.4"},
		ReservedPorts: map[uint32]struct{}{62789: {}},
	}

	spec, err := ParseAndValidate("socks5://user:pass@100.64.5.10:1080#TailscaleHome", policy)
	if err != nil {
		t.Fatalf("ParseAndValidate valid tailscale: %v", err)
	}
	if spec.Host != "100.64.5.10" || spec.Port != 1080 {
		t.Errorf("unexpected spec: %+v", spec)
	}

	// DDNS domain must remain un-resolved
	ddnsSpec, err := ParseAndValidate("hy2://u:p@myhome.ddns.example:28443?mport=20000-30000#Home", policy)
	if err != nil {
		t.Fatalf("ParseAndValidate DDNS: %v", err)
	}
	if ddnsSpec.Host != "myhome.ddns.example" {
		t.Errorf("DDNS host mutated to %q", ddnsSpec.Host)
	}

	if _, err := ParseAndValidate("socks5://127.0.0.1:1080", policy); err == nil {
		t.Error("expected loopback URI rejected")
	}
	if _, err := ParseAndValidate("socks5://169.254.169.254:80", policy); err == nil {
		t.Error("expected cloud metadata URI rejected")
	}
	if _, err := ParseAndValidate("socks5://1.2.3.4:62789", policy); err == nil {
		t.Error("expected local reserved port URI rejected")
	}
}
