package mihomo

import (
	"errors"
	"fmt"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

// defaultSSMethod is used when a shadowsocks inbound was constructed without
// an explicit cipher. Must track the clash converter's fallback
// (pkg/proxy/core/subscription/converter/clash.go convertShadowsocks) so
// the generated URI and any Clash rendering derived from it agree on the
// cipher the client should use. If one side changes the default, update
// both.
const defaultSSMethod = "2022-blake3-aes-256-gcm"

// GetUserSubscriptions returns one SubscriptionSpec per (request user, inbound)
// pair where the user has a live forward port mapping on this node.
//
// Shape of the result:
//
//   - One entry per mihomo inbound that already has a forward rule for the
//     requested user. Inbounds without a mapping are silently skipped — a
//     user may exist on this node (in UserManager) yet not be wired to a
//     particular inbound (race with FastAddInbound, group change, etc).
//     The next reconcile tick converges, but subscriptions reflect current
//     forward state rather than the "post-reconcile" state.
//
//   - Each spec carries the public Port (from forwardMgr.GetUserPortByDst),
//     the shared credential (UUID / Password / Password+Cipher depending on
//     protocol), and a URI encoded via the codec package. Extensions are
//     populated so the clash converter can re-render the entry without
//     re-parsing the URI.
//
// Returns (nil, nil) when the container has no user manager wired (unit
// tests that only exercise the inbound CRUD path); the HTTP /sub endpoint
// treats an empty slice as "this container produced nothing for this user",
// not as an error.
//
// Supported protocols today: vless / vmess / trojan / shadowsocks (all via
// Phase 1–4 ProtocolParams path, with legacy SharedCred fallback for
// pre-migration records). Unsupported protocols are logged and skipped
// rather than surfaced as hard errors — a mihomo container should never
// hold such an inbound (adapter rejects them upfront), but a corrupt-record
// path could leak through; graceful skip avoids breaking subscriptions for
// healthy inbounds on the same container.
func (c *MihomoContainer) GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	if req.User.Username == "" {
		return nil, fmt.Errorf("mihomo: subscription request missing username")
	}
	if req.Host == "" {
		return nil, fmt.Errorf("mihomo: subscription request missing host")
	}
	if c.userMgr == nil {
		return nil, nil
	}

	inbounds := c.snapshotInbounds()
	specs := make([]contracts.SubscriptionSpec, 0, len(inbounds))

	for _, inb := range inbounds {
		port, ok := c.userMgr.GetUserPortByDst(req.User.Username, inb.Port())
		if !ok {
			// No forward rule — user not wired to this inbound yet (or at
			// all). Normal during race with FastAddInbound / group change;
			// reconcileDrift converges within reconcileInterval. Log at
			// debug so operators can correlate "subscription missing an
			// inbound I expected" without spamming info/warn.
			log.Debugf("mihomo: subscription skip inbound %q for user %q (no forward port mapping)", inb.Tag(), req.User.Username)
			continue
		}
		spec, err := buildSubscriptionSpec(inb, req, port)
		if err != nil {
			if errors.Is(err, ErrProtocolNotSupported) {
				log.Warnf("mihomo: skip inbound %q with unsupported protocol in subscription: %v", inb.Tag(), err)
				continue
			}
			return nil, fmt.Errorf("mihomo: subscription for inbound %q: %w", inb.Tag(), err)
		}
		specs = append(specs, spec)
	}

	return specs, nil
}

