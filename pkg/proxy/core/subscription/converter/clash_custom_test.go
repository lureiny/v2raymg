package converter_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/converter"
	"gopkg.in/yaml.v3"
)

// --- ParseProxyGroupParam tests ---

func TestParseProxyGroupParam(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantType string
		wantLen  int
		wantErr  bool
	}{
		{"Proxy:select:all", "Proxy", "select", 1, false},
		{"Auto:url-test:all", "Auto", "url-test", 1, false},
		{"Manual:select:all:Proxy", "Manual", "select", 1, false},
		{"HK:select:HK|TW|SG", "HK", "select", 1, false},
		{"Fallback:fallback:US,JP", "Fallback", "fallback", 2, false},
		{"LoadBalance:load-balance:all", "LoadBalance", "load-balance", 1, false},
		{"SimpleGroup:select:", "SimpleGroup", "select", 1, false}, // empty split gives [""]
		{"NoProxies:select", "NoProxies", "select", 0, false},
		{"invalid", "", "", 0, true},
		{"", "", "", 0, true},
	}

	for _, tt := range tests {
		config, err := converter.ParseProxyGroupParam(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseProxyGroupParam(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if tt.wantErr {
			continue
		}
		if config.Name != tt.wantName {
			t.Errorf("ParseProxyGroupParam(%q) name = %q, want %q", tt.input, config.Name, tt.wantName)
		}
		if config.Type != tt.wantType {
			t.Errorf("ParseProxyGroupParam(%q) type = %q, want %q", tt.input, config.Type, tt.wantType)
		}
		if len(config.Proxies) != tt.wantLen {
			t.Errorf("ParseProxyGroupParam(%q) proxies len = %d, want %d", tt.input, len(config.Proxies), tt.wantLen)
		}
	}
}

func TestParseProxyGroupParam_URLTestDefaults(t *testing.T) {
	config, err := converter.ParseProxyGroupParam("Auto:url-test:all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.URL != "https://cp.cloudflare.com/generate_204" {
		t.Errorf("url-test should have default URL, got %q", config.URL)
	}
	if config.Interval != 300 {
		t.Errorf("url-test should have default interval 300, got %d", config.Interval)
	}
}

func TestParseProxyGroupParam_FallbackDefaults(t *testing.T) {
	config, err := converter.ParseProxyGroupParam("FB:fallback:all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.URL != "https://cp.cloudflare.com/generate_204" {
		t.Errorf("fallback should have default URL, got %q", config.URL)
	}
	if config.Interval != 300 {
		t.Errorf("fallback should have default interval 300, got %d", config.Interval)
	}
}

func TestParseProxyGroupParam_LoadBalanceDefaults(t *testing.T) {
	config, err := converter.ParseProxyGroupParam("LB:load-balance:all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.URL != "https://cp.cloudflare.com/generate_204" {
		t.Errorf("load-balance should have default URL, got %q", config.URL)
	}
	if config.Interval != 300 {
		t.Errorf("load-balance should have default interval 300, got %d", config.Interval)
	}
}

func TestParseProxyGroupParam_SelectNoDefaults(t *testing.T) {
	config, err := converter.ParseProxyGroupParam("Proxy:select:all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.URL != "" {
		t.Errorf("select should not have URL, got %q", config.URL)
	}
	if config.Interval != 0 {
		t.Errorf("select should not have interval, got %d", config.Interval)
	}
}

func TestParseProxyGroupParam_InjectInto(t *testing.T) {
	config, err := converter.ParseProxyGroupParam("Manual:select:all:Proxy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.InjectInto != "Proxy" {
		t.Fatalf("injectInto = %q, want %q", config.InjectInto, "Proxy")
	}
	if len(config.InjectIntos) != 1 || config.InjectIntos[0] != "Proxy" {
		t.Fatalf("injectIntos = %#v, want [Proxy]", config.InjectIntos)
	}
}

func TestParseProxyGroupParam_MultiInjectInto(t *testing.T) {
	config, err := converter.ParseProxyGroupParam("Manual:select:all:Proxy,Streaming")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(config.InjectIntos) != 2 || config.InjectIntos[0] != "Proxy" || config.InjectIntos[1] != "Streaming" {
		t.Fatalf("injectIntos = %#v, want [Proxy Streaming]", config.InjectIntos)
	}
}

// --- ParseRuleProviderParam tests ---

func TestParseRuleProviderParam(t *testing.T) {
	tests := []struct {
		input        string
		wantName     string
		wantType     string
		wantBehavior string
		wantErr      bool
	}{
		{"my-reject:http:domain:https://example.com/reject.yaml", "my-reject", "http", "domain", false},
		{"geoip-file:file:ipcidr:/path/to/geoip.yaml", "geoip-file", "file", "ipcidr", false},
		{"classical-rules:http:classical:https://rules.example.com/rules.yaml", "classical-rules", "http", "classical", false},
		{"invalid", "", "", "", true},
		{"short:two", "", "", "", true},
		{"", "", "", "", true},
	}

	for _, tt := range tests {
		config, err := converter.ParseRuleProviderParam(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseRuleProviderParam(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if tt.wantErr {
			continue
		}
		if config.Name != tt.wantName {
			t.Errorf("ParseRuleProviderParam(%q) name = %q, want %q", tt.input, config.Name, tt.wantName)
		}
		if config.Type != tt.wantType {
			t.Errorf("ParseRuleProviderParam(%q) type = %q, want %q", tt.input, config.Type, tt.wantType)
		}
		if config.Behavior != tt.wantBehavior {
			t.Errorf("ParseRuleProviderParam(%q) behavior = %q, want %q", tt.input, config.Behavior, tt.wantBehavior)
		}
	}
}

func TestParseRuleProviderParam_Defaults(t *testing.T) {
	config, err := converter.ParseRuleProviderParam("test:http:domain:https://example.com/rules.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.URL != "https://example.com/rules.yaml" {
		t.Errorf("url should preserve full value, got %q", config.URL)
	}
	if config.Interval != 86400 {
		t.Errorf("default interval should be 86400, got %d", config.Interval)
	}
	if config.Path != "./rule-provider/test.yaml" {
		t.Errorf("default path should be ./rule-provider/test.yaml, got %q", config.Path)
	}
}

// --- ParseRuleParam tests ---

func TestParseRuleParam(t *testing.T) {
	tests := []struct {
		input      string
		wantType   string
		wantValue  string
		wantPolicy string
		wantErr    bool
	}{
		{"DOMAIN-SUFFIX,google.com,Proxy", "DOMAIN-SUFFIX", "google.com", "Proxy", false},
		{"RULE-SET,my-reject,REJECT", "RULE-SET", "my-reject", "REJECT", false},
		{"IP-CIDR,192.168.0.0/16,DIRECT", "IP-CIDR", "192.168.0.0/16", "DIRECT", false},
		{"MATCH,,DIRECT", "MATCH", "", "DIRECT", false},
		{"invalid", "", "", "", true},
		{"only,Two", "", "", "", true},
		{"", "", "", "", true},
	}

	for _, tt := range tests {
		config, err := converter.ParseRuleParam(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseRuleParam(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if tt.wantErr {
			continue
		}
		if config.Type != tt.wantType {
			t.Errorf("ParseRuleParam(%q) type = %q, want %q", tt.input, config.Type, tt.wantType)
		}
		if config.Value != tt.wantValue {
			t.Errorf("ParseRuleParam(%q) value = %q, want %q", tt.input, config.Value, tt.wantValue)
		}
		if config.Policy != tt.wantPolicy {
			t.Errorf("ParseRuleParam(%q) policy = %q, want %q", tt.input, config.Policy, tt.wantPolicy)
		}
	}
}

// --- MatchProxies tests ---

func TestMatchProxies(t *testing.T) {
	nodeNames := []string{"🌿 VMESS_HK_01", "🌿 VMESS_TW_01", "🌿 TROJAN_US_01"}
	definedGroups := map[string]bool{"Auto": true, "Proxy": true}

	tests := []struct {
		name     string
		proxies  []string
		wantLen  int
		wantDesc string
	}{
		{"all", []string{"all"}, 3, "全部节点"},
		{"builtin DIRECT", []string{"DIRECT"}, 1, "内置策略"},
		{"builtin REJECT", []string{"REJECT"}, 1, "内置策略"},
		{"defined group", []string{"Auto"}, 1, "已定义 group"},
		{"regex HK|TW", []string{"HK|TW"}, 2, "正则匹配"},
		{"regex US", []string{"US"}, 1, "正则匹配"},
		{"mixed all+DIRECT", []string{"all", "DIRECT"}, 4, "混合"},
		{"mixed group+regex", []string{"Proxy", "HK"}, 2, "混合"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.MatchProxies(tt.proxies, nodeNames, definedGroups)
			if len(result) != tt.wantLen {
				t.Errorf("MatchProxies(%v) got %d results, want %d (%s)", tt.proxies, len(result), tt.wantLen, tt.wantDesc)
			}
		})
	}
}

func TestMatchProxies_AllNodes(t *testing.T) {
	nodeNames := []string{"node1", "node2", "node3"}
	result := converter.MatchProxies([]string{"all"}, nodeNames, nil)
	if len(result) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(result))
	}
	for i, name := range nodeNames {
		if result[i] != name {
			t.Errorf("result[%d] = %q, want %q", i, result[i], name)
		}
	}
}

func TestMatchProxies_BuiltinPolicies(t *testing.T) {
	builtins := []string{"DIRECT", "REJECT", "PASS", "COMPATIBLE"}
	for _, p := range builtins {
		result := converter.MatchProxies([]string{p}, []string{"node1"}, nil)
		if len(result) != 1 || result[0] != p {
			t.Errorf("builtin policy %q not matched correctly, got %v", p, result)
		}
	}
}

func TestMatchProxies_RegexMatch(t *testing.T) {
	nodeNames := []string{"🌿 VMESS_HK_01", "🌿 VMESS_TW_01", "🌿 TROJAN_US_01", "🌿 SS_JP_01"}
	result := converter.MatchProxies([]string{"VMESS"}, nodeNames, nil)
	if len(result) != 2 {
		t.Errorf("expected 2 VMESS nodes, got %d: %v", len(result), result)
	}
}

func TestMatchProxies_EmptyProxies(t *testing.T) {
	result := converter.MatchProxies([]string{}, []string{"node1"}, nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestMatchProxies_EmptyNodes(t *testing.T) {
	result := converter.MatchProxies([]string{"all"}, []string{}, nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for no nodes, got %v", result)
	}
}

// --- ValidatePolicy tests ---

func TestValidatePolicy(t *testing.T) {
	definedGroups := map[string]bool{"Proxy": true, "Auto": true}

	tests := []struct {
		name    string
		policy  string
		wantErr bool
	}{
		{"builtin DIRECT", "DIRECT", false},
		{"builtin REJECT", "REJECT", false},
		{"builtin PASS", "PASS", false},
		{"builtin COMPATIBLE", "COMPATIBLE", false},
		{"defined group", "Proxy", false},
		{"defined group Auto", "Auto", false},
		{"undefined group", "Unknown", true},
		{"empty policy", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := converter.ValidatePolicy(tt.policy, definedGroups)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePolicy(%q) error = %v, wantErr %v", tt.policy, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePolicy_NilGroups(t *testing.T) {
	// builtin policies should still work
	err := converter.ValidatePolicy("DIRECT", nil)
	if err != nil {
		t.Errorf("DIRECT should be valid even with nil groups: %v", err)
	}
	// non-builtin should fail
	err = converter.ValidatePolicy("SomeGroup", nil)
	if err == nil {
		t.Error("undefined group should fail with nil groups map")
	}
}

// --- FetchProxyGroupsFromURL tests ---

func TestFetchProxyGroupsFromURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Proxy:select:all\nAuto:url-test:all\n# comment line\n\nHK:select:HK|TW"))
	}))
	defer ts.Close()

	configs, err := converter.FetchProxyGroupsFromURL(ts.URL)
	if err != nil {
		t.Fatalf("FetchProxyGroupsFromURL error: %v", err)
	}
	if len(configs) != 3 {
		t.Errorf("expected 3 configs, got %d", len(configs))
	}
	// Check first config
	if configs[0].Name != "Proxy" || configs[0].Type != "select" {
		t.Errorf("first config: name=%q type=%q, want Proxy/select", configs[0].Name, configs[0].Type)
	}
	// Check third config (should skip empty lines and comments)
	if configs[2].Name != "HK" {
		t.Errorf("third config name = %q, want HK", configs[2].Name)
	}
}

func TestFetchProxyGroupsFromURL_EmptyResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(""))
	}))
	defer ts.Close()

	configs, err := converter.FetchProxyGroupsFromURL(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 configs for empty response, got %d", len(configs))
	}
}

