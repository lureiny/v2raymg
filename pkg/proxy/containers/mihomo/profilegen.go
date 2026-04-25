package mihomo

import (
	"fmt"
	"strconv"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
)

// BuildListener converts a MihomoInbound into a map[string]any suitable as
// one entry of mihomo's top-level listeners[] yaml array.
//
// Keys use mihomo Alpha's `inbound:"..."` struct tag values verbatim (see
// listener/inbound/{base,vmess,trojan,shadowsocks}.go). mihomo's parser
// (listener/parse.go:ParseListener) decodes the map with
// structure.NewDecoder(TagName:"inbound"), so any other casing (camelCase
// struct field names, yaml snake_case, etc.) would be silently dropped by
// DefaultKeyReplacer. Keep the literal tag strings as the single source of
// truth here.
//
// The generated entry assumes the shared-credential model: one listener
// per inbound, one credential shared by all users. User ingress is handled
// by the forward layer and never touches this yaml.
func BuildListener(inb *MihomoInbound) (map[string]any, error) {
	if err := inb.Validate(); err != nil {
		return nil, err
	}

	m := map[string]any{
		// name: listener identifier; also the key used by mihomo's
		// PatchInboundListeners name-keyed diff. Stable across v2raymg
		// inbound lifetime.
		"name": inb.Tag(),
		// type: dispatch key for mihomo's ParseListener switch. Values are
		// the literal "vmess" / "trojan" / "shadowsocks" strings, which
		// happen to match our contracts.Protocol constants (verified against
		// listener/parse.go).
		"type":   string(inb.Protocol()),
		"listen": inb.ListenAddr(),
		// port: stringified because mihomo's BaseOption.Port is a string tag
		// that supports ranges ("1000-2000"). For a single port the decimal
		// form is what the decoder expects.
		"port": strconv.FormatUint(uint64(inb.Port()), 10),
	}

	switch inb.Protocol() {
	case contracts.ProtocolVLess:
		if err := fillVLESSListener(m, inb); err != nil {
			return nil, err
		}
	case contracts.ProtocolVMess:
		if err := fillVMessListener(m, inb); err != nil {
			return nil, err
		}
	case contracts.ProtocolTrojan:
		if err := fillTrojanListener(m, inb); err != nil {
			return nil, err
		}
	case contracts.ProtocolShadowsocks:
		// shadowsocks has no users array; credentials are top-level
		// password/cipher on ShadowSocksOption.
		m["password"] = inb.SharedCred.Password
		m["cipher"] = inb.SharedCred.Cipher
	default:
		// Unreachable under correctly-validated inbound, but keeps the
		// switch exhaustive in the face of future protocol additions
		// forgetting to update profilegen.
		return nil, fmt.Errorf("%w: %q", ErrProtocolNotSupported, inb.Protocol())
	}
	return m, nil
}

// fillTrojanListener maps Trojan ProtocolParams onto mihomo Alpha listener
// yaml. The legacy SharedCred branch preserves pre-Phase-3 records exactly.
func fillTrojanListener(m map[string]any, inb *MihomoInbound) error {
	if inb.ProtocolParams == nil || inb.ProtocolParams.Trojan == nil {
		m["users"] = []map[string]any{
			{"password": inb.SharedCred.Password},
		}
		if inb.SharedCred.CertFile != "" && inb.SharedCred.KeyFile != "" {
			m["certificate"] = inb.SharedCred.CertFile
			m["private-key"] = inb.SharedCred.KeyFile
		}
		return nil
	}

	trojan := inb.ProtocolParams.Trojan
	m["users"] = []map[string]any{
		{"password": trojan.Password},
	}

	if t := inb.ProtocolParams.Transport; t != nil {
		switch t.Kind {
		case contracts.TransportTCP, "":
			// default; emit nothing
		case contracts.TransportWS:
			if t.WSPath != "" {
				m["ws-path"] = t.WSPath
			}
		case contracts.TransportGRPC:
			if t.GRPCServiceName != "" {
				m["grpc-service-name"] = t.GRPCServiceName
			}
		default:
			return fmt.Errorf("%w: trojan transport %q not wired in mihomo profilegen",
				ErrProtocolNotSupported, t.Kind)
		}
	}

	if s := inb.ProtocolParams.Security; s != nil {
		switch s.Kind {
		case contracts.SecurityTLS:
			if s.TLS != nil && s.TLS.CertFile != "" && s.TLS.KeyFile != "" {
				m["certificate"] = s.TLS.CertFile
				m["private-key"] = s.TLS.KeyFile
			}
		case contracts.SecurityReality:
			if rc := s.Reality; rc != nil {
				m["reality-config"] = buildRealityConfig(rc)
			}
		default:
			return fmt.Errorf("%w: trojan security %q not supported", ErrProtocolNotSupported, s.Kind)
		}
	}
	return nil
}