// buildSubscriptionSpec constructs a SubscriptionSpec + URI for a single
// (inbound, user, allocated forward port) triple. The URI is produced via
// codec.Encode so the wire format matches what core/subscription/codec
// Decode would emit back — important because downstream converters
// (clash / surge) re-decode URIs when Extensions is absent.
//
// Per-protocol dispatch (current state):
//
//	vless → fillVLESSSubscriptionSpec (Phase 1, ProtocolParams only)
//	vmess → fillVMessSubscriptionSpec (Phase 2, ProtocolParams preferred + SharedCred legacy fallback)
//	trojan → fillTrojanSubscriptionSpec (Phase 3, ProtocolParams preferred + SharedCred legacy fallback)
//	shadowsocks → fillSSSubscriptionSpec (Phase 4, ProtocolParams preferred + SharedCred legacy fallback)
//	hysteria2 → fillHysteria2SubscriptionSpec (Phase 5, ProtocolParams only)
//	tuic → fillTuicSubscriptionSpec (Phase 6, ProtocolParams only)
//	anytls → fillAnyTLSSubscriptionSpec (Phase 7, ProtocolParams only)
func buildSubscriptionSpec(inb *MihomoInbound, req contracts.SubscriptionRequest, port uint32) (contracts.SubscriptionSpec, error) {
	nodeName := req.NodeName
	if nodeName == "" {
		nodeName = inb.Tag()
	}

	spec := contracts.SubscriptionSpec{
		Host:       req.Host,
		Port:       port,
		NodeName:   nodeName,
		InboundTag: inb.Tag(),
		Username:   req.User.Username,
		Extensions: map[string]any{},
	}

	switch inb.Protocol() {
	case contracts.ProtocolVLess:
		if err := fillVLESSSubscriptionSpec(&spec, inb); err != nil {
			return contracts.SubscriptionSpec{}, err
		}

	case contracts.ProtocolVMess:
		if err := fillVMessSubscriptionSpec(&spec, inb); err != nil {
			return contracts.SubscriptionSpec{}, err
		}

	case contracts.ProtocolTrojan:
		if err := fillTrojanSubscriptionSpec(&spec, inb); err != nil {
			return contracts.SubscriptionSpec{}, err
		}

	case contracts.ProtocolShadowsocks:
		if err := fillSSSubscriptionSpec(&spec, inb); err != nil {
			return contracts.SubscriptionSpec{}, err
		}

	case contracts.ProtocolHysteria2:
		if err := fillHysteria2SubscriptionSpec(&spec, inb); err != nil {
			return contracts.SubscriptionSpec{}, err
		}

	case contracts.ProtocolTUIC:
		if err := fillTuicSubscriptionSpec(&spec, inb); err != nil {
			return contracts.SubscriptionSpec{}, err
		}

	case contracts.ProtocolAnyTLS:
		if err := fillAnyTLSSubscriptionSpec(&spec, inb); err != nil {
			return contracts.SubscriptionSpec{}, err
		}

	default:
		return contracts.SubscriptionSpec{}, fmt.Errorf("%w: %q", ErrProtocolNotSupported, inb.Protocol())
	}

	return spec, nil
}

// shouldSkipCertVerifyForClient is the canonical gate every fillXxxSubscriptionSpec
// uses to decide whether the generated client config should set skip-cert-verify.
// Two triggers:
//
//   - CertSource == "self_signed" (operator generated a one-shot cert; clients
//     can't validate the chain).
//   - SkipCertVerify explicit override (operator wants to ship a real-CA cert
//     whose chain the client can't or shouldn't validate).
//
// The check used to be inlined at 6 call sites (vless / vmess / trojan / hy2 /
// tuic / anytls); centralising prevents drift if cert_source values ever
// expand (e.g. "self_signed_ecc"). Returns false when sec or sec.TLS is nil
// so callers can treat "no security" the same as "no skip required".
func shouldSkipCertVerifyForClient(sec *protocolparams.SecuritySpec) bool {
	if sec == nil || sec.TLS == nil {
		return false
	}
	return sec.TLS.CertSource == "self_signed" || sec.TLS.SkipCertVerify
}