func TestFetchProxyGroupsFromURL_OnlyComments(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("# comment 1\n# comment 2\n"))
	}))
	defer ts.Close()

	configs, err := converter.FetchProxyGroupsFromURL(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 configs for comment-only response, got %d", len(configs))
	}
}

func TestFetchProxyGroupsFromURL_InvalidLines(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("invalid line\nProxy:select:all\nalso invalid"))
	}))
	defer ts.Close()

	configs, err := converter.FetchProxyGroupsFromURL(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should only parse the valid line
	if len(configs) != 1 {
		t.Errorf("expected 1 config (invalid lines skipped), got %d", len(configs))
	}
	if configs[0].Name != "Proxy" {
		t.Errorf("expected Proxy config, got %q", configs[0].Name)
	}
}

// --- FetchRuleProvidersFromURL tests ---

func TestFetchRuleProvidersFromURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("reject:http:domain:https://example.com/reject.yaml\ngeoip:file:ipcidr:/path/to/geoip.yaml"))
	}))
	defer ts.Close()

	configs, err := converter.FetchRuleProvidersFromURL(ts.URL)
	if err != nil {
		t.Fatalf("FetchRuleProvidersFromURL error: %v", err)
	}
	if len(configs) != 2 {
		t.Errorf("expected 2 configs, got %d", len(configs))
	}
	if configs[0].Name != "reject" || configs[0].Type != "http" {
		t.Errorf("first config unexpected: %+v", configs[0])
	}
}

