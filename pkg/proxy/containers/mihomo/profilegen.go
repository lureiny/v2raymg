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
		if err := fillSSListener(m, inb); err != nil {
			return nil, err
		}
	case contracts.ProtocolHysteria2:
		if err := fillHysteria2Listener(m, inb); err != nil {
			return nil, err
		}
	case contracts.ProtocolTUIC:
		if err := fillTuicListener(m, inb); err != nil {
			return nil, err
		}
	case contracts.ProtocolAnyTLS:
		if err := fillAnyTLSListener(m, inb); err != nil {
			return nil, err
		}
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

// fillSSListener maps Shadowsocks ProtocolParams onto mihomo Alpha listener
// yaml. The legacy SharedCred path preserves pre-Phase-4 records exactly.
//
// Plugin note: the `plugin` and `plugin-opts` fields are emitted when
// ProtocolParams.SS.Plugin is set. obfs-local and v2ray-plugin are supported
// as server-side listener plugins by mihomo Alpha. shadow-tls is a separate
// network proxy; its plugin info is stored for subscription (client config)
// generation only — the listener itself runs plain SS.
func fillSSListener(m map[string]any, inb *MihomoInbound) error {
	if inb.ProtocolParams == nil || inb.ProtocolParams.SS == nil {
		// Legacy SharedCred path: plain password + cipher only.
		m["password"] = inb.SharedCred.Password
		m["cipher"] = inb.SharedCred.Cipher
		return nil
	}

	ss := inb.ProtocolParams.SS
	m["password"] = ss.Password
	m["cipher"] = ss.Cipher
	if ss.UDP {
		m["udp"] = true
	}

	// shadow-tls is a separate proxy layer (not a mihomo listener plugin);
	// skip emitting plugin for shadow-tls so the mihomo SS listener stays plain.
	if ss.Plugin != "" && ss.Plugin != "shadow-tls" {
		m["plugin"] = ss.Plugin
		if len(ss.PluginOpts) > 0 {
			opts := make(map[string]any, len(ss.PluginOpts))
			for k, v := range ss.PluginOpts {
				if k == "tls" {
					opts[k] = v == "true"
				} else if k == "version" {
					// mihomo expects an integer for the version field.
					if n, err := strconv.Atoi(v); err == nil {
						opts[k] = n
					} else {
						opts[k] = v
					}
				} else {
					opts[k] = v
				}
			}
			m["plugin-opts"] = opts
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

// fillHysteria2Listener maps Hysteria2 ProtocolParams onto mihomo Alpha
// listener yaml. Hysteria2 has no legacy SharedCred path — Phase 5 is its
// first appearance in this container.
//
// Sources (mihomo Alpha listener/inbound/hysteria2.go:Hysteria2Option):
//   - users: map[string]string (username → password). Single-user case
//     uses "default" as the username. Future per-user expansion will
//     extend this map; today the user manager handles per-user routing in
//     the forward layer instead.
//   - obfs / obfs-password: only "salamander" supported by mihomo Alpha.
//   - certificate / private-key: absolute PEM paths (not cert-file/key-file).
//     These differ from the cert_file / key_file keys carried on
//     ProtocolParams.TLSSpec — the mapping happens here.
//   - alpn: defaults to ["h3"] when caller didn't specify (Hysteria2 is
//     QUIC, h3 is the only meaningful ALPN).
//   - up / down: bandwidth advertised to clients; "50 Mbps" form.
//   - ignore-client-bandwidth: independent of up/down (mihomo allows both
//     to be set; runtime picks).
//   - masquerade: server-side decoy URL/proxy/file — silently dropped on
//     the client side by convertHysteria2 since the client schema has no
//     equivalent.
func fillHysteria2Listener(m map[string]any, inb *MihomoInbound) error {
	if inb.ProtocolParams == nil || inb.ProtocolParams.Hysteria2 == nil {
		return fmt.Errorf("%w: hysteria2 inbound missing ProtocolParams.Hysteria2", ErrMissingCredential)
	}
	hy2 := inb.ProtocolParams.Hysteria2

	m["users"] = map[string]any{
		"default": hy2.Password,
	}

	sec := inb.ProtocolParams.Security
	if sec == nil || sec.Kind != contracts.SecurityTLS || sec.TLS == nil {
		return fmt.Errorf("%w: hysteria2 requires tls security", ErrMissingCredential)
	}
	if sec.TLS.CertFile == "" || sec.TLS.KeyFile == "" {
		return fmt.Errorf("%w: hysteria2 requires cert_file and key_file", ErrMissingCredential)
	}
	m["certificate"] = sec.TLS.CertFile
	m["private-key"] = sec.TLS.KeyFile

	alpn := sec.TLS.ALPN
	if len(alpn) == 0 {
		alpn = []string{"h3"}
	}
	m["alpn"] = alpn

	if hy2.Obfs != "" {
		m["obfs"] = hy2.Obfs
		if hy2.ObfsPassword != "" {
			m["obfs-password"] = hy2.ObfsPassword
		}
	}
	if hy2.Up != "" {
		m["up"] = hy2.Up
	}
	if hy2.Down != "" {
		m["down"] = hy2.Down
	}
	if hy2.IgnoreClientBandwidth {
		m["ignore-client-bandwidth"] = true
	}
	if hy2.Masquerade != "" {
		m["masquerade"] = hy2.Masquerade
	}
	return nil
}

// fillTuicListener maps TUIC ProtocolParams onto mihomo Alpha listener yaml
// (v5 only — v2raymg targets v5 for per-user attribution; v4 token-based
// auth is intentionally unsupported).
//
// Sources (mihomo Alpha listener/inbound/tuic.go::TuicOption + listener/parse.go):
//   - users: map[uuid]password. v4 `token: []string` is mutually exclusive
//     with users at the packet-overhead layer (server.go:109-112), so this
//     function NEVER writes a `token:` key.
//   - certificate / private-key: absolute PEM file paths (loaded via
//     ca.NewTLSKeyPairLoader in listener/tuic/server.go:56). The mihomo key
//     names are NOT cert-file/key-file — beware.
//   - alpn: defaults to ["h3"] when caller didn't specify. Listener forces
//     ["h3"] when the field is empty (server.go:83-87); we set the default
//     explicitly so the emitted yaml matches the runtime behaviour.
//   - congestion-controller: mihomo's parse-time default is "bbr" (injected
//     by listener/parse.go:116 BEFORE the inbound struct decodes). v2raymg
//     does not run through ParseListener — we emit the listener config as a
//     yaml fragment for the top-level config — so we MUST write "bbr"
//     explicitly when the caller didn't pick one. Skipping this would leave
//     the field empty and mihomo would call common.SetCongestionController("")
//     with undefined behaviour.
//
// Listener-side fields not exposed by v2raymg (left at mihomo defaults):
//
//   - max-idle-time / authentication-timeout / cwnd / max-udp-relay-packet-size
//     / bbr-profile / mux-option / client-auth-* / ech-key
//
// Allow0RTT is unconditionally true at the listener (server.go:101) and
// has no listener yaml key — `zero_rtt_handshake` rides exclusively on
// the subscription path to surface as client `reduce-rtt`.
//
// HeartbeatInterval / UDPRelayMode are mihomo client-only fields and are
// never written here. They reach the client via subscription Extensions.
func fillTuicListener(m map[string]any, inb *MihomoInbound) error {
	if inb.ProtocolParams == nil || inb.ProtocolParams.TUIC == nil {
		return fmt.Errorf("%w: tuic inbound missing ProtocolParams.TUIC", ErrMissingCredential)
	}
	t := inb.ProtocolParams.TUIC

	// v5 single-user mode: one inbound carries one (uuid, password); v2raymg
	// distinguishes users at the forward layer. Multi-user expansion is the
	// pending user-tracker refactor (see docs/inbound-user-tracker-refactor.md).
	m["users"] = map[string]any{
		t.UUID: t.Password,
	}

	sec := inb.ProtocolParams.Security
	if sec == nil || sec.Kind != contracts.SecurityTLS || sec.TLS == nil {
		return fmt.Errorf("%w: tuic requires tls security", ErrMissingCredential)
	}
	if sec.TLS.CertFile == "" || sec.TLS.KeyFile == "" {
		return fmt.Errorf("%w: tuic requires cert_file and key_file", ErrMissingCredential)
	}
	m["certificate"] = sec.TLS.CertFile
	m["private-key"] = sec.TLS.KeyFile

	alpn := sec.TLS.ALPN
	if len(alpn) == 0 {
		alpn = []string{"h3"}
	}
	m["alpn"] = alpn

	cc := t.CongestionController
	if cc == "" {
		// See doc above — mihomo's own default is parse-time only and we
		// don't go through ParseListener. Mirror it explicitly.
		cc = "bbr"
	}
	m["congestion-controller"] = cc

	return nil
}

// fillAnyTLSListener maps AnyTLS ProtocolParams onto mihomo Alpha listener
// yaml. AnyTLS has no legacy SharedCred path — Phase 7 is its first
// appearance in this container.
//
// Sources (mihomo Alpha listener/inbound/anytls.go::AnyTLSOption +
// listener/anytls/server.go):
//
//   - users: map[string]string. Single-user model uses "default" as the
//     username, mirroring fillHysteria2Listener. mihomo matches incoming
//     connections by sha256(password) so the username is purely a label
//     for log/observability; per-user routing is handled at the forward
//     layer like every other Phase 1+ protocol.
//   - certificate / private-key: absolute PEM file paths, kebab-case key
//     names (NOT cert-file/key-file). Server.go:116 hard-fails without
//     a certificate, so empty paths must be rejected by Validate before
//     reaching this point.
//   - padding-scheme: optional inline text. Empty defers to mihomo's
//     transport/anytls/padding.DefaultPaddingScheme; we never inject a
//     placeholder default ourselves so an explicit empty caller intent
//     and "use mihomo default" are indistinguishable on the wire (which
//     is the desired behaviour — both produce the same listener).
//
// Listener-side fields not exposed by v2raymg (left at mihomo defaults
// or unsupported):
//
//   - client-auth-type / client-auth-cert (mTLS) / ech-key (ECH)
//   - alpn (no listener key — anytls server lets Go TLS auto-negotiate)
//
// idle-session-check-interval / idle-session-timeout / min-idle-session
// are mihomo client-only outbound fields and are never written here. They
// reach the client via subscription Extensions.
func fillAnyTLSListener(m map[string]any, inb *MihomoInbound) error {
	if inb.ProtocolParams == nil || inb.ProtocolParams.AnyTLS == nil {
		return fmt.Errorf("%w: anytls inbound missing ProtocolParams.AnyTLS", ErrMissingCredential)
	}
	a := inb.ProtocolParams.AnyTLS

	m["users"] = map[string]any{
		"default": a.Password,
	}

	sec := inb.ProtocolParams.Security
	if sec == nil || sec.Kind != contracts.SecurityTLS || sec.TLS == nil {
		return fmt.Errorf("%w: anytls requires tls security", ErrMissingCredential)
	}
	if sec.TLS.CertFile == "" || sec.TLS.KeyFile == "" {
		return fmt.Errorf("%w: anytls requires cert_file and key_file", ErrMissingCredential)
	}
	m["certificate"] = sec.TLS.CertFile
	m["private-key"] = sec.TLS.KeyFile

	if a.PaddingScheme != "" {
		m["padding-scheme"] = a.PaddingScheme
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
