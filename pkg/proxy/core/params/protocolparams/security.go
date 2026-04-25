package protocolparams

import (
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// parseSecurity promotes the security layer out of the raw map.
//
// Return shapes:
//
//   - (nil, nil) — no KeySecurity key present; caller substitutes the
//     per-protocol default (none for SS, tls for Trojan/Hy2/TUIC/AnyTLS,
//     whatever caller-supplied or default for VLESS/VMess).
//   - (*SecuritySpec with Kind=none, nil) — explicit "none"; TLS/Reality
//     nested specs are nil.
//   - (*SecuritySpec with Kind=tls and TLS set, nil) — TLS selected; all
//     cert material must be on the raw map (FillDefaults resolves it).
//   - (*SecuritySpec with Kind=reality and Reality set, nil) — Reality.
//   - (nil, error) — unknown KeySecurity value, or missing required
//     sub-fields for the chosen mode.
func parseSecurity(raw map[string]any) (*SecuritySpec, error) {
	kindStr := optionalString(raw, KeySecurity)
	if kindStr == "" {
		return nil, nil
	}
	kind := contracts.Security(strings.ToLower(strings.TrimSpace(kindStr)))
	if !kind.IsValid() {
		return nil, fmt.Errorf("%w: security %q not recognised", ErrProtocolNotSupported, kindStr)
	}

	spec := &SecuritySpec{Kind: kind}

	switch kind {
	case contracts.SecurityNone:
		// No sub-struct; nothing to parse.

	case contracts.SecurityTLS:
		tls, err := parseTLS(raw)
		if err != nil {
			return nil, err
		}
		spec.TLS = tls

	case contracts.SecurityReality:
		rc, err := parseReality(raw)
		if err != nil {
			return nil, err
		}
		spec.Reality = rc
	}

	return spec, nil
}

// parseTLS reads the TLS block. Cert material (cert_file/key_file) is
// expected to be pre-resolved by pkg/proxy/core/params.FillDefaults —
// the parser does not accept raw PEM content or domain lookups here.
// An empty CertFile/KeyFile pair is allowed at the parser layer (the
// protocol-specific validator enforces whether TLS is required).
func parseTLS(raw map[string]any) (*TLSSpec, error) {
	tls := &TLSSpec{
		SNI:             optionalString(raw, KeySNI),
		ALPN:            optionalStringSlice(raw, KeyALPN),
		CertFile:        optionalString(raw, KeyCertFile),
		KeyFile:         optionalString(raw, KeyKeyFile),
		CertSource:      optionalString(raw, KeyCertSource),
		MinVersion:      optionalString(raw, KeyTLSMinVersion),
		UTLSFingerprint: optionalString(raw, KeyUTLSFingerprint),
	}
	if b, ok := optionalBool(raw, KeyTLSRejectUnknownSNI); ok {
		tls.RejectUnknownSNI = b
	}
	if b, ok := optionalBool(raw, KeySkipCertVerify); ok {
		tls.SkipCertVerify = b
	}

	// Cross-field rule: cert_file and key_file must both be set or both
	// empty. Mismatched pair always signals a caller error — enforce here
	// rather than waiting for the adapter to trip over it later.
	if (tls.CertFile == "") != (tls.KeyFile == "") {
		return nil, fmt.Errorf("%w: tls cert_file and key_file must be set together", ErrMissingRequired)
	}

	return tls, nil
}

// parseReality reads the Reality handshake configuration.
//
// Required (no sensible default):
//   - reality_target  (e.g. "www.microsoft.com:443")
//   - reality_server_names (at least one entry)
//
// xray-parity auto-fill (matches pkg/proxy/containers/xray/inbound_adapter.go
// buildStreamSettings Reality branch):
//
//   - reality_short_ids: if the caller omits it, a single random 16-hex
//     shortId is generated. Back-written so the subscription layer sees
//     the same value the listener runs with.
//   - reality_private_key + reality_public_key: must be supplied as a
//     pair or omitted entirely. If both are omitted, an X25519 key pair
//     is generated; if only one is set, that's a caller error.
//     Base64 / base64url encoded 32-byte keys are accepted; any other
//     shape is rejected.
//
// ShortID values (caller-supplied or generated) are validated against the
// 16-hex-char shape Reality expects.
func parseReality(raw map[string]any) (*RealitySpec, error) {
	rc := &RealitySpec{
		Target:       optionalString(raw, KeyRealityTarget),
		ServerNames:  optionalStringSlice(raw, KeyRealityServerNames),
		ShortIDs:     optionalStringSlice(raw, KeyRealityShortIDs),
		PrivateKey:   optionalString(raw, KeyRealityPrivateKey),
		PublicKey:    optionalString(raw, KeyRealityPublicKey),
		MinClientVer: optionalString(raw, KeyRealityMinClientVer),
		MaxTimeDiff:  optionalString(raw, KeyRealityMaxTimeDiff),
	}

	if rc.Target == "" {
		return nil, fmt.Errorf("%w: reality target (reality_target)", ErrMissingRequired)
	}
	if len(rc.ServerNames) == 0 {
		return nil, fmt.Errorf("%w: reality server_names (reality_server_names) needs at least one entry", ErrMissingRequired)
	}

	if err := autofillReality(rc); err != nil {
		return nil, err
	}

	for i, sid := range rc.ShortIDs {
		if err := ValidateRealityShortID(sid); err != nil {
			return nil, fmt.Errorf("reality_short_ids[%d]: %w", i, err)
		}
	}

	// MaxTimeDiff: reject early if provided but not integer-shaped.
	// profilegen used to silently drop unparseable values, which left
	// misconfigured clock-skew settings invisible to the operator.
	if rc.MaxTimeDiff != "" {
		if _, err := parseSignedInt(rc.MaxTimeDiff); err != nil {
			return nil, fmt.Errorf("%w: reality_max_time_diff must be integer seconds, got %q", ErrMissingRequired, rc.MaxTimeDiff)
		}
	}

	return rc, nil
}

// parseSignedInt is a small Atoi shim; lifted out so profilegen and
// parseReality stay in agreement on accepted format.
func parseSignedInt(s string) (int, error) {
	var n int
	var sign = 1
	if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("non-digit %q", c)
		}
		n = n*10 + int(c-'0')
	}
	return sign * n, nil
}

