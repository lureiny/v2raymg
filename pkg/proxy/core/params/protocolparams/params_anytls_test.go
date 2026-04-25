package protocolparams

import (
	"errors"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func anytlsBaseRaw() map[string]any {
	return map[string]any{
		"protocol":  "anytls",
		"port":      uint32(443),
		"password":  "secret",
		"cert_file": "/tmp/cert.pem",
		"key_file":  "/tmp/key.pem",
	}
}

func TestParseAnyTLS_Basic(t *testing.T) {
	pp, err := Parse(anytlsBaseRaw())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Protocol != contracts.ProtocolAnyTLS {
		t.Errorf("Protocol = %q, want anytls", pp.Protocol)
	}
	if pp.AnyTLS == nil {
		t.Fatal("AnyTLS is nil")
	}
	if pp.AnyTLS.Password != "secret" {
		t.Errorf("password not propagated: %+v", pp.AnyTLS)
	}
	if pp.Transport != nil {
		t.Errorf("Transport must be nil for anytls, got %+v", pp.Transport)
	}
	if pp.Security == nil || pp.Security.Kind != contracts.SecurityTLS {
		t.Fatalf("Security must default to tls, got %+v", pp.Security)
	}
	if pp.Security.TLS == nil ||
		pp.Security.TLS.CertFile != "/tmp/cert.pem" || pp.Security.TLS.KeyFile != "/tmp/key.pem" {
		t.Errorf("cert/key not propagated: %+v", pp.Security.TLS)
	}
}

func TestParseAnyTLS_AllowsEmptyCertPair(t *testing.T) {
	// Mirror parseHysteria2/parseTUIC behaviour: parser is permissive on
	// empty cert material, the adapter (inbound.validateAnyTLS) enforces
	// presence.
	raw := map[string]any{
		"protocol": "anytls",
		"port":     uint32(443),
		"password": "secret",
	}
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Security == nil || pp.Security.TLS == nil {
		t.Fatal("Security.TLS must be set even with empty cert/key")
	}
}

func TestParseAnyTLS_MissingPassword(t *testing.T) {
	raw := anytlsBaseRaw()
	delete(raw, "password")
	_, err := Parse(raw)
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
}

func TestParseAnyTLS_EmptyPassword(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["password"] = ""
	_, err := Parse(raw)
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired for empty password, got %v", err)
	}
}

func TestParseAnyTLS_RejectTransport(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["transport"] = "ws"
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for transport, got %v", err)
	}
}

func TestParseAnyTLS_RejectSecurityNone(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["security"] = "none"
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for security=none, got %v", err)
	}
}

func TestParseAnyTLS_RejectSecurityReality(t *testing.T) {
	// Mirror TUIC's reality-reject test: pass enough reality fields for
	// parseSecurity to accept the spec shape, then rely on parseAnyTLS's
	// own gate to refuse reality.
	raw := anytlsBaseRaw()
	raw["security"] = "reality"
	raw["reality_target"] = "www.microsoft.com:443"
	raw["reality_server_names"] = []string{"www.microsoft.com"}
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for security=reality, got %v", err)
	}
}

func TestParseAnyTLS_PaddingScheme_PassThrough(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["padding_scheme"] = "stop=4\n0=30-30\n1=100-400\n2=400-500\n3=9-9,500-1000"
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.AnyTLS.PaddingScheme == "" {
		t.Errorf("PaddingScheme not propagated")
	}
}

func TestParseAnyTLS_PaddingScheme_Default(t *testing.T) {
	pp, err := Parse(anytlsBaseRaw())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.AnyTLS.PaddingScheme != "" {
		t.Errorf("PaddingScheme defaulted unexpectedly: %q", pp.AnyTLS.PaddingScheme)
	}
}

