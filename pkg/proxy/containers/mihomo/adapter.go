package mihomo

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
)

// FromParams builds a MihomoInbound from a FastAddInbound params map.
//
// Required keys (all protocols):
//   - protocol: string ("vmess" | "trojan" | "shadowsocks")
//   - port:     numeric; accepted as uint32, int, int64, or float64
//
// Required protocol-specific credentials:
//   - vmess:        uuid     (string)
//   - trojan:       password (string)
//   - shadowsocks:  password (string), cipher (string)
//
// Optional:
//   - listen_addr: string; defaults to 127.0.0.1. Non-loopback values are
//     accepted but strongly discouraged — the forward layer is
//     the only sanctioned ingress path (see principle 3).
//   - cert_file / key_file: trojan only. Absolute paths to PEM cert and
//     private key files. Mihomo refuses to start a trojan
//     listener without them. Both must be set together or
//     both omitted (omitting is allowed at adapter layer for
//     legacy/unit-test configurations, but a production
//     trojan inbound without them will fail at mihomo
//     startup).
//
// Errors wrap ErrProtocolNotSupported or ErrMissingCredential from inbound.go
// so HTTP handlers can map them to appropriate status codes.
func FromParams(tag string, params map[string]any) (*MihomoInbound, error) {
	protocol, err := requireString(params, "protocol")
	if err != nil {
		return nil, err
	}

	port, err := requirePort(params, "port")
	if err != nil {
		return nil, err
	}

	cred, err := extractSharedCred(contracts.Protocol(protocol), params)
	if err != nil {
		return nil, err
	}

	inb := NewMihomoInbound(tag, contracts.Protocol(protocol), port, cred)

	if listen, ok := params["listen_addr"].(string); ok && listen != "" {
		if !isLoopbackListen(listen) {
			// Non-loopback binds expose mihomo directly to the network,
			// bypassing the forward layer. Principle 3 says forward is the
			// only sanctioned ingress path; warn so operators who did this
			// by accident can spot it in the logs. Not rejected because
			// tests and one-off integration scenarios may still want it.
			log.Warnf("mihomo: inbound %q listen_addr=%q is non-loopback; forward layer is the sanctioned external ingress path (see docs/container-design-principles.md principle 3)", tag, listen)
		}
		inb.DefaultInbound.SetListenAddr(listen)
	}

	// Final validation: catches Tag/Port bounds plus any per-protocol rule
	// that extractSharedCred missed. Redundant with above for the happy path,
	// cheap insurance for edge cases (e.g. whitespace-only tag).
	if err := inb.Validate(); err != nil {
		return nil, err
	}
	return inb, nil
}

func extractSharedCred(protocol contracts.Protocol, params map[string]any) (MihomoSharedCred, error) {
	switch protocol {
	case contracts.ProtocolVMess:
		uuid, err := requireString(params, "uuid")
		if err != nil {
			return MihomoSharedCred{}, err
		}
		return MihomoSharedCred{UUID: uuid}, nil

	case contracts.ProtocolTrojan:
		pw, err := requireString(params, "password")
		if err != nil {
			return MihomoSharedCred{}, err
		}
		cred := MihomoSharedCred{Password: pw}
		// cert_file / key_file are optional at the adapter layer — but
		// mihomo Alpha refuses to boot a trojan listener without TLS
		// credentials (verified at stage 11+ E2E). Callers in production
		// should always pass a valid pair; tests that intentionally omit
		// them will still fail at mihomo startup, with an error that
		// clearly names the missing config. The adapter only enforces
		// the "both or neither" pairing rule (see Validate); it does not
		// fabricate default certs.
		if certFile, ok := params["cert_file"].(string); ok && certFile != "" {
			cred.CertFile = certFile
		}
		if keyFile, ok := params["key_file"].(string); ok && keyFile != "" {
			cred.KeyFile = keyFile
		}
		// cert_source (optional) is populated by pkg/proxy/core/params
		// during RPC FastAddInbound normalisation. It tells
		// RemoveInboundConfig whether the cert files are ours to delete.
		// Legacy records without this field default to "" which is
		// treated as "external" (don't touch).
		if src, ok := params["cert_source"].(string); ok && src != "" {
			cred.CertSource = src
		}
		return cred, nil

	case contracts.ProtocolShadowsocks:
		pw, err := requireString(params, "password")
		if err != nil {
			return MihomoSharedCred{}, err
		}
		cipher, err := requireString(params, "cipher")
		if err != nil {
			return MihomoSharedCred{}, err
		}
		return MihomoSharedCred{Password: pw, Cipher: cipher}, nil

	default:
		return MihomoSharedCred{}, fmt.Errorf("%w: %q", ErrProtocolNotSupported, protocol)
	}
}

