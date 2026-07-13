package converter

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// TestConvertWithOptions_FallbackWhenExternalFails verifies the availability
// fix: when every external sub-converter source is unreachable, ConvertWithOptions
// degrades to the built-in minimal template instead of hard-failing the whole
// subscription. Before the fix this path returned an error (see the network-
// dependent TestConvertWithOptions_VLessIncluded, which fails when the external
// services are down). The seam fetchClashTemplateFn forces the failure
// deterministically without touching the network.
func TestConvertWithOptions_FallbackWhenExternalFails(t *testing.T) {
	orig := fetchClashTemplateFn
	fetchClashTemplateFn = func() (NodeMap, error) {
		return nil, fmt.Errorf("forced: all external sources down")
	}
	defer func() { fetchClashTemplateFn = orig }()

	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVLess,
			Host:       "h.example.com",
			Port:       443,
			Password:   "uuid-1",
			NodeName:   "FallbackNode",
			InboundTag: "vless-x",
			Extensions: map[string]any{"security": "tls", "transport": "ws", "ws_path": "/p"},
		},
	}

	c := &ClashConverter{}
	out, err := c.ConvertWithOptions(specs, nil)
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}

	// Output must be a valid Clash config containing the converted node, the
	// Manual selector, and the catch-all rule.
	var m map[string]any
	if err := yaml.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("fallback output is not valid YAML: %v\n%s", err, out)
	}
	if !strings.Contains(out, "FallbackNode") {
		t.Error("fallback config must include the converted proxy node name")
	}
	if !strings.Contains(out, "Manual") {
		t.Error("fallback config must include the Manual selector group")
	}
	if !strings.Contains(out, "MATCH,Manual") {
		t.Error("fallback config must include the catch-all MATCH rule")
	}
	// The fallback must NOT carry the external template's injection-surface keys.
	if _, bad := m["proxy-providers"]; bad {
		t.Error("fallback template must not contain proxy-providers")
	}
	if _, bad := m["dns"]; bad {
		t.Error("fallback template must not contain dns")
	}
}

// TestLocalClashTemplateNodeMap_UsablePipeline checks the built-in template
// parses and drives the same inject pipeline as the external one: the empty
// Manual group is filled with the node names plus DIRECT.
func TestLocalClashTemplateNodeMap_UsablePipeline(t *testing.T) {
	nodeMap, err := localClashTemplateNodeMap()
	if err != nil {
		t.Fatalf("localClashTemplateNodeMap: %v", err)
	}
	proxies := []*ClashProxy{{Name: "N1"}, {Name: "N2"}}
	if err := injectProxiesToTemplate(nodeMap, proxies); err != nil {
		t.Fatalf("injectProxiesToTemplate: %v", err)
	}
	data, err := yaml.Marshal(nodeMap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(data)
	for _, want := range []string{"N1", "N2", "DIRECT", "MATCH,Manual"} {
		if !strings.Contains(out, want) {
			t.Errorf("template output missing %q\n%s", want, out)
		}
	}
}
