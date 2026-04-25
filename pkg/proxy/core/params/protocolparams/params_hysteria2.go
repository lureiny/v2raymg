package protocolparams

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// parseHysteria2 fills the Hysteria2 slot of a ProtocolParams from the raw
// request map.
//
// Contract:
//
//   - KeyPassword is required — caller-supplied or fabricated by FillDefaults.
//   - Transport is forced to nil. Hysteria2 is QUIC-native, mihomo's listener
//     has no pluggable transport layer. Any KeyTransport on the raw map is a
//     caller error.
//   - Security defaults to TLS. Hysteria2 is QUIC-over-TLS at the wire level;
//     explicit security=none and security=reality are both rejected. Cert
//     material flows through SecuritySpec.TLS like the other TLS-required
//     protocols.
//   - KeyObfs is optional. When non-empty, only "salamander" is accepted
//     (mihomo Alpha hysteria2 listener has no other obfs implementation),
//     and KeyObfsPassword becomes required.
//   - KeyUp / KeyDown are optional bandwidth advertisements (e.g. "50 Mbps").
//     KeyIgnoreClientBandwidth is an independent bool — mihomo allows it to
//     coexist with up/down (server picks at runtime), so the parser does not
//     enforce mutual exclusion.
//   - KeyMasquerade is an optional server-side masquerade target (URL,
//     "file:/path", or "proxy:..." form per hysteria2 spec).
func parseHysteria2(raw map[string]any, pp *ProtocolParams) error {
	password, err := requireString(raw, KeyPassword)
	if err != nil {
		return fmt.Errorf("hysteria2: %w", err)
	}

	if _, hasTransport := raw[KeyTransport]; hasTransport {
		return fmt.Errorf("%w: hysteria2 has no pluggable transport (QUIC-native); drop %q",
			ErrInvalidCombination, KeyTransport)
	}

	security, err := parseSecurity(raw)
	if err != nil {
		return fmt.Errorf("hysteria2: %w", err)
	}
	if security == nil {
		tls, err := parseTLS(raw)
		if err != nil {
			return fmt.Errorf("hysteria2: %w", err)
		}
		security = &SecuritySpec{Kind: contracts.SecurityTLS, TLS: tls}
	}
	switch security.Kind {
	case contracts.SecurityTLS:
		// ok
	case contracts.SecurityReality:
		return fmt.Errorf("%w: hysteria2 does not support reality security; use tls",
			ErrInvalidCombination)
	case contracts.SecurityNone:
		return fmt.Errorf("%w: hysteria2 requires tls security", ErrInvalidCombination)
	default:
		// Defensive: parseSecurity already gates on contracts.Security.IsValid,
		// so an unrecognised Kind reaching here would mean a new security type
		// was added without updating this switch.
		return fmt.Errorf("%w: hysteria2 security %q not supported", ErrInvalidCombination, security.Kind)
	}

	hy2 := &Hysteria2Params{
		Password:     password,
		Obfs:         optionalString(raw, KeyObfs),
		ObfsPassword: optionalString(raw, KeyObfsPassword),
		Up:           optionalString(raw, KeyUp),
		Down:         optionalString(raw, KeyDown),
		Masquerade:   optionalString(raw, KeyMasquerade),
	}
	if b, ok := optionalBool(raw, KeyIgnoreClientBandwidth); ok {
		hy2.IgnoreClientBandwidth = b
	}

	switch hy2.Obfs {
	case "":
		// no obfs; obfs_password must also be empty
		if hy2.ObfsPassword != "" {
			return fmt.Errorf("%w: %q set without %q", ErrInvalidCombination, KeyObfsPassword, KeyObfs)
		}
	case "salamander":
		if hy2.ObfsPassword == "" {
			return fmt.Errorf("%w: %q is required when %q=%q",
				ErrMissingRequired, KeyObfsPassword, KeyObfs, hy2.Obfs)
		}
	default:
		return fmt.Errorf("%w: hysteria2 obfs %q not recognised; only \"salamander\" is supported",
			ErrInvalidCombination, hy2.Obfs)
	}

	pp.Transport = nil
	pp.Security = security
	pp.Hysteria2 = hy2
	return nil
}