// fillVLESSSubscriptionSpec projects the inbound's ProtocolParams onto the
// SubscriptionSpec. Every field the clash converter needs is set either on
// the typed spec fields (Protocol / Password) or in Extensions so the
// converter doesn't have to re-parse the URI.
//
// Extensions keys match the ones convertVLess in
// pkg/proxy/core/subscription/converter/clash.go already reads:
//
//	security, flow, sni, transport, ws_path, grpc_service_name,
//	xhttp_path, xhttp_host, xhttp_mode, reality_public_key,
//	reality_short_id, skip_cert_verify
func fillVLESSSubscriptionSpec(spec *contracts.SubscriptionSpec, inb *MihomoInbound) error {
	if inb.ProtocolParams == nil || inb.ProtocolParams.VLESS == nil {
		return fmt.Errorf("%w: vless inbound %q has no ProtocolParams", ErrMissingCredential, inb.Tag())
	}
	v := inb.ProtocolParams.VLESS
	if v.UUID == "" {
		return fmt.Errorf("%w: vless inbound %q has empty uuid", ErrMissingCredential, inb.Tag())
	}
	spec.Protocol = contracts.ProtocolVLess
	spec.Password = v.UUID

	node := &codec.VLessNode{
		NodeName: spec.NodeName,
		Host:     spec.Host,
		Port:     spec.Port,
		UUID:     v.UUID,
		Flow:     v.Flow,
	}
	if v.Flow != "" {
		spec.Extensions["flow"] = v.Flow
	}

	// Security → codec fields + Extensions echo
	if sec := inb.ProtocolParams.Security; sec != nil {
		switch sec.Kind {
		case contracts.SecurityTLS:
			node.Security = "tls"
			spec.Extensions["security"] = "tls"
			if sec.TLS != nil {
				node.SNI = sec.TLS.SNI
				if sec.TLS.SNI != "" {
					// converter/clash.go reads "server_name" (not "sni")
					// — keep the two sides in lockstep.
					spec.Extensions["server_name"] = sec.TLS.SNI
				}
				// skip_cert_verify is set when either:
				//   - the cert was self-signed (client has no way to
				//     reach a matching root), or
				//   - the caller explicitly asked for the override
				//     (e.g. real-CA cert the client cannot validate).
				if shouldSkipCertVerifyForClient(sec) {
					node.SkipCertVerify = true
					spec.Extensions["skip_cert_verify"] = true
				}
			}
		case contracts.SecurityReality:
			node.Security = "reality"
			spec.Extensions["security"] = "reality"
			if rc := sec.Reality; rc != nil {
				// Clients need the x25519 public key (derived from the
				// server private key) plus one of the configured short
				// IDs. If the caller supplied PublicKey we pass it
				// through; otherwise the subscription omits it and the
				// client's own ops must fill in.
				if rc.PublicKey != "" {
					node.RealityPublicKey = rc.PublicKey
					spec.Extensions["reality_public_key"] = rc.PublicKey
				}
				if len(rc.ShortIDs) > 0 {
					node.RealityShortID = rc.ShortIDs[0]
					// converter/clash.go's buildRealityOpts reads the
					// plural "reality_short_ids" and picks the first
					// entry — mirror that shape.
					spec.Extensions["reality_short_ids"] = rc.ShortIDs
				}
				if len(rc.ServerNames) > 0 && node.SNI == "" {
					node.SNI = rc.ServerNames[0]
					spec.Extensions["server_name"] = rc.ServerNames[0]
				}
			}
		}
	}

	// Transport → codec fields + Extensions echo. Mirrors what the clash
	// converter needs to render the Clash proxy entry.
	if t := inb.ProtocolParams.Transport; t != nil {
		switch t.Kind {
		case contracts.TransportWS:
			node.Transport = "ws"
			node.WSPath = t.WSPath
			node.WSHost = t.WSHost
			spec.Extensions["transport"] = "ws"
			if t.WSPath != "" {
				spec.Extensions["ws_path"] = t.WSPath
			}
			if t.WSHost != "" {
				spec.Extensions["ws_host"] = t.WSHost
			}
		case contracts.TransportGRPC:
			node.Transport = "grpc"
			node.GRPCServiceName = t.GRPCServiceName
			spec.Extensions["transport"] = "grpc"
			if t.GRPCServiceName != "" {
				spec.Extensions["grpc_service_name"] = t.GRPCServiceName
			}
		case contracts.TransportXHTTP, contracts.TransportSplitHTTP:
			node.Transport = string(t.Kind)
			node.XHTTPPath = t.XHTTPPath
			node.XHTTPHost = t.XHTTPHost
			node.XHTTPMode = t.XHTTPMode
			spec.Extensions["transport"] = string(t.Kind)
			if t.XHTTPPath != "" {
				spec.Extensions["xhttp_path"] = t.XHTTPPath
			}
			if t.XHTTPHost != "" {
				spec.Extensions["xhttp_host"] = t.XHTTPHost
			}
			if t.XHTTPMode != "" {
				spec.Extensions["xhttp_mode"] = t.XHTTPMode
			}
		case contracts.TransportTCP, "":
			// canonical default; emit nothing
		}
	}

	spec.URI = node.Encode()
	return nil
}