// fillVLESSListener maps the VLESS protocol params onto the mihomo Alpha
// listener yaml. mihomo's VlessOption has no explicit `network` field —
// the transport is inferred from which of {WsPath, GrpcServiceName,
// XHTTPConfig} is non-empty. We therefore populate exactly one transport
// group per call, keyed off TransportSpec.Kind.
//
// Sources (mihomo Alpha):
//   - listener/inbound/vless.go:VlessOption — `users`, `decryption`,
//     `ws-path`, `grpc-service-name`, `xhttp-config`, `certificate`,
//     `private-key`, `reality-config`
//   - listener/inbound/vless.go:VlessUser — `username`, `uuid`, `flow`
//   - listener/inbound/reality.go:RealityConfig — `dest` (not "target"),
//     `private-key`, `server-names`, `short-id`, `max-time-difference`, `proxy`
func fillVLESSListener(m map[string]any, inb *MihomoInbound) error {
	if inb.ProtocolParams == nil || inb.ProtocolParams.VLESS == nil {
		return fmt.Errorf("%w: vless inbound missing ProtocolParams.VLESS", ErrMissingCredential)
	}
	v := inb.ProtocolParams.VLESS

	user := map[string]any{"uuid": v.UUID}
	if v.Flow != "" {
		user["flow"] = v.Flow
	}
	m["users"] = []map[string]any{user}

	// decryption: parseVLESS guarantees this is at least "none", so the
	// if-check is defensive for hand-constructed *ProtocolParams (unit
	// tests skipping Parse). When empty we just let mihomo default.
	if v.Decryption != "" {
		m["decryption"] = v.Decryption
	}

	// Transport: mihomo Alpha infers network from which transport field
	// is populated; exactly one is written.
	if t := inb.ProtocolParams.Transport; t != nil {
		switch t.Kind {
		case contracts.TransportTCP, "":
			// Nothing to set; tcp is the default when no transport field
			// is present.
		case contracts.TransportWS:
			if t.WSPath != "" {
				m["ws-path"] = t.WSPath
			}
		case contracts.TransportGRPC:
			if t.GRPCServiceName != "" {
				m["grpc-service-name"] = t.GRPCServiceName
			}
		case contracts.TransportXHTTP, contracts.TransportSplitHTTP:
			xh := map[string]any{}
			if t.XHTTPPath != "" {
				xh["path"] = t.XHTTPPath
			}
			if t.XHTTPHost != "" {
				xh["host"] = t.XHTTPHost
			}
			if t.XHTTPMode != "" {
				xh["mode"] = t.XHTTPMode
			}
			if len(xh) > 0 {
				m["xhttp-config"] = xh
			}
		default:
			// parseVLESS pre-rejects httpupgrade and h2, so this branch
			// only fires on newly-added contracts.Transport constants
			// that forgot to wire in the mihomo emit path.
			return fmt.Errorf("%w: vless transport %q not wired in mihomo profilegen",
				ErrProtocolNotSupported, t.Kind)
		}
	}

	// Security: TLS → certificate/private-key; Reality → reality-config.
	if s := inb.ProtocolParams.Security; s != nil {
		switch s.Kind {
		case contracts.SecurityTLS:
			if s.TLS != nil && s.TLS.CertFile != "" && s.TLS.KeyFile != "" {
				m["certificate"] = s.TLS.CertFile
				m["private-key"] = s.TLS.KeyFile
			}
		case contracts.SecurityReality:
			if rc := s.Reality; rc != nil {
				m["reality-config"] = buildRealityConfig(rc)
			}
		case contracts.SecurityNone, "":
			// plain; nothing to write
		default:
			return fmt.Errorf("%w: vless security %q not supported", ErrProtocolNotSupported, s.Kind)
		}
	}
	return nil
}