// TestParseAnyTLS_PaddingScheme_RejectMissingStop catches the most common
// operator typo: writing the format header without `stop=`. mihomo's
// padding.NewPaddingFactory rejects schemes without `stop=` at server
// startup, crashing the mihomo process. Catching it at FastAdd time turns
// a silent crash into a clear 400.
func TestParseAnyTLS_PaddingScheme_RejectMissingStop(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["padding_scheme"] = "0=30-30\n1=100-400" // no `stop=` line
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for padding_scheme without stop=, got %v", err)
	}
}

// TestParseAnyTLS_PaddingScheme_RejectOversize protects against accidental
// file-content paste (operator typing `padding_scheme=$(cat ...)` and
// loading megabytes).
func TestParseAnyTLS_PaddingScheme_RejectOversize(t *testing.T) {
	raw := anytlsBaseRaw()
	// Build an 8KiB+1 byte string that still contains `stop=`, so the
	// length check is the failing guard (not the missing-stop guard).
	big := strings.Repeat("X", maxAnyTLSPaddingSchemeBytes+1) + "\nstop=4"
	raw["padding_scheme"] = big
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for oversize padding_scheme, got %v", err)
	}
}

func TestParseAnyTLS_IdleCheck_RejectOverCap(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["idle_session_check_interval_seconds"] = maxAnyTLSIdleSessionSeconds + 1
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for over-cap idle_session_check_interval_seconds, got %v", err)
	}
}

func TestParseAnyTLS_IdleTimeout_RejectOverCap(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["idle_session_timeout_seconds"] = maxAnyTLSIdleSessionSeconds + 1
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for over-cap idle_session_timeout_seconds, got %v", err)
	}
}

func TestParseAnyTLS_MinIdle_RejectOverCap(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["min_idle_session"] = maxAnyTLSMinIdleSession + 1
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for over-cap min_idle_session, got %v", err)
	}
}

// TestParseAnyTLS_PaddingScheme_Verbatim locks "no normalisation". A
// future contributor adding a "helpful" CRLF→LF or trim whitespace pass
// would change the byte string mihomo's parser sees and could subtly
// break NewPaddingFactory's StringMapFromBytes (which is byte-sensitive
// inside value tokens).
func TestParseAnyTLS_PaddingScheme_Verbatim(t *testing.T) {
	scheme := "  stop=4\r\n0=30-30 \n1=100-400\r\n2=400-500\n3=9-9,500-1000\n"
	raw := anytlsBaseRaw()
	raw["padding_scheme"] = scheme
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.AnyTLS.PaddingScheme != scheme {
		t.Errorf("PaddingScheme not byte-identical to input.\n have %q\n want %q",
			pp.AnyTLS.PaddingScheme, scheme)
	}
}

func TestParseAnyTLS_IdleSessionCheckInterval_Int(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["idle_session_check_interval_seconds"] = 30
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.AnyTLS.IdleSessionCheckInterval != 30 {
		t.Errorf("IdleSessionCheckInterval = %d, want 30", pp.AnyTLS.IdleSessionCheckInterval)
	}
}

func TestParseAnyTLS_IdleSessionCheckInterval_Float64(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["idle_session_check_interval_seconds"] = float64(45)
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.AnyTLS.IdleSessionCheckInterval != 45 {
		t.Errorf("IdleSessionCheckInterval = %d, want 45 (float64 input)", pp.AnyTLS.IdleSessionCheckInterval)
	}
}

func TestParseAnyTLS_IdleSessionCheckInterval_String(t *testing.T) {
	// optionalInt accepts string-shaped numbers (some HTTP form encoders
	// stringify integers); confirm it round-trips.
	raw := anytlsBaseRaw()
	raw["idle_session_check_interval_seconds"] = "60"
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.AnyTLS.IdleSessionCheckInterval != 60 {
		t.Errorf("IdleSessionCheckInterval = %d, want 60", pp.AnyTLS.IdleSessionCheckInterval)
	}
}

func TestParseAnyTLS_IdleSessionTimeout_PassThrough(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["idle_session_timeout_seconds"] = 90
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.AnyTLS.IdleSessionTimeout != 90 {
		t.Errorf("IdleSessionTimeout = %d, want 90", pp.AnyTLS.IdleSessionTimeout)
	}
}