// fillVMessSubscriptionSpec projects a VMess inbound's state onto the
// SubscriptionSpec, preferring ProtocolParams when present and falling
// back to the legacy SharedCred path for pre-Phase-2 records.
//
// Extensions keys are the ones convertVMess in
// pkg/proxy/core/subscription/converter/clash.go reads:
//
//	alter_id, security, server_name, skip_cert_verify, transport,
//	ws_path, ws_host, grpc_service_name, utls_fingerprint,
//	reality_public_key, reality_short_ids
//
// VMess itself has no cipher field on the server or URI side (xray and
// mihomo both omit it); the clash converter hardcodes "auto" on the
// client. We therefore do not emit Extensions["cipher"].
func fillVMessSubscriptionSpec(spec *contracts.SubscriptionSpec, inb *MihomoInbound) error {
	spec.Protocol = contracts.ProtocolVMess

	// Legacy SharedCred path — covers pre-Phase-2 records and any
	// adapter caller that still constructs MihomoInbound via
	// NewMihomoInbound without ProtocolParams.
	if inb.ProtocolParams == nil || inb.ProtocolParams.VMess == nil {
		if inb.SharedCred.UUID == "" {
			return fmt.Errorf("%w: vmess inbound %q has empty uuid", ErrMissingCredential, inb.Tag())
		}
		spec.Password = inb.SharedCred.UUID
		node := &codec.VMessNode{
			NodeName: spec.NodeName,
			Host:     spec.Host,
			Port:     spec.Port,
			UUID:     inb.SharedCred.UUID,
		}
		spec.URI = node.Encode()
		return nil
	}

	v := inb.ProtocolParams.VMess
	if v.UUID == "" {
		return fmt.Errorf("%w: vmess inbound %q has empty uuid", ErrMissingCredential, inb.Tag())
	}
	spec.Password = v.UUID

	node := &codec.VMessNode{
		NodeName: spec.NodeName,
		Host:     spec.Host,
		Port:     spec.Port,
		UUID:     v.UUID,
		AlterId:  v.AlterID,
	}
	// alter_id round-trip: clash/surge converters don't decode the URI
	// "aid" field — they only read spec + Extensions. Emit unconditionally
	// (including 0) so AlterID > 0 callers don't get silently downgraded
	// to AEAD-only on the client side.
	spec.Extensions["alter_id"] = v.AlterID

	// Security → codec fields + Extensions echo.
	if sec := inb.ProtocolParams.Security; sec != nil {
		switch sec.Kind {
		case contracts.SecurityTLS:
			node.Security = "tls"
			spec.Extensions["security"] = "tls"
			if sec.TLS != nil {
				node.SNI = sec.TLS.SNI
				if sec.TLS.SNI != "" {
					// convertVMess reads "server_name" for TLS SNI.
					spec.Extensions["server_name"] = sec.TLS.SNI
				}
				if shouldSkipCertVerifyForClient(sec) {
					node.SkipCertVerify = true
					spec.Extensions["skip_cert_verify"] = true
				}
				if sec.TLS.UTLSFingerprint != "" {
					node.Fingerprint = sec.TLS.UTLSFingerprint
					spec.Extensions["utls_fingerprint"] = sec.TLS.UTLSFingerprint
				}
			}
		case contracts.SecurityReality:
			// mihomo VMess accepts reality-config on the listener side.
			// The codec VMessNode has no reality fields, so the URI
			// won't carry pbk/sid; Extensions does the work for the
			// clash converter. This matches how fillVLESSSubscriptionSpec
			// populates Extensions["reality_public_key"] etc.
			spec.Extensions["security"] = "reality"
			if rc := sec.Reality; rc != nil {
				if rc.PublicKey != "" {
					spec.Extensions["reality_public_key"] = rc.PublicKey
				}
				if len(rc.ShortIDs) > 0 {
					spec.Extensions["reality_short_ids"] = rc.ShortIDs
				}
				if len(rc.ServerNames) > 0 && node.SNI == "" {
					node.SNI = rc.ServerNames[0]
					spec.Extensions["server_name"] = rc.ServerNames[0]
				}
			}
		}
	}

	// Transport → codec fields + Extensions echo.
	if t := inb.ProtocolParams.Transport; t != nil {
		switch t.Kind {
		case contracts.TransportWS:
			node.Transport = "ws"
			node.WSPath = t.WSPath
			node.WSHost = t.WSHost
			spec.Extensions["transport"] = "ws"
			if t.WSPath != "" {
				spec.Extensions["ws_path"] = t.WSPath
			}
			if t.WSHost != "" {
				spec.Extensions["ws_host"] = t.WSHost
			}
		case contracts.TransportGRPC:
			node.Transport = "grpc"
			node.GRPCServiceName = t.GRPCServiceName
			spec.Extensions["transport"] = "grpc"
			if t.GRPCServiceName != "" {
				spec.Extensions["grpc_service_name"] = t.GRPCServiceName
			}
		case contracts.TransportTCP, "":
			// canonical default; emit nothing
		}
	}

	spec.URI = node.Encode()
	return nil
}

