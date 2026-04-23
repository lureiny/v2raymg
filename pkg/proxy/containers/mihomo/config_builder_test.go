package mihomo

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"gopkg.in/yaml.v3"
)

func baseConfigForTest() mihomoInitialConfig {
	return mihomoInitialConfig{
		MixedPort:          0,
		AllowLan:           false,
		ExternalController: "127.0.0.1:9090",
		Secret:             "",
		LogLevel:           "info",
		Mode:               "global",
		Proxies:            []any{},
		ProxyGroups:        []any{},
		Rules:              []string{"MATCH,DIRECT"},
		Listeners:          nil, // BuildConfig sets this
	}
}

func TestBuildConfig_EmptyInbounds(t *testing.T) {
	data, err := BuildConfig(baseConfigForTest(), nil)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	listeners, ok := cfg["listeners"].([]any)
	if !ok {
		t.Fatalf("listeners not a list: %T", cfg["listeners"])
	}
	if len(listeners) != 0 {
		t.Errorf("listeners should be empty, got %d entries", len(listeners))
	}
	// The inert outbound section must survive — if it gets dropped mihomo
	// will synthesise defaults that might reach for the network.
	if cfg["mode"] != "global" {
		t.Errorf("mode preserved: %v", cfg["mode"])
	}
	if rules, ok := cfg["rules"].([]any); !ok || len(rules) != 1 || rules[0] != "MATCH,DIRECT" {
		t.Errorf("rules not preserved: %v", cfg["rules"])
	}
}

func TestBuildConfig_MultipleInbounds_DeterministicOrder(t *testing.T) {
	// Pass inbounds out of alphabetical order; expect sorted output so
	// on-disk diffs stay clean across FastAddInbound call order.
	in := []*MihomoInbound{
		NewMihomoInbound("zebra", contracts.ProtocolTrojan, 10003, MihomoSharedCred{Password: "pw-z"}),
		NewMihomoInbound("apple", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "uuid-a"}),
		NewMihomoInbound("mango", contracts.ProtocolShadowsocks, 10002, MihomoSharedCred{Password: "pw-m", Cipher: "aes-128-gcm"}),
	}
	data, err := BuildConfig(baseConfigForTest(), in)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	listeners, _ := cfg["listeners"].([]any)
	if len(listeners) != 3 {
		t.Fatalf("expected 3 listeners, got %d", len(listeners))
	}

	gotNames := make([]string, len(listeners))
	for i, l := range listeners {
		m := l.(map[string]any)
		gotNames[i] = m["name"].(string)
	}
	wantNames := []string{"apple", "mango", "zebra"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("listener order = %v, want %v", gotNames, wantNames)
	}
}

func TestBuildConfig_ListenerFieldsMatchProfilegen(t *testing.T) {
	// Byte-compare protection is too fragile (yaml ordering within a map
	// isn't guaranteed); instead round-trip and spot-check the critical
	// wire-level keys per protocol.
	in := []*MihomoInbound{
		NewMihomoInbound("vmess-a", contracts.ProtocolVMess, 10001, MihomoSharedCred{UUID: "uuid-a"}),
		NewMihomoInbound("trojan-b", contracts.ProtocolTrojan, 10002, MihomoSharedCred{Password: "pw-b"}),
		NewMihomoInbound("ss-c", contracts.ProtocolShadowsocks, 10003, MihomoSharedCred{Password: "pw-c", Cipher: "aes-128-gcm"}),
	}
	data, err := BuildConfig(baseConfigForTest(), in)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	var cfg map[string]any
	_ = yaml.Unmarshal(data, &cfg)
	listeners := cfg["listeners"].([]any)

	byName := map[string]map[string]any{}
	for _, l := range listeners {
		m := l.(map[string]any)
		byName[m["name"].(string)] = m
	}

	if got := byName["vmess-a"]; got["type"] != "vmess" || got["listen"] != "127.0.0.1" || got["port"] != "10001" {
		t.Errorf("vmess listener wire fields wrong: %#v", got)
	}
	if got := byName["trojan-b"]; got["type"] != "trojan" {
		t.Errorf("trojan listener type wrong: %#v", got)
	}
	if got := byName["ss-c"]; got["type"] != "shadowsocks" || got["password"] != "pw-c" || got["cipher"] != "aes-128-gcm" {
		t.Errorf("ss listener wire fields wrong: %#v", got)
	}
}

func TestBuildConfig_InvalidInboundPropagates(t *testing.T) {
	// An unvalidated inbound should trip Build and surface a wrapped error
	// with the offending tag so operators can locate the bad record.
	bad := NewMihomoInbound("bad", contracts.ProtocolVMess, 10001, MihomoSharedCred{}) // missing uuid
	_, err := BuildConfig(baseConfigForTest(), []*MihomoInbound{bad})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("error should include offending tag: %v", err)
	}
}