// --- FetchRulesFromURL tests ---

func TestFetchRulesFromURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("DOMAIN-SUFFIX,google.com,Proxy\nRULE-SET,my-reject,REJECT"))
	}))
	defer ts.Close()

	configs, err := converter.FetchRulesFromURL(ts.URL)
	if err != nil {
		t.Fatalf("FetchRulesFromURL error: %v", err)
	}
	if len(configs) != 2 {
		t.Errorf("expected 2 configs, got %d", len(configs))
	}
	if configs[0].Type != "DOMAIN-SUFFIX" || configs[0].Value != "google.com" {
		t.Errorf("first config unexpected: %+v", configs[0])
	}
}

// --- ConvertWithOptions integration tests ---

func TestConvertWithOptions_Basic(t *testing.T) {
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVMess,
			Host:       "hk.example.com",
			Port:       443,
			Password:   "uuid-test-hk",
			NodeName:   "HK_01",
			InboundTag: "vmess-hk",
			Extensions: map[string]any{"security": "tls"},
		},
		{
			Protocol:   contracts.ProtocolTrojan,
			Host:       "us.example.com",
			Port:       443,
			Password:   "pwd-test-us",
			NodeName:   "US_01",
			InboundTag: "trojan-us",
		},
	}

	opts := &converter.ConvertOptions{
		ProxyGroups: []converter.ProxyGroupConfig{
			{Name: "Proxy", Type: "select", Proxies: []string{"Auto", "DIRECT"}},
			{Name: "Auto", Type: "url-test", Proxies: []string{"all"}},
		},
		Rules: []converter.RuleConfig{
			{Type: "MATCH", Value: "", Policy: "Proxy"},
		},
	}

	c := &converter.ClashConverter{}
	result, err := c.ConvertWithOptions(specs, opts)
	if err != nil {
		t.Fatalf("ConvertWithOptions error: %v", err)
	}

	// Verify YAML output contains expected sections
	if !strings.Contains(result, "proxies:") {
		t.Error("missing proxies section")
	}
	if !strings.Contains(result, "proxy-groups:") {
		t.Error("missing proxy-groups section")
	}
	if !strings.Contains(result, "rules:") {
		t.Error("missing rules section")
	}

	// Verify node names are present
	if !strings.Contains(result, "VMESS_HK_01") {
		t.Error("missing VMESS_HK_01 node")
	}
	if !strings.Contains(result, "TROJAN_US_01") {
		t.Error("missing TROJAN_US_01 node")
	}

	// Verify proxy-groups
	if !strings.Contains(result, "name: Proxy") {
		t.Error("missing Proxy group")
	}
	if !strings.Contains(result, "name: Auto") {
		t.Error("missing Auto group")
	}

	// Verify rules
	if !strings.Contains(result, "MATCH,,Proxy") {
		t.Error("missing MATCH rule")
	}
}