// fillTrojanSubscriptionSpec projects a Trojan inbound onto SubscriptionSpec,
// preferring ProtocolParams for Phase 3+ records and preserving the legacy
// SharedCred-only shape for pre-Phase-3 records.
func fillTrojanSubscriptionSpec(spec *contracts.SubscriptionSpec, inb *MihomoInbound) error {
	spec.Protocol = contracts.ProtocolTrojan

	if inb.ProtocolParams == nil || inb.ProtocolParams.Trojan == nil {
		if inb.SharedCred.Password == "" {
			return fmt.Errorf("%w: trojan inbound %q has empty password", ErrMissingCredential, inb.Tag())
		}
		spec.Password = inb.SharedCred.Password
		node := &codec.TrojanNode{
			NodeName: spec.NodeName,
			Host:     spec.Host,
			Port:     spec.Port,
			Password: inb.SharedCred.Password,
		}
		spec.URI = node.Encode()
		return nil
	}

	trojan := inb.ProtocolParams.Trojan
	if trojan.Password == "" {
		return fmt.Errorf("%w: trojan inbound %q has empty password", ErrMissingCredential, inb.Tag())
	}
	spec.Password = trojan.Password
	node := &codec.TrojanNode{
		NodeName: spec.NodeName,
		Host:     spec.Host,
		Port:     spec.Port,
		Password: trojan.Password,
	}

	if sec := inb.ProtocolParams.Security; sec != nil {
		switch sec.Kind {
		case contracts.SecurityTLS:
			if sec.TLS != nil {
				node.SNI = sec.TLS.SNI
				if sec.TLS.SNI != "" {
					spec.Extensions["server_name"] = sec.TLS.SNI
				}
				if shouldSkipCertVerifyForClient(sec) {
					node.SkipCertVerify = true
					spec.Extensions["skip_cert_verify"] = true
				}
				if sec.TLS.UTLSFingerprint != "" {
					node.Fingerprint = sec.TLS.UTLSFingerprint
					spec.Extensions["utls_fingerprint"] = sec.TLS.UTLSFingerprint
				}
			}
			// TLS is implicit in trojan:// URIs and Clash trojan proxies.
			spec.Extensions["security"] = "tls"
		case contracts.SecurityReality:
			node.Security = "reality"
			spec.Extensions["security"] = "reality"
			if rc := sec.Reality; rc != nil {
				if rc.PublicKey != "" {
					node.RealityPublicKey = rc.PublicKey
					spec.Extensions["reality_public_key"] = rc.PublicKey
				}
				if len(rc.ShortIDs) > 0 {
					node.RealityShortID = rc.ShortIDs[0]
					spec.Extensions["reality_short_ids"] = rc.ShortIDs
				}
				if len(rc.ServerNames) > 0 {
					node.SNI = rc.ServerNames[0]
					spec.Extensions["server_name"] = rc.ServerNames[0]
				}
			}
		}
	}

	if t := inb.ProtocolParams.Transport; t != nil {
		switch t.Kind {
		case contracts.TransportWS:
			node.Transport = "ws"
			node.WSPath = t.WSPath
			node.WSHost = t.WSHost
			spec.Extensions["transport"] = "ws"
			if t.WSPath != "" {
				spec.Extensions["ws_path"] = t.WSPath
			}
			if t.WSHost != "" {
				spec.Extensions["ws_host"] = t.WSHost
			}
		case contracts.TransportGRPC:
			node.Transport = "grpc"
			node.GRPCServiceName = t.GRPCServiceName
			spec.Extensions["transport"] = "grpc"
			if t.GRPCServiceName != "" {
				spec.Extensions["grpc_service_name"] = t.GRPCServiceName
			}
		case contracts.TransportTCP, "":
			// canonical default; emit nothing
		}
	}

	spec.URI = node.Encode()
	return nil
}

// fillSSSubscriptionSpec projects the Shadowsocks inbound onto a SubscriptionSpec.
// ProtocolParams path is preferred (Phase 4+); legacy SharedCred path handles
// pre-Phase-4 records that carry no ProtocolParams.
//
// Extensions populated (for the clash converter):
//
//	"method"          — cipher name (converter emits it as the clash proxy cipher)
//	"plugin"          — optional plugin name
//	"plugin_mode"     — plugin opt: obfs mode (obfs-local) or transport mode (v2ray-plugin)
//	"plugin_host"     — plugin opt: obfs/shadow-tls target host
//	"plugin_path"     — plugin opt: v2ray-plugin path
//	"plugin_tls"      — plugin opt: v2ray-plugin TLS (bool)
//	"plugin_password" — plugin opt: shadow-tls password
//	"plugin_version"  — plugin opt: shadow-tls version string
func fillSSSubscriptionSpec(spec *contracts.SubscriptionSpec, inb *MihomoInbound) error {
	if inb.ProtocolParams != nil && inb.ProtocolParams.SS != nil {
		return fillSSSubscriptionSpecPP(spec, inb)
	}
	return fillSSSubscriptionSpecLegacy(spec, inb)
}

