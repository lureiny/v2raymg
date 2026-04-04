package converter

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEnsureTemplateGroups_AddsManualWhenMissingAndLinksToProxy(t *testing.T) {
	nodeMap := NodeMap{}
	err := yaml.Unmarshal([]byte(`proxies: []
proxy-groups:
  - name: Proxy
    type: select
    proxies: [Auto, DIRECT]
  - name: Auto
    type: url-test
    proxies: []
`), &nodeMap)
	if err != nil {
		t.Fatalf("unmarshal template: %v", err)
	}

	proxies := []*ClashProxy{{Name: "node-a"}, {Name: "node-b"}}
	ensureTemplateGroups(nodeMap, proxies)

	data, err := yaml.Marshal(nodeMap)
	if err != nil {
		t.Fatalf("marshal nodeMap: %v", err)
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	groups := toInterfaceSlice(parsed[clashProxyGroupsKey])
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups after adding Manual, got %d", len(groups))
	}

	proxy := groups[0].(map[string]interface{})
	auto := groups[1].(map[string]interface{})
	manual := groups[2].(map[string]interface{})

	if manual["name"] != "Manual" {
		t.Fatalf("expected third group to be Manual, got %v", manual["name"])
	}
	if got := toInterfaceSlice(manual["proxies"]); len(got) != 3 || got[2] != "DIRECT" {
		t.Fatalf("Manual proxies = %#v, want node-a,node-b,DIRECT", got)
	}
	if got := toInterfaceSlice(proxy["proxies"]); len(got) != 3 || got[2] != "Manual" {
		t.Fatalf("Proxy proxies = %#v, want appended Manual", got)
	}
	if got := toInterfaceSlice(auto["proxies"]); len(got) != 3 || got[2] != "Manual" {
		t.Fatalf("Auto proxies = %#v, want filled node list plus Manual", got)
	}
}

func TestEnsureTemplateGroups_FillsExistingManualGroup(t *testing.T) {
	nodeMap := NodeMap{}
	err := yaml.Unmarshal([]byte(`proxies: []
proxy-groups:
  - name: Proxy
    type: select
    proxies: [DIRECT]
  - name: Manual
    type: select
    proxies: []
`), &nodeMap)
	if err != nil {
		t.Fatalf("unmarshal template: %v", err)
	}

	proxies := []*ClashProxy{{Name: "node-a"}}
	ensureTemplateGroups(nodeMap, proxies)

	data, err := yaml.Marshal(nodeMap)
	if err != nil {
		t.Fatalf("marshal nodeMap: %v", err)
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	groups := toInterfaceSlice(parsed[clashProxyGroupsKey])
	manual := groups[1].(map[string]interface{})
	if got := toInterfaceSlice(manual["proxies"]); len(got) != 2 || got[1] != "DIRECT" {
		t.Fatalf("Manual proxies = %#v, want node-a,DIRECT", got)
	}
}
