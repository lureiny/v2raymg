package mihomo

import (
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
)

func makeHy2Inbound(t *testing.T, tag string, params map[string]any) *MihomoInbound {
	t.Helper()
	pp, err := protocolparams.Parse(params)
	if err != nil {
		t.Fatalf("protocolparams.Parse: %v", err)
	}
	pp.Tag = tag
	inb, err := FromProtocolParams(pp)
	if err != nil {
		t.Fatalf("FromProtocolParams: %v", err)
	}
	return inb
}

func hy2BaseInboundParams() map[string]any {
	return map[string]any{
		"protocol":  "hysteria2",
		"port":      uint32(443),
		"password":  "shh",
		"cert_file": "/tmp/cert.pem",
		"key_file":  "/tmp/key.pem",
	}
}

func TestFillHysteria2Listener_Baseline(t *testing.T) {
	inb := makeHy2Inbound(t, "hy2-base", hy2BaseInboundParams())
	m := map[string]any{}
	if err := fillHysteria2Listener(m, inb); err != nil {
		t.Fatalf("fillHysteria2Listener: %v", err)
	}

	// users: { default: <password> } — single-user shape (memory: A).
	// Assert string typing on the password value: mihomo's Hysteria2Option
	// declares Users as `map[string]string`, so emitting an int or other
	// non-string would silently wrap into the listener and fail at decode
	// time. Lock the type here.
	users, ok := m["users"].(map[string]any)
	if !ok {
		t.Fatalf("users = %T, want map[string]any", m["users"])
	}
	pwd, ok := users["default"].(string)
	if !ok {
		t.Fatalf("users[default] = %T, want string", users["default"])
	}
	if pwd != "shh" {
		t.Errorf("users[default] = %q, want shh", pwd)
	}

	if m["certificate"] != "/tmp/cert.pem" {
		t.Errorf("certificate = %q", m["certificate"])
	}
	if m["private-key"] != "/tmp/key.pem" {
		t.Errorf("private-key = %q", m["private-key"])
	}

	// Default ALPN h3 — Hysteria2 is QUIC; h3 is the only meaningful value.
	alpn, ok := m["alpn"].([]string)
	if !ok || !reflect.DeepEqual(alpn, []string{"h3"}) {
		t.Errorf("alpn = %v (%T), want [h3]", m["alpn"], m["alpn"])
	}

	// Bandwidth / obfs / masquerade absent on baseline.
	for _, k := range []string{"obfs", "obfs-password", "up", "down", "ignore-client-bandwidth", "masquerade"} {
		if _, present := m[k]; present {
			t.Errorf("baseline should not emit %q, got %v", k, m[k])
		}
	}
}

func TestFillHysteria2Listener_Salamander(t *testing.T) {
	params := hy2BaseInboundParams()
	params["obfs"] = "salamander"
	params["obfs_password"] = "obfsec"
	inb := makeHy2Inbound(t, "hy2-obfs", params)
	m := map[string]any{}
	if err := fillHysteria2Listener(m, inb); err != nil {
		t.Fatalf("fillHysteria2Listener: %v", err)
	}
	if m["obfs"] != "salamander" {
		t.Errorf("obfs = %q", m["obfs"])
	}
	if m["obfs-password"] != "obfsec" {
		t.Errorf("obfs-password = %q", m["obfs-password"])
	}
}

func TestFillHysteria2Listener_BandwidthAndMasquerade(t *testing.T) {
	params := hy2BaseInboundParams()
	params["up"] = "50 Mbps"
	params["down"] = "100 Mbps"
	params["ignore_client_bandwidth"] = true
	params["masquerade"] = "https://www.microsoft.com"
	inb := makeHy2Inbound(t, "hy2-bw", params)
	m := map[string]any{}
	if err := fillHysteria2Listener(m, inb); err != nil {
		t.Fatalf("fillHysteria2Listener: %v", err)
	}
	if m["up"] != "50 Mbps" {
		t.Errorf("up = %q", m["up"])
	}
	if m["down"] != "100 Mbps" {
		t.Errorf("down = %q", m["down"])
	}
	if v, ok := m["ignore-client-bandwidth"].(bool); !ok || !v {
		t.Errorf("ignore-client-bandwidth = %v", m["ignore-client-bandwidth"])
	}
	if m["masquerade"] != "https://www.microsoft.com" {
		t.Errorf("masquerade = %q", m["masquerade"])
	}
}

func TestFillHysteria2Listener_CustomALPN(t *testing.T) {
	params := hy2BaseInboundParams()
	params["alpn"] = []string{"h3", "h3-32"}
	inb := makeHy2Inbound(t, "hy2-alpn", params)
	m := map[string]any{}
	if err := fillHysteria2Listener(m, inb); err != nil {
		t.Fatalf("fillHysteria2Listener: %v", err)
	}
	alpn, ok := m["alpn"].([]string)
	if !ok || !reflect.DeepEqual(alpn, []string{"h3", "h3-32"}) {
		t.Errorf("alpn = %v, want [h3 h3-32]", m["alpn"])
	}
}

func TestBuildListener_Hysteria2(t *testing.T) {
	// End-to-end through the public BuildListener entry point — confirms
	// the dispatch switch picks the hy2 branch and the top-level keys
	// (name/type/listen/port) come out correctly.
	inb := makeHy2Inbound(t, "hy2-full", hy2BaseInboundParams())
	m, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	if m["type"] != "hysteria2" {
		t.Errorf("type = %q, want hysteria2", m["type"])
	}
	if m["name"] != "hy2-full" {
		t.Errorf("name = %q", m["name"])
	}
	if m["port"] != "443" {
		t.Errorf("port = %v", m["port"])
	}
	if m["listen"] != "127.0.0.1" {
		t.Errorf("listen = %q", m["listen"])
	}
}
