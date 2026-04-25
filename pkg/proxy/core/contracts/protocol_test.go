package contracts

import "testing"

// TestProtocolAllProtocolsMatchesIsValid protects against drift between the
// AllProtocols() list and the IsValid() switch. Historically these two were
// out of sync (AllProtocols returned only 4 entries while IsValid accepted
// 8), which silently broke anything iterating AllProtocols for validation.
// Any future protocol addition must update both sites or this test fails.
func TestProtocolAllProtocolsMatchesIsValid(t *testing.T) {
	all := AllProtocols()
	if len(all) == 0 {
		t.Fatal("AllProtocols returned empty slice")
	}

	seen := make(map[Protocol]int, len(all))
	for _, p := range all {
		seen[p]++
		if !p.IsValid() {
			t.Errorf("AllProtocols includes %q but IsValid returns false", p)
		}
	}
	for p, count := range seen {
		if count > 1 {
			t.Errorf("AllProtocols contains duplicate %q (%d times)", p, count)
		}
	}

	// Every named constant must appear in AllProtocols. Listed explicitly so
	// renaming a constant without updating the list is caught here.
	expected := []Protocol{
		ProtocolVMess, ProtocolVLess, ProtocolTrojan, ProtocolShadowsocks,
		ProtocolSOCKS5, ProtocolHTTP, ProtocolSnell, ProtocolHysteria2,
		ProtocolTUIC, ProtocolAnyTLS,
	}
	for _, p := range expected {
		if _, ok := seen[p]; !ok {
			t.Errorf("AllProtocols is missing %q", p)
		}
	}
	if len(all) != len(expected) {
		t.Errorf("AllProtocols has %d entries, expected %d (same as named constants)", len(all), len(expected))
	}
}

func TestProtocolIsValid(t *testing.T) {
	valid := []Protocol{
		ProtocolVMess, ProtocolVLess, ProtocolTrojan, ProtocolShadowsocks,
		ProtocolSOCKS5, ProtocolHTTP, ProtocolSnell, ProtocolHysteria2,
		ProtocolTUIC, ProtocolAnyTLS,
	}
	for _, p := range valid {
		if !p.IsValid() {
			t.Errorf("Protocol(%q).IsValid() = false, want true", p)
		}
	}
	invalid := []Protocol{"", "unknown", "VMESS", "vless ", "hysteria"}
	for _, p := range invalid {
		if p.IsValid() {
			t.Errorf("Protocol(%q).IsValid() = true, want false", p)
		}
	}
}

func TestProtocolStringValues(t *testing.T) {
	cases := map[Protocol]string{
		ProtocolVMess:       "vmess",
		ProtocolVLess:       "vless",
		ProtocolTrojan:      "trojan",
		ProtocolShadowsocks: "shadowsocks",
		ProtocolSOCKS5:      "socks5",
		ProtocolHTTP:        "http",
		ProtocolSnell:       "snell",
		ProtocolHysteria2:   "hysteria2",
		ProtocolTUIC:        "tuic",
		ProtocolAnyTLS:      "anytls",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("Protocol(%q).String() = %q, want %q", p, got, want)
		}
	}
}

func TestTransportAllMatchesIsValid(t *testing.T) {
	all := AllTransports()
	seen := make(map[Transport]int, len(all))
	for _, tr := range all {
		seen[tr]++
		if !tr.IsValid() {
			t.Errorf("AllTransports includes %q but IsValid returns false", tr)
		}
	}
	for tr, count := range seen {
		if count > 1 {
			t.Errorf("AllTransports contains duplicate %q (%d times)", tr, count)
		}
	}
}

func TestSecurityAllMatchesIsValid(t *testing.T) {
	for _, s := range AllSecurityModes() {
		if !s.IsValid() {
			t.Errorf("AllSecurityModes includes %q but IsValid returns false", s)
		}
	}
}

func TestValidateCombinationRejectsUnknown(t *testing.T) {
	if err := ValidateCombination("frobnicate", TransportTCP, SecurityNone); err == nil {
		t.Error("ValidateCombination accepted unknown protocol")
	}
	if err := ValidateCombination(ProtocolVMess, "nonexistent", SecurityNone); err == nil {
		t.Error("ValidateCombination accepted unknown transport")
	}
	if err := ValidateCombination(ProtocolVMess, TransportTCP, "wat"); err == nil {
		t.Error("ValidateCombination accepted unknown security")
	}
	if err := ValidateCombination(ProtocolTUIC, TransportTCP, SecurityTLS); err != nil {
		t.Errorf("ValidateCombination rejected valid triple (tuic,tcp,tls): %v", err)
	}
	if err := ValidateCombination(ProtocolAnyTLS, TransportTCP, SecurityTLS); err != nil {
		t.Errorf("ValidateCombination rejected valid triple (anytls,tcp,tls): %v", err)
	}
}