// fillVMessListener maps the VMess protocol params onto the mihomo Alpha
// listener yaml. mihomo's VmessOption has fields for ws-path,
// grpc-service-name, certificate/private-key, and reality-config —
// nothing for httpupgrade / xhttp / h2 (parseVMess already rejects
// those). The SharedCred branch (ProtocolParams=nil) preserves the
// exact pre-Phase-2 output so legacy records reload byte-identical.
//
// Sources (mihomo Alpha):
//   - listener/inbound/vmess.go:VmessOption — `users`, `ws-path`,
//     `grpc-service-name`, `certificate`, `private-key`, `reality-config`
//   - listener/inbound/vmess.go:VmessUser — `username`, `uuid`, `alterId`
func fillVMessListener(m map[string]any, inb *MihomoInbound) error {
	// Legacy SharedCred path — pre-Phase-2 records. Emit the exact
	// shape the prior profilegen did so byte-level reload diffs stay
	// clean.
	if inb.ProtocolParams == nil || inb.ProtocolParams.VMess == nil {
		m["users"] = []map[string]any{
			{
				"uuid":    inb.SharedCred.UUID,
				"alterId": 0,
			},
		}
		return nil
	}

	v := inb.ProtocolParams.VMess

	// alterId is written unconditionally (including 0) to match the
	// existing byte-level output — mihomo's structure decoder treats
	// a missing field and a zero value identically, but emitting it
	// keeps diffs against the legacy path clean.
	m["users"] = []map[string]any{
		{
			"uuid":    v.UUID,
			"alterId": v.AlterID,
		},
	}

	// Transport: mihomo VMess has no explicit network field; exactly
	// one of ws-path / grpc-service-name is written. TCP emits nothing.
	if t := inb.ProtocolParams.Transport; t != nil {
		switch t.Kind {
		case contracts.TransportTCP, "":
			// default; emit nothing
		case contracts.TransportWS:
			if t.WSPath != "" {
				m["ws-path"] = t.WSPath
			}
		case contracts.TransportGRPC:
			if t.GRPCServiceName != "" {
				m["grpc-service-name"] = t.GRPCServiceName
			}
		default:
			// parseVMess pre-rejects httpupgrade/xhttp/splithttp/h2,
			// so this only fires on newly-added transport constants.
			return fmt.Errorf("%w: vmess transport %q not wired in mihomo profilegen",
				ErrProtocolNotSupported, t.Kind)
		}
	}

	// Security: TLS → certificate/private-key; Reality → reality-config.
	if s := inb.ProtocolParams.Security; s != nil {
		switch s.Kind {
		case contracts.SecurityTLS:
			if s.TLS != nil && s.TLS.CertFile != "" && s.TLS.KeyFile != "" {
				m["certificate"] = s.TLS.CertFile
				m["private-key"] = s.TLS.KeyFile
			}
		case contracts.SecurityReality:
			if rc := s.Reality; rc != nil {
				m["reality-config"] = buildRealityConfig(rc)
			}
		case contracts.SecurityNone, "":
			// plain; nothing to write
		default:
			return fmt.Errorf("%w: vmess security %q not supported", ErrProtocolNotSupported, s.Kind)
		}
	}
	return nil
}

// buildRealityConfig translates a RealitySpec into mihomo's `reality-config`
// map entry. Key names mirror mihomo Alpha's RealityConfig struct tags
// verbatim ("dest", "server-names", "short-id", "private-key",
// "max-time-difference").
func buildRealityConfig(rc *protocolparams.RealitySpec) map[string]any {
	out := map[string]any{
		"dest": rc.Target,
	}
	if len(rc.ServerNames) > 0 {
		out["server-names"] = rc.ServerNames
	}
	if len(rc.ShortIDs) > 0 {
		out["short-id"] = rc.ShortIDs
	}
	if rc.PrivateKey != "" {
		out["private-key"] = rc.PrivateKey
	}
	// MaxTimeDiff is a string on RealitySpec (wire format) but mihomo
	// expects an int. parseReality rejects unparseable values, so by the
	// time we reach here a non-empty string is guaranteed numeric.
	if rc.MaxTimeDiff != "" {
		if n, err := strconv.Atoi(rc.MaxTimeDiff); err == nil {
			out["max-time-difference"] = n
		}
	}
	return out
}