// autofillReality applies xray-parity defaults to a RealitySpec:
//
//   - generates a random 16-hex shortId when none is supplied;
//   - rejects half-supplied key pairs (both or neither);
//   - validates the format of caller-supplied keys;
//   - auto-generates an X25519 key pair when neither key is supplied.
//
// The back-writing for shortIDs matches xray's behaviour so downstream
// subscription consumers see the same value the listener uses.
func autofillReality(rc *RealitySpec) error {
	if len(rc.ShortIDs) == 0 {
		sid, err := GenerateRealityShortID()
		if err != nil {
			return fmt.Errorf("reality: auto-generate shortId: %w", err)
		}
		rc.ShortIDs = []string{sid}
	}

	privPresent := rc.PrivateKey != ""
	pubPresent := rc.PublicKey != ""
	if privPresent != pubPresent {
		return fmt.Errorf("%w: reality_private_key and reality_public_key must be provided together", ErrMissingRequired)
	}
	if privPresent {
		if err := ValidateRealityBase64Key(rc.PrivateKey); err != nil {
			return fmt.Errorf("invalid reality_private_key: %w", err)
		}
		if err := ValidateRealityBase64Key(rc.PublicKey); err != nil {
			return fmt.Errorf("invalid reality_public_key: %w", err)
		}
	} else {
		priv, pub, err := GenerateRealityKeyPair()
		if err != nil {
			return fmt.Errorf("reality: auto-generate key pair: %w", err)
		}
		rc.PrivateKey = priv
		rc.PublicKey = pub
	}

	return nil
}
