package mihomo

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
)

func makeAnyTLSInbound(t *testing.T, tag string, params map[string]any) *MihomoInbound {
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

func anytlsBaseInboundParams() map[string]any {
	return map[string]any{
		"protocol":  "anytls",
		"port":      uint32(443),
		"password":  "shh",
		"cert_file": "/tmp/cert.pem",
		"key_file":  "/tmp/key.pem",
	}
}

func TestFillAnyTLSListener_Baseline(t *testing.T) {
	inb := makeAnyTLSInbound(t, "anytls-base", anytlsBaseInboundParams())
	m := map[string]any{}
	if err := fillAnyTLSListener(m, inb); err != nil {
		t.Fatalf("fillAnyTLSListener: %v", err)
	}

	// users: {default: <password>} — single-user shape, mirroring hy2.
	// Lock the string typing — mihomo AnyTLSOption.Users is
	// map[string]string and a non-string value would silently fail decode.
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

	// padding-scheme should be absent when caller didn't supply one —
	// mihomo's runtime DefaultPaddingScheme handles the empty case, and
	// we want "use mihomo default" and "no value" to remain
	// indistinguishable on the listener wire.
	if _, present := m["padding-scheme"]; present {
		t.Errorf("baseline should not emit padding-scheme, got %v", m["padding-scheme"])
	}

	// Listener-only fields must stay at mihomo default — never emitted.
	for _, k := range []string{
		"client-auth-type", "client-auth-cert", "ech-key", "alpn",
	} {
		if _, present := m[k]; present {
			t.Errorf("baseline should not emit %q, got %v", k, m[k])
		}
	}

	// Client-only fields must not leak into listener yaml.
	for _, k := range []string{
		"idle-session-check-interval", "idle-session-timeout",
		"min-idle-session", "fingerprint", "client-fingerprint",
		"sni", "skip-cert-verify",
	} {
		if _, present := m[k]; present {
			t.Errorf("listener must not emit client-only key %q, got %v", k, m[k])
		}
	}
}

func TestFillAnyTLSListener_PaddingScheme(t *testing.T) {
	scheme := "stop=4\n0=30-30\n1=100-400\n2=400-500\n3=9-9,500-1000"
	params := anytlsBaseInboundParams()
	params["padding_scheme"] = scheme
	inb := makeAnyTLSInbound(t, "anytls-padding", params)
	m := map[string]any{}
	if err := fillAnyTLSListener(m, inb); err != nil {
		t.Fatalf("fillAnyTLSListener: %v", err)
	}
	got, ok := m["padding-scheme"].(string)
	if !ok {
		t.Fatalf("padding-scheme = %T, want string", m["padding-scheme"])
	}
	if got != scheme {
		t.Errorf("padding-scheme = %q, want %q", got, scheme)
	}
}

// fillAnyTLSListener is a verbatim relay; rejection of empty passwords
// happens at the Validate gate (TestValidateAnyTLS_*). If Validate is
// bypassed (e.g. hand-edited InboundStore record), fill must NOT silently
// generate a usable listener with an empty password — the resulting
// listener would accept any client whose password hashes to sha256("").
//
// Lock the wire shape: with Password = "", users["default"] = "" — that's
// the relay-faithful behaviour, but it must be visible in the yaml so
// operators see something is wrong rather than discovering it via traffic
// audit.
func TestFillAnyTLSListener_EmptyPasswordRelaysAsEmpty(t *testing.T) {
	inb := makeAnyTLSInbound(t, "anytls-bad", anytlsBaseInboundParams())
	inb.ProtocolParams.AnyTLS.Password = "" // bypass Validate
	m := map[string]any{}
	if err := fillAnyTLSListener(m, inb); err != nil {
		t.Fatalf("fill should be a verbatim relay, got error: %v", err)
	}
	users, ok := m["users"].(map[string]any)
	if !ok {
		t.Fatalf("users = %T, want map[string]any", m["users"])
	}
	got, ok := users["default"].(string)
	if !ok {
		t.Fatalf("users[default] = %T, want string", users["default"])
	}
	if got != "" {
		t.Errorf("users[default] = %q, want empty (relay should not synthesise password)", got)
	}
}

func TestFillAnyTLSListener_RejectMissingCertPair(t *testing.T) {
	inb := makeAnyTLSInbound(t, "anytls-nocert", anytlsBaseInboundParams())
	inb.ProtocolParams.Security.TLS.CertFile = "" // bypass Validate
	m := map[string]any{}
	if err := fillAnyTLSListener(m, inb); err == nil {
		t.Fatal("fillAnyTLSListener should reject empty cert_file")
	}
}

func TestValidateAnyTLS_BaselineOK(t *testing.T) {
	inb := makeAnyTLSInbound(t, "anytls-validate-ok", anytlsBaseInboundParams())
	if err := inb.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateAnyTLS_RejectMissingCert(t *testing.T) {
	params := anytlsBaseInboundParams()
	delete(params, "cert_file")
	delete(params, "key_file")
	pp, err := protocolparams.Parse(params)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pp.Tag = "anytls-nocert"
	inb := NewMihomoInboundFromProtocolParams(pp)
	if err := inb.Validate(); err == nil {
		t.Fatal("Validate should reject anytls without cert_file/key_file")
	}
}

func TestValidateAnyTLS_NegativeIdleRejected(t *testing.T) {
	inb := makeAnyTLSInbound(t, "anytls-negidle", anytlsBaseInboundParams())
	inb.ProtocolParams.AnyTLS.IdleSessionCheckInterval = -1
	if err := inb.Validate(); err == nil {
		t.Fatal("Validate should reject negative idle_session_check_interval")
	}
}

// TestBuildListener_AnyTLS exercises the BuildListener entry-point (the
// outer `m[type]/m[name]/m[port]/m[listen]` scaffolding + the AnyTLS
// switch dispatch), proving the protocol case is wired in BuildListener
// not just fillAnyTLSListener. Mirrors TestBuildListener_Tuic / _Hysteria2.
func TestBuildListener_AnyTLS(t *testing.T) {
	inb := makeAnyTLSInbound(t, "anytls-full", anytlsBaseInboundParams())
	m, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	if m["type"] != "anytls" {
		t.Errorf("type = %q, want anytls", m["type"])
	}
	if m["name"] != "anytls-full" {
		t.Errorf("name = %q", m["name"])
	}
	if m["port"] != "443" {
		t.Errorf("port = %v", m["port"])
	}
	if m["listen"] != "127.0.0.1" {
		t.Errorf("listen = %q", m["listen"])
	}
}
