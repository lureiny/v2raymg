package protocolparams

import (
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// Parse promotes a raw FastAddInbound payload into a typed *ProtocolParams.
//
// The raw map is expected to have already been through
// pkg/proxy/core/params.FillDefaults, which means:
//   - Credentials (uuid/password/cipher) are filled or caller-supplied.
//   - For TLS-required protocols, cert_file + key_file + cert_source are
//     materialised to absolute paths.
//
// Parse is read-only: it does not mutate raw. Two calls with the same raw
// map produce equal *ProtocolParams values.
//
// Errors wrap ErrProtocolNotSupported / ErrMissingRequired /
// ErrInvalidCombination with field-level context. HTTP/RPC handlers use
// errors.Is on these sentinels to decide status codes.
//
// Phase 0 ships this as a stub returning ErrProtocolNotSupported for every
// protocol. Each subsequent phase (per plan docs/cosmic-leaping-moon.md /
// docs/mihomo-protocol-expansion-plan.md) fills in one parseXxx branch.
func Parse(raw map[string]any) (*ProtocolParams, error) {
	if raw == nil {
		return nil, fmt.Errorf("%w: params map is nil", ErrMissingRequired)
	}

	protoStr, err := requireString(raw, KeyProtocol)
	if err != nil {
		return nil, err
	}
	protocol := contracts.Protocol(normaliseProtocolString(protoStr))
	if !protocol.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrProtocolNotSupported, protoStr)
	}

	port, err := requireUint32(raw, KeyPort)
	if err != nil {
		return nil, err
	}

	pp := &ProtocolParams{
		Protocol:   protocol,
		Port:       port,
		Tag:        optionalString(raw, KeyTag),
		ListenAddr: optionalString(raw, KeyListenAddr),
	}
	if pp.ListenAddr == "" {
		pp.ListenAddr = "127.0.0.1"
	}

	// Protocol-specific branches are filled in by subsequent tasks. Until
	// a branch is implemented, Parse returns ErrProtocolNotSupported so
	// callers get a clear error rather than a zero-valued struct.
	switch protocol {
	case contracts.ProtocolVLess:
		if err := parseVLESS(raw, pp); err != nil {
			return nil, err
		}
		return pp, nil
	case contracts.ProtocolVMess:
		if err := parseVMess(raw, pp); err != nil {
			return nil, err
		}
		return pp, nil
	case contracts.ProtocolTrojan:
		if err := parseTrojan(raw, pp); err != nil {
			return nil, err
		}
		return pp, nil
	case contracts.ProtocolShadowsocks:
		if err := parseSS(raw, pp); err != nil {
			return nil, err
		}
		return pp, nil
	case contracts.ProtocolHysteria2:
		if err := parseHysteria2(raw, pp); err != nil {
			return nil, err
		}
		return pp, nil
	case contracts.ProtocolTUIC:
		if err := parseTUIC(raw, pp); err != nil {
			return nil, err
		}
		return pp, nil
	case contracts.ProtocolAnyTLS:
		return nil, fmt.Errorf("%w: anytls parser not yet implemented", ErrProtocolNotSupported)
	default:
		// SOCKS5 / HTTP / Snell: not handled through FastAddInbound's
		// ProtocolParams path (they have their own container-specific flows).
		return nil, fmt.Errorf("%w: %q not handled by FastAdd ProtocolParams layer", ErrProtocolNotSupported, protocol)
	}
}

// normaliseProtocolString folds a few historical aliases to the canonical
// form. Mirrors the logic in pkg/http/fastAddInbound_handler.go's
// validProtocols map.
func normaliseProtocolString(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ss":
		return string(contracts.ProtocolShadowsocks)
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

// ---- small helpers shared by every parseXxx to come ----

// requireString reads a key that must be present and non-empty.
func requireString(raw map[string]any, key string) (string, error) {
	v, ok := raw[key]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrMissingRequired, key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%w: %q must be string, got %T", ErrMissingRequired, key, v)
	}
	if s == "" {
		return "", fmt.Errorf("%w: %q must not be empty", ErrMissingRequired, key)
	}
	return s, nil
}

// optionalString reads a key that may be absent. Returns "" when missing,
// when the stored value is not a string, or when the string is empty.
// Callers that need to distinguish "missing" from "empty" must use the
// raw map directly.
func optionalString(raw map[string]any, key string) string {
	v, ok := raw[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// requireUint32 parses an integral port-like value from the map, accepting
// every numeric shape the various decoders produce:
//   - int / int64 — literal yaml / config structs
//   - float64 — encoding/json default for JSON numbers
//   - int32 — proto-generated structs (FastAddInboundReq.Port is int32)
//   - uint / uint16 / uint32 — typed callers
//
// 0 is accepted — the adapter layer interprets it as "allocate one".
// Out-of-range values produce ErrMissingRequired-wrapped errors so callers
// get a single branch for "port missing or invalid".
func requireUint32(raw map[string]any, key string) (uint32, error) {
	v, ok := raw[key]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrMissingRequired, key)
	}
	switch x := v.(type) {
	case uint32:
		return x, nil
	case uint16:
		return uint32(x), nil
	case uint:
		if x > 65535 {
			return 0, fmt.Errorf("%w: %q out of range [0,65535]: %d", ErrMissingRequired, key, x)
		}
		return uint32(x), nil
	case int:
		if x < 0 || x > 65535 {
			return 0, fmt.Errorf("%w: %q out of range [0,65535]: %d", ErrMissingRequired, key, x)
		}
		return uint32(x), nil
	case int32:
		if x < 0 || x > 65535 {
			return 0, fmt.Errorf("%w: %q out of range [0,65535]: %d", ErrMissingRequired, key, x)
		}
		return uint32(x), nil
	case int64:
		if x < 0 || x > 65535 {
			return 0, fmt.Errorf("%w: %q out of range [0,65535]: %d", ErrMissingRequired, key, x)
		}
		return uint32(x), nil
	case float64:
		if x != float64(uint32(x)) || x < 0 || x > 65535 {
			return 0, fmt.Errorf("%w: %q must be integer in [0,65535]: %v", ErrMissingRequired, key, x)
		}
		return uint32(x), nil
	default:
		return 0, fmt.Errorf("%w: %q must be numeric, got %T", ErrMissingRequired, key, v)
	}
}