func fillSSSubscriptionSpecPP(spec *contracts.SubscriptionSpec, inb *MihomoInbound) error {
	ss := inb.ProtocolParams.SS
	nodeName := spec.NodeName
	if nodeName == "" {
		nodeName = inb.Tag()
	}

	node := &codec.ShadowsocksNode{
		NodeName: nodeName,
		Host:     spec.Host,
		Port:     spec.Port,
		Password: ss.Password,
		Method:   ss.Cipher,
	}
	if ss.Plugin != "" {
		node.Plugin = ss.Plugin
		if len(ss.PluginOpts) > 0 {
			node.PluginOpts = make(map[string]string, len(ss.PluginOpts))
			for k, v := range ss.PluginOpts {
				node.PluginOpts[k] = v
			}
		}
	}

	spec.Protocol = contracts.ProtocolShadowsocks
	spec.Password = ss.Password
	spec.URI = node.Encode()

	spec.Extensions["method"] = ss.Cipher
	spec.Extensions["udp"] = ss.UDP
	if ss.Plugin != "" {
		spec.Extensions["plugin"] = ss.Plugin
		if v, ok := ss.PluginOpts["mode"]; ok {
			spec.Extensions["plugin_mode"] = v
		}
		if v, ok := ss.PluginOpts["host"]; ok {
			spec.Extensions["plugin_host"] = v
		}
		if v, ok := ss.PluginOpts["path"]; ok {
			spec.Extensions["plugin_path"] = v
		}
		if v, ok := ss.PluginOpts["tls"]; ok && v == "true" {
			spec.Extensions["plugin_tls"] = true
		}
		if v, ok := ss.PluginOpts["password"]; ok {
			spec.Extensions["plugin_password"] = v
		}
		if v, ok := ss.PluginOpts["version"]; ok {
			spec.Extensions["plugin_version"] = v
		}
	}
	return nil
}

// fillHysteria2SubscriptionSpec projects a Hysteria2 inbound onto a
// SubscriptionSpec. There is no legacy SharedCred path — Phase 5 is
// hysteria2's first appearance in the mihomo container.
//
// Extensions populated for the clash converter:
//
//	"server_name"      — TLS SNI
//	"skip_cert_verify" — true for self-signed or explicit override
//	"obfs"             — "salamander" or absent
//	"obfs_password"    — paired with obfs
//	"up" / "down"      — bandwidth advertise (passed through to client)
//	"masquerade"       — server-side decoy; converter intentionally drops it
//	                     (mihomo client schema has no field), kept here so
//	                     other converters / debug tooling can read it.
func fillHysteria2SubscriptionSpec(spec *contracts.SubscriptionSpec, inb *MihomoInbound) error {
	if inb.ProtocolParams == nil || inb.ProtocolParams.Hysteria2 == nil {
		return fmt.Errorf("%w: hysteria2 inbound %q has no ProtocolParams", ErrMissingCredential, inb.Tag())
	}
	hy2 := inb.ProtocolParams.Hysteria2
	if hy2.Password == "" {
		return fmt.Errorf("%w: hysteria2 inbound %q has empty password", ErrMissingCredential, inb.Tag())
	}

	spec.Protocol = contracts.ProtocolHysteria2
	spec.Password = hy2.Password

	node := &codec.Hysteria2Node{
		NodeName:     spec.NodeName,
		Host:         spec.Host,
		Port:         spec.Port,
		Password:     hy2.Password,
		Obfs:         hy2.Obfs,
		ObfsPassword: hy2.ObfsPassword,
		Up:           hy2.Up,
		Down:         hy2.Down,
		Masquerade:   hy2.Masquerade,
	}

	if sec := inb.ProtocolParams.Security; sec != nil && sec.Kind == contracts.SecurityTLS && sec.TLS != nil {
		node.SNI = sec.TLS.SNI
		if sec.TLS.SNI != "" {
			spec.Extensions["server_name"] = sec.TLS.SNI
		}
		if len(sec.TLS.ALPN) > 0 {
			node.ALPN = sec.TLS.ALPN
		}
		if shouldSkipCertVerifyForClient(sec) {
			node.SkipCertVerify = true
			spec.Extensions["skip_cert_verify"] = true
		}
	}

	if hy2.Obfs != "" {
		spec.Extensions["obfs"] = hy2.Obfs
		if hy2.ObfsPassword != "" {
			spec.Extensions["obfs_password"] = hy2.ObfsPassword
		}
	}
	if hy2.Up != "" {
		spec.Extensions["up"] = hy2.Up
	}
	if hy2.Down != "" {
		spec.Extensions["down"] = hy2.Down
	}
	if hy2.Masquerade != "" {
		spec.Extensions["masquerade"] = hy2.Masquerade
	}

	spec.URI = node.Encode()
	return nil
}

