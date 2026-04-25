package protocolparams

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// parseTUIC fills the TUIC slot of a ProtocolParams from the raw request map.
//
// Contract (v5 only — see reference_tuic_protocol_facts.md):
//
//   - KeyUUID + KeyPassword are required (caller-supplied or filled by
//     FillDefaults). UUID is validated via google/uuid; mihomo Alpha's
//     listener parses uuid via uuid.FromStringOrNil and silently zeros
//     invalid input (`listener/tuic/server.go:170`). That collapses all
//     users onto the zero uuid in the `users:` map — we reject early at
//     the parser instead.
//   - Transport is forced to nil. TUIC is QUIC-native, no pluggable
//     transport. Any KeyTransport is a caller error.
//   - Security defaults to TLS. Reality and "none" are rejected. The
//     listener forces TLS 1.3 and Allow0RTT=true at runtime; cert
//     material flows through SecuritySpec.TLS like the other
//     TLS-required protocols.
//   - KeyCongestionController optional. Whitelist: "" | "bbr" | "cubic"
//     | "new_reno". Empty defers to profilegen which writes "bbr"
//     (mihomo's parse-time default lives in `listener/parse.go:116` and
//     does not run on our codepath).
//   - KeyUDPRelayMode optional. Whitelist: "" | "native" | "quic". The
//     listener has no such field (mihomo derives at runtime); we capture
//     for client subscription pass-through only.
//   - KeyZeroRTTHandshake optional bool. Listener forces Allow0RTT=true
//     unconditionally, so the value is a client-side hint surfaced in
//     subscription as `reduce-rtt`.
//   - KeyHeartbeatInterval optional. Client-only field; format is a
//     human-readable duration string (e.g. "10s"). Listener has no
//     heartbeat concept.
//   - KeyDisableSNI optional bool. Client-only knob (mihomo's
//     adapter/outbound/tuic.go::TuicOption::disable-sni; the listener
//     struct has no equivalent — TLS SNI is needed server-side for cert
//     selection). Surfaces only in the subscription path.
func parseTUIC(raw map[string]any, pp *ProtocolParams) error {
	userID, err := requireString(raw, KeyUUID)
	if err != nil {
		return fmt.Errorf("tuic: %w", err)
	}
	if _, parseErr := uuid.Parse(userID); parseErr != nil {
		return fmt.Errorf("%w: tuic uuid %q is not a valid UUID (mihomo would silently zero it)",
			ErrMissingRequired, userID)
	}
	password, err := requireString(raw, KeyPassword)
	if err != nil {
		return fmt.Errorf("tuic: %w", err)
	}

	if _, hasTransport := raw[KeyTransport]; hasTransport {
		return fmt.Errorf("%w: tuic has no pluggable transport (QUIC-native); drop %q",
			ErrInvalidCombination, KeyTransport)
	}

	security, err := parseSecurity(raw)
	if err != nil {
		return fmt.Errorf("tuic: %w", err)
	}
	if security == nil {
		tls, err := parseTLS(raw)
		if err != nil {
			return fmt.Errorf("tuic: %w", err)
		}
		security = &SecuritySpec{Kind: contracts.SecurityTLS, TLS: tls}
	}
	switch security.Kind {
	case contracts.SecurityTLS:
		// ok
	case contracts.SecurityReality:
		return fmt.Errorf("%w: tuic does not support reality security; use tls",
			ErrInvalidCombination)
	case contracts.SecurityNone:
		return fmt.Errorf("%w: tuic requires tls security", ErrInvalidCombination)
	default:
		// Defensive: parseSecurity already gates on contracts.Security.IsValid.
		return fmt.Errorf("%w: tuic security %q not supported", ErrInvalidCombination, security.Kind)
	}

	tuicSpec := &TUICParams{
		UUID:                 userID,
		Password:             password,
		CongestionController: strings.ToLower(strings.TrimSpace(optionalString(raw, KeyCongestionController))),
		UDPRelayMode:         strings.ToLower(strings.TrimSpace(optionalString(raw, KeyUDPRelayMode))),
		HeartbeatInterval:    optionalString(raw, KeyHeartbeatInterval),
	}
	if b, ok := optionalBool(raw, KeyZeroRTTHandshake); ok {
		tuicSpec.ZeroRTTHandshake = b
	}
	if b, ok := optionalBool(raw, KeyDisableSNI); ok {
		tuicSpec.DisableSNI = b
	}

	switch tuicSpec.CongestionController {
	case "", "bbr", "cubic", "new_reno":
		// "" defers to profilegen default ("bbr").
	default:
		return fmt.Errorf("%w: tuic congestion_controller %q not recognised; want one of bbr|cubic|new_reno",
			ErrInvalidCombination, tuicSpec.CongestionController)
	}
	switch tuicSpec.UDPRelayMode {
	case "", "native", "quic":
		// ok
	default:
		return fmt.Errorf("%w: tuic udp_relay_mode %q not recognised; want native|quic",
			ErrInvalidCombination, tuicSpec.UDPRelayMode)
	}

	pp.Transport = nil
	pp.Security = security
	pp.TUIC = tuicSpec
	return nil
}
