package protocolparams

import (
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// parseTransport promotes a transport configuration out of the raw map.
//
// Returns (nil, nil) when no transport key is present — the caller then
// uses the protocol's default (TCP for vless/vmess/trojan, nothing for
// hy2/tuic/anytls which ignore this function entirely).
//
// An explicit KeyTransport value that names an unsupported transport is
// an error; callers should not silently fall back, as "transport=wss"
// (typo) becoming TCP masks the caller's intent.
//
// Transport-specific sub-fields are read opportunistically — if the
// caller specifies Kind=ws plus a ws_path, the ws_path is captured.
// Sub-fields that don't match the Kind are ignored to tolerate callers
// who pass the full convenience-field set regardless of transport.
func parseTransport(raw map[string]any) (*TransportSpec, error) {
	kindStr := optionalString(raw, KeyTransport)
	if kindStr == "" {
		return nil, nil
	}
	// xray treats "http" and "h2" interchangeably for HTTP/2 inbound;
	// normalise to h2 here so downstream switches see one form. mihomo
	// VLESS does NOT support either (VlessOption has no http/h2 field),
	// parseVLESS rejects h2 explicitly — this normalisation only
	// matters for protocols/containers that do support h2.
	if kindStr == "http" {
		kindStr = "h2"
	}
	kind := contracts.Transport(strings.ToLower(strings.TrimSpace(kindStr)))

	// Gate the Kind against the FastAdd-supported transport set. This
	// list mirrors pkg/http/fastAddInbound_handler.go:102-105 so the
	// layers agree on "allowed" vs "allowed-but-legacy".
	switch kind {
	case contracts.TransportTCP, contracts.TransportWS, contracts.TransportGRPC,
		contracts.TransportHTTPUpgrade, contracts.TransportXHTTP,
		contracts.TransportSplitHTTP, "h2":
		// ok
	default:
		return nil, fmt.Errorf("%w: transport %q not supported in FastAdd", ErrProtocolNotSupported, kindStr)
	}

	spec := &TransportSpec{Kind: kind}

	switch kind {
	case contracts.TransportWS:
		spec.WSPath = optionalString(raw, KeyWSPath)
		spec.WSHost = optionalString(raw, KeyWSHost)

	case contracts.TransportGRPC:
		spec.GRPCServiceName = optionalString(raw, KeyGRPCServiceName)

	case contracts.TransportHTTPUpgrade:
		spec.HTTPUpgradePath = optionalString(raw, KeyHTTPUpgradePath)
		spec.HTTPUpgradeHost = optionalString(raw, KeyHTTPUpgradeHost)

	case contracts.TransportXHTTP, contracts.TransportSplitHTTP:
		spec.XHTTPPath = optionalString(raw, KeyXHTTPPath)
		spec.XHTTPHost = optionalString(raw, KeyXHTTPHost)
		spec.XHTTPMode = optionalString(raw, KeyXHTTPMode)

	case "h2":
		spec.HTTPPath = optionalString(raw, KeyHTTPPath)
		spec.HTTPHost = optionalStringSlice(raw, KeyHTTPHost)
	}

	return spec, nil
}

// optionalStringSlice reads a key expected to hold a []string, accepting
// several on-wire shapes:
//   - []string — already-decoded form (RPC handler post-split)
//   - []any — yaml's default for a list
//   - string — comma-separated; split and trimmed
//
// Returns nil for missing / empty values so callers distinguish "explicit
// empty list" (rare, but if needed they read the raw map directly).
func optionalStringSlice(raw map[string]any, key string) []string {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if x == "" {
			return nil
		}
		parts := strings.Split(x, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// optionalBool reads a key that may be a bool, string ("true"/"false"), or
// absent. Returns (false, false) for missing / unparseable values. Used
// by parseSecurity and the per-protocol parsers to read UDP / self_signed
// / zero_rtt_handshake / etc.
func optionalBool(raw map[string]any, key string) (value bool, ok bool) {
	v, present := raw[key]
	if !present {
		return false, false
	}
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no", "":
			return false, true
		}
	}
	return false, false
}

// optionalInt reads a key that may be an integer in several shapes. Used
// by per-protocol parsers for alter_id / min_idle_session / etc. Returns
// (0, false) for missing; out-of-range ints32 produce an error path the
// caller must handle (here we keep it to a boolean "was it int-shaped?"
// contract; range checks live in the per-protocol parsers).
func optionalInt(raw map[string]any, key string) (value int, ok bool) {
	v, present := raw[key]
	if !present {
		return 0, false
	}
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case float64:
		if x == float64(int(x)) {
			return int(x), true
		}
	case string:
		var parsed int
		if _, err := fmt.Sscanf(x, "%d", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}
