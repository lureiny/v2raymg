package converter

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestApplyCustomGroupMembership_TargetSpecificGroup(t *testing.T) {
	groups := []interface{}{
		map[string]interface{}{"name": "Proxy", "type": "select", "proxies": []interface{}{"DIRECT"}},
		map[string]interface{}{"name": "Streaming", "type": "select", "proxies": []interface{}{"DIRECT"}},
		map[string]interface{}{"name": "Manual", "type": "select", "proxies": []interface{}{"DIRECT"}},
	}

	updated := applyCustomGroupMembership(groups, []ProxyGroupConfig{{
		Name:        "Manual",
		Type:        "select",
		Proxies:     []string{"all"},
		InjectInto:  "Proxy",
		InjectIntos: []string{"Proxy"},
	}})

	proxy := updated[0].(map[string]interface{})
	streaming := updated[1].(map[string]interface{})

	if got := toInterfaceSlice(proxy["proxies"]); len(got) != 2 || got[1] != "Manual" {
		t.Fatalf("Proxy group proxies = %#v, want appended Manual", got)
	}
	if got := toInterfaceSlice(streaming["proxies"]); len(got) != 1 {
		t.Fatalf("Streaming group should remain unchanged, got %#v", got)
	}
}

func TestApplyCustomGroupMembership_MultiTargetGroups(t *testing.T) {
	groups := []interface{}{
		map[string]interface{}{"name": "Proxy", "type": "select", "proxies": []interface{}{"DIRECT"}},
		map[string]interface{}{"name": "Streaming", "type": "select", "proxies": []interface{}{"DIRECT"}},
		map[string]interface{}{"name": "Backup", "type": "select", "proxies": []interface{}{"DIRECT"}},
		map[string]interface{}{"name": "Manual", "type": "select", "proxies": []interface{}{"DIRECT"}},
	}

	updated := applyCustomGroupMembership(groups, []ProxyGroupConfig{{
		Name:        "Manual",
		Type:        "select",
		Proxies:     []string{"all"},
		InjectInto:  "Proxy,Streaming",
		InjectIntos: []string{"Proxy", "Streaming"},
	}})

	proxy := updated[0].(map[string]interface{})
	streaming := updated[1].(map[string]interface{})
	backup := updated[2].(map[string]interface{})

	if got := toInterfaceSlice(proxy["proxies"]); len(got) != 2 || got[1] != "Manual" {
		t.Fatalf("Proxy group proxies = %#v, want appended Manual", got)
	}
	if got := toInterfaceSlice(streaming["proxies"]); len(got) != 2 || got[1] != "Manual" {
		t.Fatalf("Streaming group proxies = %#v, want appended Manual", got)
	}
	if got := toInterfaceSlice(backup["proxies"]); len(got) != 1 {
		t.Fatalf("Backup group should remain unchanged, got %#v", got)
	}
}

func TestApplyCustomGroupMembership_CleansExistingReferencesBeforeReinject(t *testing.T) {
	groups := []interface{}{
		map[string]interface{}{"name": "Proxy", "type": "select", "proxies": []interface{}{"DIRECT", "Manual"}},
		map[string]interface{}{"name": "Streaming", "type": "select", "proxies": []interface{}{"DIRECT", "Manual"}},
		map[string]interface{}{"name": "Manual", "type": "select", "proxies": []interface{}{"node-a", "DIRECT"}},
	}

	updated := applyCustomGroupMembership(groups, []ProxyGroupConfig{{
		Name:        "Manual",
		Type:        "select",
		Proxies:     []string{"all"},
		InjectInto:  "Streaming",
		InjectIntos: []string{"Streaming"},
	}})

	proxy := updated[0].(map[string]interface{})
	streaming := updated[1].(map[string]interface{})

	if got := toInterfaceSlice(proxy["proxies"]); len(got) != 1 {
		t.Fatalf("Proxy group should have stale Manual removed, got %#v", got)
	}
	if got := toInterfaceSlice(streaming["proxies"]); len(got) != 2 || got[1] != "Manual" {
		t.Fatalf("Streaming group proxies = %#v, want re-injected Manual only here", got)
	}
}

func TestApplyCustomGroupMembership_FallbackToAllNonCustomGroups(t *testing.T) {
	groups := []interface{}{
		map[string]interface{}{"name": "Proxy", "type": "select", "proxies": []interface{}{"DIRECT"}},
		map[string]interface{}{"name": "Streaming", "type": "select", "proxies": []interface{}{"DIRECT"}},
		map[string]interface{}{"name": "Manual", "type": "select", "proxies": []interface{}{"DIRECT"}},
		map[string]interface{}{"name": "Backup", "type": "select", "proxies": []interface{}{"DIRECT"}},
	}

	updated := applyCustomGroupMembership(groups, []ProxyGroupConfig{
		{Name: "Manual", Type: "select", Proxies: []string{"all"}},
		{Name: "Backup", Type: "select", Proxies: []string{"all"}},
	})

	proxy := updated[0].(map[string]interface{})
	streaming := updated[1].(map[string]interface{})
	manual := updated[2].(map[string]interface{})
	backup := updated[3].(map[string]interface{})

	if got := toInterfaceSlice(proxy["proxies"]); len(got) != 3 {
		t.Fatalf("Proxy group proxies = %#v, want DIRECT+Manual+Backup", got)
	}
	if got := toInterfaceSlice(streaming["proxies"]); len(got) != 3 {
		t.Fatalf("Streaming group proxies = %#v, want DIRECT+Manual+Backup", got)
	}
	if got := toInterfaceSlice(manual["proxies"]); len(got) != 1 {
		t.Fatalf("Manual group should not receive Backup to avoid cycles, got %#v", got)
	}
	if got := toInterfaceSlice(backup["proxies"]); len(got) != 1 {
		t.Fatalf("Backup group should not receive Manual to avoid cycles, got %#v", got)
	}
}

func TestPatchTemplateWithOptions_InjectsCustomGroupIntoTargetGroup(t *testing.T) {
	conv := &ClashConverter{}
	nodeMap := NodeMap{}
	if err := yaml.Unmarshal([]byte(`proxies: []
proxy-groups:
  - name: Proxy
    type: select
    proxies: [DIRECT]
  - name: Streaming
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,DIRECT
`), &nodeMap); err != nil {
		t.Fatalf("unmarshal template: %v", err)
	}

	result, err := conv.patchTemplateWithOptions(nodeMap, []string{"node-a"}, &ConvertOptions{
		ProxyGroups: []ProxyGroupConfig{{Name: "Manual", Type: "select", Proxies: []string{"all"}, InjectInto: "Proxy"}},
	})
	if err != nil {
		t.Fatalf("patchTemplateWithOptions error: %v", err)
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("yaml unmarshal result: %v", err)
	}
	groups := toInterfaceSlice(parsed[clashProxyGroupsKey])
	proxy := groups[0].(map[string]interface{})
	streaming := groups[1].(map[string]interface{})
	manual := groups[2].(map[string]interface{})

	if got := toInterfaceSlice(proxy["proxies"]); len(got) != 2 || got[1] != "Manual" {
		t.Fatalf("Proxy group proxies = %#v, want appended Manual", got)
	}
	if got := toInterfaceSlice(streaming["proxies"]); len(got) != 1 {
		t.Fatalf("Streaming group should remain unchanged, got %#v", got)
	}
	if name := manual["name"]; name != "Manual" {
		t.Fatalf("missing Manual group, got %v", name)
	}
}