func TestConvertWithOptions_DefaultProxyGroups(t *testing.T) {
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVMess,
			Host:       "test.example.com",
			Port:       443,
			Password:   "uuid-test",
			NodeName:   "TestNode",
			InboundTag: "test",
		},
	}

	// No ProxyGroups specified, should generate defaults
	opts := &converter.ConvertOptions{
		Rules: []converter.RuleConfig{
			{Type: "MATCH", Value: "", Policy: "Proxy"},
		},
	}

	c := &converter.ClashConverter{}
	result, err := c.ConvertWithOptions(specs, opts)
	if err != nil {
		t.Fatalf("ConvertWithOptions error: %v", err)
	}

	// Should have default Proxy / Manual / Auto groups
	if !strings.Contains(result, "name: Proxy") {
		t.Error("missing default Proxy group")
	}
	if !strings.Contains(result, "name: Manual") {
		t.Error("missing default Manual group")
	}
	if !strings.Contains(result, "name: Auto") {
		t.Error("missing default Auto group")
	}
	if !strings.Contains(result, "- DIRECT") {
		t.Error("default Manual group should include DIRECT")
	}
}

func TestConvertWithOptions_RuleProviders(t *testing.T) {
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVMess,
			Host:       "test.example.com",
			Port:       443,
			Password:   "uuid-test",
			NodeName:   "TestNode",
			InboundTag: "test",
		},
	}

	opts := &converter.ConvertOptions{
		ProxyGroups: []converter.ProxyGroupConfig{
			{Name: "Proxy", Type: "select", Proxies: []string{"all"}},
		},
		RuleProviders: []converter.RuleProviderConfig{
			{
				Name:     "my-reject",
				Type:     "http",
				Behavior: "domain",
				URL:      "https://example.com/reject.yaml",
			},
		},
		Rules: []converter.RuleConfig{
			{Type: "RULE-SET", Value: "my-reject", Policy: "REJECT"},
		},
	}

	c := &converter.ClashConverter{}
	result, err := c.ConvertWithOptions(specs, opts)
	if err != nil {
		t.Fatalf("ConvertWithOptions error: %v", err)
	}

	if !strings.Contains(result, "rule-providers:") {
		t.Error("missing rule-providers section")
	}
	if !strings.Contains(result, "my-reject:") {
		t.Error("missing my-reject rule-provider")
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("yaml unmarshal failed: %v", err)
	}

	rawRuleProviders, ok := parsed["rule-providers"]
	if !ok {
		t.Fatal("rule-providers section not found after yaml unmarshal")
	}

	ruleProvidersMap, ok := rawRuleProviders.(map[string]interface{})
	if !ok {
		t.Fatalf("rule-providers should be a map, got %T", rawRuleProviders)
	}

	rawProvider, ok := ruleProvidersMap["my-reject"]
	if !ok {
		t.Fatal("my-reject provider not found in rule-providers")
	}

	providerMap, ok := rawProvider.(map[string]interface{})
	if !ok {
		t.Fatalf("my-reject provider should be a map, got %T", rawProvider)
	}

	if providerMap["url"] != "https://example.com/reject.yaml" {
		t.Fatalf("unexpected rule-provider url: %v", providerMap["url"])
	}
	if providerMap["behavior"] != "domain" {
		t.Fatalf("unexpected rule-provider behavior: %v", providerMap["behavior"])
	}
	if providerMap["type"] != "http" {
		t.Fatalf("unexpected rule-provider type: %v", providerMap["type"])
	}
}

