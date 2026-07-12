package converter

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestPruneUnsafeTemplateKeys verifies the unsafe top-level keys the project
// never emits (dns, proxy-providers) are stripped from a third-party template,
// while the keys the project does produce are preserved.
func TestPruneUnsafeTemplateKeys(t *testing.T) {
	src := []byte("dns:\n" +
		"  enable: true\n" +
		"proxy-providers:\n" +
		"  p: {}\n" +
		"proxies: []\n" +
		"rules:\n" +
		"  - MATCH,DIRECT\n" +
		"rule-providers:\n" +
		"  r: {}\n" +
		"proxy-groups: []\n")

	nm := NodeMap{}
	if err := yaml.Unmarshal(src, nm); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	pruneUnsafeTemplateKeys(nm)

	for _, k := range []string{"dns", "proxy-providers"} {
		if _, ok := nm[k]; ok {
			t.Errorf("unsafe key %q was not stripped", k)
		}
	}
	for _, k := range []string{"proxies", "rules", "rule-providers", "proxy-groups"} {
		if _, ok := nm[k]; !ok {
			t.Errorf("legitimate key %q was wrongly stripped", k)
		}
	}
}
