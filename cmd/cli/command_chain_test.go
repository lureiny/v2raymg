package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestCLI_ChainAndIsolationCommands(t *testing.T) {
	var capturedBody map[string]any
	var capturedMethod, capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedBody = nil
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	prevCfg := *globalConfig
	globalConfig.Host = srv.URL
	globalConfig.Token = "cli-token"
	globalConfig.Username = ""
	globalConfig.Password = ""
	t.Cleanup(func() { *globalConfig = prevCfg })

	// 1. parseCSVStrings
	if got := parseCSVStrings(" vip , hk, "); !reflect.DeepEqual(got, []string{"vip", "hk"}) {
		t.Fatalf("parseCSVStrings=%v, want [vip hk]", got)
	}
	if got := parseCSVStrings("  "); got != nil {
		t.Fatalf("parseCSVStrings blank=%v, want nil", got)
	}

	// 2. setInboundTags clears with empty string
	if err := setInboundTags("node-a", "mihomo", "hy2", ""); err != nil {
		t.Fatalf("setInboundTags: %v", err)
	}
	if capturedMethod != http.MethodPut || capturedPath != "/api/inbounds/tags" || !reflect.DeepEqual(capturedBody["access_tags"], []any{}) {
		t.Fatalf("unexpected setInboundTags request: %s %s %+v", capturedMethod, capturedPath, capturedBody)
	}

	// 3. updateUser sentinel handling
	if err := updateUser("", "alice", "", 0, 0, "", 0, 0, 0, 0, 0, "", ""); err != nil {
		t.Fatalf("updateUser unchanged: %v", err)
	}
	if _, exists := capturedBody["inbound_tags"]; exists {
		t.Fatalf("expected omitted inbound_tags when empty, got %+v", capturedBody)
	}

	if err := updateUser("", "alice", "", 0, 0, "", 0, 0, 0, 0, 0, "", "clear"); err != nil {
		t.Fatalf("updateUser clear: %v", err)
	}
	if !reflect.DeepEqual(capturedBody["inbound_tags"], []any{}) {
		t.Fatalf("expected [] when 'clear', got %+v", capturedBody)
	}

	if err := updateUser("", "alice", "", 0, 0, "", 0, 0, 0, 0, 0, "", "vip,hk"); err != nil {
		t.Fatalf("updateUser set: %v", err)
	}
	if !reflect.DeepEqual(capturedBody["inbound_tags"], []any{"vip", "hk"}) {
		t.Fatalf("expected [vip hk], got %+v", capturedBody)
	}
}