func TestParseAnyTLS_MinIdleSession_PassThrough(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["min_idle_session"] = 5
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.AnyTLS.MinIdleSession != 5 {
		t.Errorf("MinIdleSession = %d, want 5", pp.AnyTLS.MinIdleSession)
	}
}

func TestParseAnyTLS_NegativeIdleCheck_Reject(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["idle_session_check_interval_seconds"] = -1
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for negative idle_session_check_interval_seconds, got %v", err)
	}
}

func TestParseAnyTLS_NegativeIdleTimeout_Reject(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["idle_session_timeout_seconds"] = -1
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for negative idle_session_timeout_seconds, got %v", err)
	}
}

func TestParseAnyTLS_NegativeMinIdle_Reject(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["min_idle_session"] = -1
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for negative min_idle_session, got %v", err)
	}
}

func TestParseAnyTLS_LowIdleNotBumped(t *testing.T) {
	// mihomo's session/client.go:46-51 silently bumps ≤5s values to 30s
	// at runtime. The parser must NOT mask that — pass small values
	// through verbatim so the operator sees the same number they typed.
	raw := anytlsBaseRaw()
	raw["idle_session_check_interval_seconds"] = 3
	raw["idle_session_timeout_seconds"] = 1
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.AnyTLS.IdleSessionCheckInterval != 3 {
		t.Errorf("IdleSessionCheckInterval = %d, want 3 (parser must not override mihomo runtime fallback)",
			pp.AnyTLS.IdleSessionCheckInterval)
	}
	if pp.AnyTLS.IdleSessionTimeout != 1 {
		t.Errorf("IdleSessionTimeout = %d, want 1 (parser must not override mihomo runtime fallback)",
			pp.AnyTLS.IdleSessionTimeout)
	}
}

func TestParseAnyTLS_IdleFieldsAbsent(t *testing.T) {
	pp, err := Parse(anytlsBaseRaw())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.AnyTLS.IdleSessionCheckInterval != 0 ||
		pp.AnyTLS.IdleSessionTimeout != 0 ||
		pp.AnyTLS.MinIdleSession != 0 {
		t.Errorf("absent idle fields should be zero, got %+v", pp.AnyTLS)
	}
}

func TestParseAnyTLS_TLSExtras(t *testing.T) {
	// Confirm TLS knobs from the shared TLSSpec layer survive into the
	// parsed Security.TLS without AnyTLSParams duplicating them. SNI and
	// SkipCertVerify are the most user-visible.
	raw := anytlsBaseRaw()
	raw["sni"] = "example.com"
	raw["skip_cert_verify"] = true
	raw["alpn"] = "h2,http/1.1"
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Security.TLS.SNI != "example.com" {
		t.Errorf("SNI = %q, want example.com", pp.Security.TLS.SNI)
	}
	if !pp.Security.TLS.SkipCertVerify {
		t.Errorf("SkipCertVerify lost")
	}
	if len(pp.Security.TLS.ALPN) != 2 {
		t.Errorf("ALPN = %v, want [h2 http/1.1]", pp.Security.TLS.ALPN)
	}
}

func TestParseAnyTLS_IsReadOnly(t *testing.T) {
	raw := anytlsBaseRaw()
	raw["padding_scheme"] = "stop=4\n0=30-30\n1=100-400\n2=400-500\n3=9-9,500-1000"
	raw["idle_session_check_interval_seconds"] = 30
	raw["idle_session_timeout_seconds"] = 60
	raw["min_idle_session"] = 3

	before := make(map[string]any, len(raw))
	for k, v := range raw {
		before[k] = v
	}
	if _, err := Parse(raw); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(raw) != len(before) {
		t.Errorf("Parse mutated raw map (size changed): %v -> %v", before, raw)
	}
	for k, want := range before {
		if got := raw[k]; got != want {
			t.Errorf("Parse mutated raw[%q]: %v -> %v", k, want, got)
		}
	}
}
