package mihomo

import (
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
)

const tuicProfileTestUUID = "00112233-4455-6677-8899-aabbccddeeff"

func makeTuicInbound(t *testing.T, tag string, params map[string]any) *MihomoInbound {
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

func tuicBaseInboundParams() map[string]any {
	return map[string]any{
		"protocol":  "tuic",
		"port":      uint32(443),
		"uuid":      tuicProfileTestUUID,
		"password":  "shh",
		"cert_file": "/tmp/cert.pem",
		"key_file":  "/tmp/key.pem",
	}
}

func TestFillTuicListener_Baseline(t *testing.T) {
	inb := makeTuicInbound(t, "tuic-base", tuicBaseInboundParams())
	m := map[string]any{}
	if err := fillTuicListener(m, inb); err != nil {
		t.Fatalf("fillTuicListener: %v", err)
	}

	// users: {<uuid>: <password>} — v5 single-user shape (memory: A).
	// Lock the *string* typing on the password: mihomo's TuicOption.Users
	// is map[string]string and a non-string value would silently wrap in
	// and fail at decode time.
	users, ok := m["users"].(map[string]any)
	if !ok {
		t.Fatalf("users = %T, want map[string]any", m["users"])
	}
	pwd, ok := users[tuicProfileTestUUID].(string)
	if !ok {
		t.Fatalf("users[uuid] = %T, want string", users[tuicProfileTestUUID])
	}
	if pwd != "shh" {
		t.Errorf("users[uuid] = %q, want shh", pwd)
	}
	// v4 token form must not leak in.
	if _, present := m["token"]; present {
		t.Errorf("listener must not emit token: (v4) when v5 users: present; got %v", m["token"])
	}

	if m["certificate"] != "/tmp/cert.pem" {
		t.Errorf("certificate = %q", m["certificate"])
	}
	if m["private-key"] != "/tmp/key.pem" {
		t.Errorf("private-key = %q", m["private-key"])
	}
	if v, ok := m["congestion-controller"].(string); !ok || v != "bbr" {
		t.Errorf("congestion-controller = %v (%T), want bbr (string)", m["congestion-controller"], m["congestion-controller"])
	}

	alpn, ok := m["alpn"].([]string)
	if !ok || !reflect.DeepEqual(alpn, []string{"h3"}) {
		t.Errorf("alpn = %v (%T), want [h3]", m["alpn"], m["alpn"])
	}

	// Listener-only fields stay at mihomo default — must not be emitted.
	for _, k := range []string{
		"max-idle-time", "authentication-timeout", "cwnd",
		"max-udp-relay-packet-size", "bbr-profile", "mux-option",
		"client-auth-type", "client-auth-cert", "ech-key",
	} {
		if _, present := m[k]; present {
			t.Errorf("baseline should not emit %q, got %v", k, m[k])
		}
	}
	// Client-only fields must not leak into listener yaml.
	for _, k := range []string{
		"reduce-rtt", "heartbeat-interval", "udp-relay-mode", "disable-sni",
		"zero-rtt-handshake",
	} {
		if _, present := m[k]; present {
			t.Errorf("listener must not emit client-only key %q, got %v", k, m[k])
		}
	}
}

func TestFillTuicListener_CustomCongestionController(t *testing.T) {
	for _, cc := range []string{"bbr", "cubic", "new_reno"} {
		t.Run(cc, func(t *testing.T) {
			params := tuicBaseInboundParams()
			params["congestion_controller"] = cc
			inb := makeTuicInbound(t, "tuic-cc-"+cc, params)
			m := map[string]any{}
			if err := fillTuicListener(m, inb); err != nil {
				t.Fatalf("fillTuicListener: %v", err)
			}
			if got, _ := m["congestion-controller"].(string); got != cc {
				t.Errorf("congestion-controller = %q, want %q", got, cc)
			}
		})
	}
}

func TestFillTuicListener_CustomALPN(t *testing.T) {
	params := tuicBaseInboundParams()
	params["alpn"] = []string{"h3", "h3-32"}
	inb := makeTuicInbound(t, "tuic-alpn", params)
	m := map[string]any{}
	if err := fillTuicListener(m, inb); err != nil {
		t.Fatalf("fillTuicListener: %v", err)
	}
	alpn, ok := m["alpn"].([]string)
	if !ok || !reflect.DeepEqual(alpn, []string{"h3", "h3-32"}) {
		t.Errorf("alpn = %v, want [h3 h3-32]", m["alpn"])
	}
}

// Listener-side ZeroRTTHandshake / HeartbeatInterval / UDPRelayMode /
// DisableSNI are CLIENT-only knobs in mihomo's schema; even if a caller
// passes them via FastAdd they must NOT show up in the listener yaml.
func TestFillTuicListener_DropsClientOnlyKnobs(t *testing.T) {
	params := tuicBaseInboundParams()
	params["zero_rtt_handshake"] = true
	params["heartbeat_interval"] = "10s"
	params["udp_relay_mode"] = "quic"
	params["disable_sni"] = true
	inb := makeTuicInbound(t, "tuic-client-knobs", params)
	m := map[string]any{}
	if err := fillTuicListener(m, inb); err != nil {
		t.Fatalf("fillTuicListener: %v", err)
	}
	for _, k := range []string{
		"reduce-rtt", "zero-rtt-handshake", "heartbeat-interval", "udp-relay-mode",
		"disable-sni", "disable_sni",
	} {
		if _, present := m[k]; present {
			t.Errorf("listener must not emit client-only key %q, got %v", k, m[k])
		}
	}
}

func TestBuildListener_Tuic(t *testing.T) {
	inb := makeTuicInbound(t, "tuic-full", tuicBaseInboundParams())
	m, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	if m["type"] != "tuic" {
		t.Errorf("type = %q, want tuic", m["type"])
	}
	if m["name"] != "tuic-full" {
		t.Errorf("name = %q", m["name"])
	}
	if m["port"] != "443" {
		t.Errorf("port = %v", m["port"])
	}
	if m["listen"] != "127.0.0.1" {
		t.Errorf("listen = %q", m["listen"])
	}
}
