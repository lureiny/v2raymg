package converter

import (
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// withClashTemplateURLs saves the configured template URLs, applies urls for the
// duration of the test, and restores the original set afterwards. This keeps the
// package-global configuration isolated between tests.
func withClashTemplateURLs(t *testing.T, urls []string) {
	t.Helper()
	orig := clashTemplateURLsSnapshot()
	SetClashTemplateURLs(urls)
	t.Cleanup(func() { SetClashTemplateURLs(orig) })
}

// TestSetClashTemplateURLs_TrimsBlanks verifies that SetClashTemplateURLs drops
// empty/whitespace-only entries and keeps the rest in order.
func TestSetClashTemplateURLs_TrimsBlanks(t *testing.T) {
	withClashTemplateURLs(t, []string{"  https://a/%s ", "", "   ", "https://b/%s"})

	got := clashTemplateURLsSnapshot()
	want := []string{"https://a/%s", "https://b/%s"}
	if len(got) != len(want) {
		t.Fatalf("snapshot len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("snapshot[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSetClashTemplateURLs_SnapshotIsCopy verifies the snapshot cannot be used to
// mutate the internal state (defends the per-request read path).
func TestSetClashTemplateURLs_SnapshotIsCopy(t *testing.T) {
	withClashTemplateURLs(t, []string{"https://a/%s"})
	snap := clashTemplateURLsSnapshot()
	snap[0] = "tampered"
	if again := clashTemplateURLsSnapshot(); again[0] != "https://a/%s" {
		t.Errorf("internal state mutated via snapshot: %q", again[0])
	}
}

// TestFetchClashTemplate_EmptyReturnsError verifies that with no configured
// sources fetchClashTemplate fails (without touching the network) so the caller
// can degrade to the built-in template.
func TestFetchClashTemplate_EmptyReturnsError(t *testing.T) {
	withClashTemplateURLs(t, nil)
	if nm, err := fetchClashTemplate(); err == nil {
		t.Fatalf("expected error with no sources configured, got nodeMap=%v", nm)
	}
}

// TestFetchClashTemplate_MissingPlaceholderSkipped verifies that URLs without a
// "%s" verb are skipped rather than turned into malformed requests. With every
// entry lacking the placeholder, no network call happens and fetch fails.
func TestFetchClashTemplate_MissingPlaceholderSkipped(t *testing.T) {
	withClashTemplateURLs(t, []string{"https://example.com/no-verb", "https://example.org/also-none"})
	_, err := fetchClashTemplate()
	if err == nil {
		t.Fatal("expected error when all sources lack the placeholder verb")
	}
	if !strings.Contains(err.Error(), "all clash template sources failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestConvertWithOptions_EmptyURLsFallsBack is the end-to-end guarantee the
// operator cares about: when subscription.clash_template_urls is empty (no remote
// template), /sub must NOT fail — it degrades to the built-in minimal template.
// This exercises the real fetchClashTemplate path (not the test seam).
func TestConvertWithOptions_EmptyURLsFallsBack(t *testing.T) {
	withClashTemplateURLs(t, nil)

	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVLess,
			Host:       "h.example.com",
			Port:       443,
			Password:   "uuid-1",
			NodeName:   "NoTemplateNode",
			InboundTag: "vless-x",
			Extensions: map[string]any{"security": "tls", "transport": "ws", "ws_path": "/p"},
		},
	}

	out, err := (&ClashConverter{}).ConvertWithOptions(specs, nil)
	if err != nil {
		t.Fatalf("empty clash_template_urls must degrade gracefully, got error: %v", err)
	}
	for _, want := range []string{"NoTemplateNode", "Manual", "MATCH,Manual"} {
		if !strings.Contains(out, want) {
			t.Errorf("fallback output missing %q\n%s", want, out)
		}
	}
}