// fillTuicSubscriptionSpec projects a TUIC inbound onto a SubscriptionSpec.
// No legacy SharedCred path — Phase 6 is TUIC's first appearance in the
// mihomo container, so ProtocolParams.TUIC is mandatory.
//
// Extensions populated for the clash converter (consumed by convertTuic in
// pkg/proxy/core/subscription/converter/clash.go):
//
//	"uuid"                  — convertTuic uses extString(ext,"uuid") for
//	                          ClashProxy.UUID; spec.Password carries the
//	                          v5 password
//	"server_name"           — TLS SNI
//	"skip_cert_verify"      — true for self-signed or explicit override
//	"congestion_controller" — passed through verbatim; profilegen wrote
//	                          "bbr" by default on the listener side
//	"udp_relay_mode"        — "" / "native" / "quic"; converter only emits
//	                          when non-empty (mihomo client default native)
//	"zero_rtt_handshake"    — bool; converter maps to client `reduce-rtt`.
//	                          Listener forces Allow0RTT=true unconditionally
//	                          so this is purely a client-side hint.
//	"heartbeat_interval"    — duration string, e.g. "10s"; converter
//	                          parses via time.ParseDuration to ms int.
//	"disable_sni"           — bool; client-only knob (no listener field).
//
// ALPN: when the listener carries an explicit ALPN list it's passed onto
// the TuicNode; mihomo client/listener both default to ["h3"] otherwise so
// emitting nothing on the client side keeps the schemas in lockstep.
func fillTuicSubscriptionSpec(spec *contracts.SubscriptionSpec, inb *MihomoInbound) error {
	if inb.ProtocolParams == nil || inb.ProtocolParams.TUIC == nil {
		return fmt.Errorf("%w: tuic inbound %q has no ProtocolParams", ErrMissingCredential, inb.Tag())
	}
	t := inb.ProtocolParams.TUIC
	if t.UUID == "" {
		return fmt.Errorf("%w: tuic inbound %q has empty uuid", ErrMissingCredential, inb.Tag())
	}
	if t.Password == "" {
		return fmt.Errorf("%w: tuic inbound %q has empty password", ErrMissingCredential, inb.Tag())
	}

	spec.Protocol = contracts.ProtocolTUIC
	spec.Password = t.Password
	spec.Extensions["uuid"] = t.UUID

	node := &codec.TuicNode{
		NodeName:          spec.NodeName,
		Host:              spec.Host,
		Port:              spec.Port,
		UUID:              t.UUID,
		Password:          t.Password,
		CongestionControl: t.CongestionController,
		UDPRelayMode:      t.UDPRelayMode,
		ZeroRTTHandshake:  t.ZeroRTTHandshake,
		HeartbeatInterval: t.HeartbeatInterval,
	}

	if sec := inb.ProtocolParams.Security; sec != nil && sec.Kind == contracts.SecurityTLS && sec.TLS != nil {
		node.SNI = sec.TLS.SNI
		if sec.TLS.SNI != "" {
			spec.Extensions["server_name"] = sec.TLS.SNI
		}
		if len(sec.TLS.ALPN) > 0 {
			node.ALPN = sec.TLS.ALPN
		}
		if shouldSkipCertVerifyForClient(sec) {
			node.SkipCertVerify = true
			spec.Extensions["skip_cert_verify"] = true
		}
	}

	if t.CongestionController != "" {
		spec.Extensions["congestion_controller"] = t.CongestionController
	}
	if t.UDPRelayMode != "" {
		spec.Extensions["udp_relay_mode"] = t.UDPRelayMode
	}
	if t.ZeroRTTHandshake {
		spec.Extensions["zero_rtt_handshake"] = true
	}
	if t.HeartbeatInterval != "" {
		spec.Extensions["heartbeat_interval"] = t.HeartbeatInterval
	}
	if t.DisableSNI {
		spec.Extensions["disable_sni"] = true
		// codec TuicNode mirrors the listener-side server_name handling;
		// disable_sni rides only on the converter path, not the URI (the
		// dae spec emits disable_sni=1 in the URI but mihomo's URI parser
		// already routes it through the same converter we drive here).
		node.DisableSNI = true
	}

	spec.URI = node.Encode()
	return nil
}