func requireString(params map[string]any, key string) (string, error) {
	v, ok := params[key]
	if !ok {
		return "", fmt.Errorf("mihomo: param %q required", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("mihomo: param %q must be string, got %T", key, v)
	}
	if s == "" {
		return "", fmt.Errorf("mihomo: param %q must not be empty", key)
	}
	return s, nil
}

// requirePort parses a numeric port from a map, accepting the common shapes
// that flow in from decoders and typed callers:
//   - int / int64 — literal yaml / config structs
//   - float64 — encoding/json default for JSON numbers
//   - int32 — proto-generated structs (FastAddInboundReq.Port is int32)
//   - uint / uint16 / uint32 — typed callers with a port-specific alias
func requirePort(params map[string]any, key string) (uint32, error) {
	v, ok := params[key]
	if !ok {
		return 0, fmt.Errorf("mihomo: param %q required", key)
	}
	switch vv := v.(type) {
	case uint32:
		if vv > 65535 {
			return 0, fmt.Errorf("mihomo: param %q out of range [0,65535]: %d", key, vv)
		}
		return vv, nil
	case uint16:
		return uint32(vv), nil
	case uint:
		if vv > 65535 {
			return 0, fmt.Errorf("mihomo: param %q out of range [0,65535]: %d", key, vv)
		}
		return uint32(vv), nil
	case int:
		if vv < 0 || vv > 65535 {
			return 0, fmt.Errorf("mihomo: param %q out of range [0,65535]: %d", key, vv)
		}
		return uint32(vv), nil
	case int32:
		if vv < 0 || vv > 65535 {
			return 0, fmt.Errorf("mihomo: param %q out of range [0,65535]: %d", key, vv)
		}
		return uint32(vv), nil
	case int64:
		if vv < 0 || vv > 65535 {
			return 0, fmt.Errorf("mihomo: param %q out of range [0,65535]: %d", key, vv)
		}
		return uint32(vv), nil
	case float64:
		if vv != float64(uint32(vv)) || vv < 0 || vv > 65535 {
			return 0, fmt.Errorf("mihomo: param %q must be integer in [0,65535]: %v", key, vv)
		}
		return uint32(vv), nil
	default:
		return 0, fmt.Errorf("mihomo: param %q must be numeric, got %T", key, v)
	}
}

// isLoopbackListen tells whether an address string binds only to the local
// interface. Covers the common forms; more exotic forms (e.g. 127.0.0.2)
// are flagged as non-loopback by omission, which is the conservative
// default for the listen_addr warning.
func isLoopbackListen(addr string) bool {
	switch addr {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

// fastAddBuildInbound dispatches on protocol: Phase 1+ go via
// protocolparams.Parse → FromProtocolParams; unmigrated protocols stay on
// FromParams + SharedCred. Collapses to a single Parse call once every
// phase has landed.
func fastAddBuildInbound(tag string, params map[string]any) (*MihomoInbound, error) {
	protoStr, _ := params["protocol"].(string)
	switch contracts.Protocol(protoStr) {
	case contracts.ProtocolVLess, contracts.ProtocolVMess, contracts.ProtocolTrojan:
		// Parse reads KeyTag from params if present; we overwrite with
		// the canonical FastAdd tag afterwards — that's the authoritative
		// identifier, and it preserves Parse's read-only contract on
		// the raw map (no write-before-parse).
		pp, err := protocolparams.Parse(params)
		if err != nil {
			return nil, err
		}
		pp.Tag = tag
		return FromProtocolParams(pp)
	default:
		return FromParams(tag, params)
	}
}

// FromProtocolParams is the Phase 1+ entry point: it consumes a typed
// *ProtocolParams produced by protocolparams.Parse(raw) and returns a
// ready-to-use MihomoInbound.
//
// Caller contract:
//   - pp.Tag, pp.Port, pp.Protocol must already be set (Parse guarantees this).
//   - pp.ListenAddr may be empty — NewMihomoInboundFromProtocolParams
//     substitutes "127.0.0.1".
//   - Per-protocol sub-spec (pp.VLESS, pp.Hysteria2, …) must be populated for
//     the declared Protocol. Validate catches mismatched states.
//
// Non-loopback listen_addr is warn-logged, matching FromParams. Plan
// constraint: the forward layer is the only sanctioned external ingress.
func FromProtocolParams(pp *protocolparams.ProtocolParams) (*MihomoInbound, error) {
	if pp == nil {
		return nil, fmt.Errorf("mihomo: nil ProtocolParams")
	}
	if pp.Tag == "" {
		return nil, fmt.Errorf("mihomo: tag required")
	}
	inb := NewMihomoInboundFromProtocolParams(pp)

	if pp.ListenAddr != "" && !isLoopbackListen(pp.ListenAddr) {
		log.Warnf("mihomo: inbound %q listen_addr=%q is non-loopback; forward layer is the sanctioned external ingress path (see docs/container-design-principles.md principle 3)", pp.Tag, pp.ListenAddr)
	}

	if err := inb.Validate(); err != nil {
		return nil, err
	}
	return inb, nil
}
