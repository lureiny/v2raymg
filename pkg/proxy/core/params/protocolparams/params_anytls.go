package protocolparams

import (
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// AnyTLS-specific bounds. These are pragmatic ceilings, not protocol
// limits — they catch obvious operator-input typos (a 1 MiB padding
// scheme is almost certainly an accidental file-content paste; an idle
// of one year is almost certainly a unit confusion). mihomo upstream
// would either accept gracefully or fail with a clearer error, so these
// guards trade some flexibility for early actionable rejection.
const (
	maxAnyTLSPaddingSchemeBytes = 8192  // upstream DefaultPaddingScheme is ~150 bytes; 8 KiB allows room for very elaborate schemes
	maxAnyTLSIdleSessionSeconds = 86400 // 1 day; mihomo runtime defaults are 30s, anything beyond a day is almost certainly a typo
	maxAnyTLSMinIdleSession     = 1024  // mihomo session/client.go uses MinIdleSession to keep N sessions warm; 1024 already over-provisions even for very large fanout
)

// parseAnyTLS fills the AnyTLS slot of a ProtocolParams from the raw
// request map.
//
// Contract (Phase 7 — see project_protocol_expansion_status memory and
// the Phase 7.0 schema fact-check report):
//
//   - KeyPassword is required (caller-supplied or filled by FillDefaults).
//     mihomo Alpha listener (`listener/anytls/server.go:83-85`) keeps a
//     map keyed by sha256(password); empty strings would degenerate the
//     match. Reject early at the parser instead.
//   - Transport is forced to nil. AnyTLS is TCP-over-TLS with no
//     pluggable transport (`adapter/outbound/anytls.go` has no ws/grpc/h2
//     fields, listener wraps a bare `inbound.Listen("tcp", addr)` then
//     `tls.NewListener`). Any KeyTransport is a caller error.
//   - Security defaults to TLS. Reality and "none" are rejected — the
//     listener hard-fails without a certificate
//     (`server.go:116`: `disallow using AnyTLS without certificates
//     config`).
//   - KeyPaddingScheme optional. Listener-only inline text; empty defers
//     to mihomo's `transport/anytls/padding.DefaultPaddingScheme`. The
//     parser accepts any non-empty string and defers structural
//     validation to mihomo (`util.StringMapFromBytes` + the `stop=`
//     check in `padding.NewPaddingFactory:51-56`).
//   - KeyIdleSessionCheckInterval / KeyIdleSessionTimeout / KeyMinIdleSession
//     optional ints. Client-only knobs surfaced to subscriptions; the
//     listener struct has no equivalents. Negative values are rejected.
//     Values ≤5 are accepted as-is — mihomo silently bumps them to 30s
//     at runtime (`session/client.go:46-51`), so the parser must not
//     mask that fallback by overriding here.
//
// Server-side TLS material (cert/key/SNI/ALPN/skip_cert_verify) flows
// through SecuritySpec.TLS like the other TLS-required protocols
// (Trojan / Hysteria2 / TUIC). AnyTLSParams deliberately does NOT
// duplicate those fields.
func parseAnyTLS(raw map[string]any, pp *ProtocolParams) error {
	password, err := requireString(raw, KeyPassword)
	if err != nil {
		return fmt.Errorf("anytls: %w", err)
	}

	if t, hasTransport := raw[KeyTransport]; hasTransport {
		if s, _ := t.(string); s != "" && strings.ToLower(s) != string(contracts.TransportTCP) {
			return fmt.Errorf("%w: anytls only supports TCP transport, got %q",
				ErrInvalidCombination, s)
		}
	}

	security, err := parseSecurity(raw)
	if err != nil {
		return fmt.Errorf("anytls: %w", err)
	}
	if security == nil {
		tls, err := parseTLS(raw)
		if err != nil {
			return fmt.Errorf("anytls: %w", err)
		}
		security = &SecuritySpec{Kind: contracts.SecurityTLS, TLS: tls}
	}
	switch security.Kind {
	case contracts.SecurityTLS:
		// ok
	case contracts.SecurityReality:
		return fmt.Errorf("%w: anytls does not support reality security; use tls",
			ErrInvalidCombination)
	case contracts.SecurityNone:
		return fmt.Errorf("%w: anytls requires tls security", ErrInvalidCombination)
	default:
		// Defensive: parseSecurity already gates on contracts.Security.IsValid.
		return fmt.Errorf("%w: anytls security %q not supported", ErrInvalidCombination, security.Kind)
	}

	paddingScheme := optionalString(raw, KeyPaddingScheme)
	if paddingScheme != "" {
		if len(paddingScheme) > maxAnyTLSPaddingSchemeBytes {
			return fmt.Errorf("%w: anytls %s exceeds %d-byte cap (got %d) — likely accidental file-content paste",
				ErrInvalidCombination, KeyPaddingScheme, maxAnyTLSPaddingSchemeBytes, len(paddingScheme))
		}
		// Soft format gate: mihomo's padding.NewPaddingFactory requires a
		// `stop=` line and rejects schemes without it at server startup,
		// crashing the mihomo process. Catch the most common typo
		// (omitted/misspelled `stop=`) at parse time so the error surfaces
		// on FastAdd rather than on the next listener push.
		if !strings.Contains(paddingScheme, "stop=") {
			return fmt.Errorf("%w: anytls %s missing required `stop=` line (mihomo padding.NewPaddingFactory rejects it)",
				ErrInvalidCombination, KeyPaddingScheme)
		}
	}

	anytlsSpec := &AnyTLSParams{
		Password:      password,
		PaddingScheme: paddingScheme,
	}
	if v, ok := optionalInt(raw, KeyIdleSessionCheckInterval); ok {
		if v < 0 {
			return fmt.Errorf("%w: anytls %s must be >= 0, got %d",
				ErrInvalidCombination, KeyIdleSessionCheckInterval, v)
		}
		if v > maxAnyTLSIdleSessionSeconds {
			return fmt.Errorf("%w: anytls %s exceeds %d-second cap (got %d) — likely unit confusion (value is seconds, not ms)",
				ErrInvalidCombination, KeyIdleSessionCheckInterval, maxAnyTLSIdleSessionSeconds, v)
		}
		anytlsSpec.IdleSessionCheckInterval = v
	}
	if v, ok := optionalInt(raw, KeyIdleSessionTimeout); ok {
		if v < 0 {
			return fmt.Errorf("%w: anytls %s must be >= 0, got %d",
				ErrInvalidCombination, KeyIdleSessionTimeout, v)
		}
		if v > maxAnyTLSIdleSessionSeconds {
			return fmt.Errorf("%w: anytls %s exceeds %d-second cap (got %d) — likely unit confusion (value is seconds, not ms)",
				ErrInvalidCombination, KeyIdleSessionTimeout, maxAnyTLSIdleSessionSeconds, v)
		}
		anytlsSpec.IdleSessionTimeout = v
	}
	if v, ok := optionalInt(raw, KeyMinIdleSession); ok {
		if v < 0 {
			return fmt.Errorf("%w: anytls %s must be >= 0, got %d",
				ErrInvalidCombination, KeyMinIdleSession, v)
		}
		if v > maxAnyTLSMinIdleSession {
			return fmt.Errorf("%w: anytls %s exceeds %d cap (got %d)",
				ErrInvalidCombination, KeyMinIdleSession, maxAnyTLSMinIdleSession, v)
		}
		anytlsSpec.MinIdleSession = v
	}

	pp.Transport = nil
	pp.Security = security
	pp.AnyTLS = anytlsSpec
	return nil
}