// fillAnyTLSSubscriptionSpec projects an AnyTLS inbound onto a SubscriptionSpec.
// No legacy SharedCred path — Phase 7 is AnyTLS's first appearance in the
// mihomo container, so ProtocolParams.AnyTLS is mandatory.
//
// Extensions populated for the clash converter (consumed by convertAnyTLS in
// pkg/proxy/core/subscription/converter/clash.go):
//
//	"server_name"                        — TLS SNI (mirrors hy2/tuic key)
//	"skip_cert_verify"                   — true for self-signed or override
//	"idle_session_check_interval_seconds" — int seconds; client-only
//	"idle_session_timeout_seconds"        — int seconds; client-only
//	"min_idle_session"                   — int; client-only
//
// Server-only fields (padding_scheme / client-auth-* / ech-key) are NOT
// propagated — mihomo's outbound schema has no client-side counterpart and
// our converter explicitly drops them.
//
// Cert-pin fingerprint (URI hpkp=) is intentionally NOT surfaced. mihomo's
// outbound schema accepts a `fingerprint:` field but v2raymg has no
// TLSSpec.Fingerprint to source it from today; if/when that's added, plumb
// it through Extensions["fingerprint"] and the convertAnyTLS read.
//
// ALPN: AnyTLS has no ALPN default ([h3] is hy2/tuic specific). The listener
// lets Go TLS auto-negotiate; we mirror that by emitting nothing client-side.
func fillAnyTLSSubscriptionSpec(spec *contracts.SubscriptionSpec, inb *MihomoInbound) error {
	if inb.ProtocolParams == nil || inb.ProtocolParams.AnyTLS == nil {
		return fmt.Errorf("%w: anytls inbound %q has no ProtocolParams", ErrMissingCredential, inb.Tag())
	}
	a := inb.ProtocolParams.AnyTLS
	if a.Password == "" {
		return fmt.Errorf("%w: anytls inbound %q has empty password", ErrMissingCredential, inb.Tag())
	}

	spec.Protocol = contracts.ProtocolAnyTLS
	spec.Password = a.Password

	node := &codec.AnyTLSNode{
		NodeName:                 spec.NodeName,
		Host:                     spec.Host,
		Port:                     spec.Port,
		Password:                 a.Password,
		PaddingScheme:            a.PaddingScheme,
		IdleSessionCheckInterval: a.IdleSessionCheckInterval,
		IdleSessionTimeout:       a.IdleSessionTimeout,
		MinIdleSession:           a.MinIdleSession,
	}

	if sec := inb.ProtocolParams.Security; sec != nil && sec.Kind == contracts.SecurityTLS && sec.TLS != nil {
		node.SNI = sec.TLS.SNI
		if sec.TLS.SNI != "" {
			spec.Extensions["server_name"] = sec.TLS.SNI
		}
		if len(sec.TLS.ALPN) > 0 {
			node.ALPN = sec.TLS.ALPN
		}
		if shouldSkipCertVerifyForClient(sec) {
			node.SkipCertVerify = true
			spec.Extensions["skip_cert_verify"] = true
		}
	}

	if a.IdleSessionCheckInterval > 0 {
		spec.Extensions["idle_session_check_interval_seconds"] = a.IdleSessionCheckInterval
	}
	if a.IdleSessionTimeout > 0 {
		spec.Extensions["idle_session_timeout_seconds"] = a.IdleSessionTimeout
	}
	if a.MinIdleSession > 0 {
		spec.Extensions["min_idle_session"] = a.MinIdleSession
	}

	spec.URI = node.Encode()
	return nil
}

func fillSSSubscriptionSpecLegacy(spec *contracts.SubscriptionSpec, inb *MihomoInbound) error {
	if inb.SharedCred.Password == "" {
		return fmt.Errorf("%w: shadowsocks inbound %q has empty password", ErrMissingCredential, inb.Tag())
	}
	method := inb.SharedCred.Cipher
	if method == "" {
		method = defaultSSMethod
	}
	nodeName := spec.NodeName
	if nodeName == "" {
		nodeName = inb.Tag()
	}
	spec.Protocol = contracts.ProtocolShadowsocks
	spec.Password = inb.SharedCred.Password
	node := &codec.ShadowsocksNode{
		NodeName: nodeName,
		Host:     spec.Host,
		Port:     spec.Port,
		Password: inb.SharedCred.Password,
		Method:   method,
	}
	spec.URI = node.Encode()
	spec.Extensions["method"] = method
	return nil
}