func TestConvertWithOptions_InvalidRulePolicy(t *testing.T) {
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVMess,
			Host:       "test.example.com",
			Port:       443,
			Password:   "uuid-test",
			NodeName:   "TestNode",
			InboundTag: "test",
		},
	}

	opts := &converter.ConvertOptions{
		ProxyGroups: []converter.ProxyGroupConfig{
			{Name: "Proxy", Type: "select", Proxies: []string{"all"}},
		},
		Rules: []converter.RuleConfig{
			{Type: "MATCH", Value: "", Policy: "UndefinedGroup"},
		},
	}

	c := &converter.ClashConverter{}
	_, err := c.ConvertWithOptions(specs, opts)
	if err == nil {
		t.Error("expected error for invalid policy")
	}
	if !strings.Contains(err.Error(), "invalid policy") {
		t.Errorf("expected invalid policy error, got: %v", err)
	}
}

func TestConvertWithOptions_VLessSkipped(t *testing.T) {
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVLess,
			Host:       "test.example.com",
			Port:       443,
			Password:   "uuid-test",
			NodeName:   "VLessNode",
			InboundTag: "vless-test",
		},
	}

	opts := &converter.ConvertOptions{
		ProxyGroups: []converter.ProxyGroupConfig{
			{Name: "Proxy", Type: "select", Proxies: []string{"all"}},
		},
	}

	c := &converter.ClashConverter{}
	result, err := c.ConvertWithOptions(specs, opts)
	if err != nil {
		t.Fatalf("ConvertWithOptions error: %v", err)
	}

	// VLess should be skipped - proxies list empty
	if strings.Contains(result, "VLessNode") {
		t.Error("VLess node should be skipped")
	}
}

func TestConvertWithOptions_NilOptions(t *testing.T) {
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVMess,
			Host:       "test.example.com",
			Port:       443,
			Password:   "uuid-test",
			NodeName:   "TestNode",
			InboundTag: "test",
		},
	}

	c := &converter.ClashConverter{}
	// nil opts should trigger external template fallback or basic output
	result, err := c.ConvertWithOptions(specs, nil)
	if err != nil {
		t.Fatalf("ConvertWithOptions with nil opts error: %v", err)
	}
	// Should at least contain proxies
	if !strings.Contains(result, "proxies:") {
		t.Error("expected proxies section in output")
	}
}

func TestConvertWithOptions_EmptySpecs(t *testing.T) {
	opts := &converter.ConvertOptions{
		ProxyGroups: []converter.ProxyGroupConfig{
			{Name: "Proxy", Type: "select", Proxies: []string{"DIRECT"}},
		},
	}

	c := &converter.ClashConverter{}
	result, err := c.ConvertWithOptions([]contracts.SubscriptionSpec{}, opts)
	if err != nil {
		t.Fatalf("ConvertWithOptions error: %v", err)
	}

	// Should still have proxy-groups
	if !strings.Contains(result, "proxy-groups:") {
		t.Error("expected proxy-groups section")
	}
}
