package mihomo

import (
	"errors"
	"fmt"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

// defaultSSMethod is used when a shadowsocks inbound was constructed without
// an explicit cipher. Must track the clash converter's fallback
// (pkg/proxy/core/subscription/converter/clash.go convertShadowsocks) so
// the generated URI and any Clash rendering derived from it agree on the
// cipher the client should use. If one side changes the default, update
// both.
const defaultSSMethod = "aes-256-gcm"

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
// Supported protocols today: vless / vmess (Phase 1+ structured-params
// path) and trojan / shadowsocks (legacy SharedCred path). Phase 3/4
// migrations will move trojan/ss onto ProtocolParams as well; this list
// must be updated when each phase lands. Unsupported protocols are
// logged and skipped rather than surfaced as hard errors — a mihomo
// container should never hold such an inbound (adapter rejects them
// upfront), but a corrupt-record path could leak through; graceful skip
// avoids breaking subscriptions for healthy inbounds on the same
// container.
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
//	vless → fillVLESSSubscriptionSpec (Phase 1, ProtocolParams; falls back to nothing — VLESS only ever lived on the new path)
//	vmess → fillVMessSubscriptionSpec (Phase 2, ProtocolParams preferred + SharedCred legacy fallback for pre-Phase-2 records)
//	trojan → SharedCred.Password (legacy; will migrate in Phase 3)
//	shadowsocks → SharedCred.Password + (Cipher || default) (legacy; will migrate in Phase 4)
//
// Adding a protocol (hysteria2 / tuic / anytls) means (a) extending
// MihomoInbound.Validate + adapter, (b) adding a codec.<Proto>Node Encode
// path here. Phase 3/4 will collapse the trojan / ss SharedCred branches
// onto ProtocolParams; update this list at each landing.
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
		if inb.SharedCred.Password == "" {
			return contracts.SubscriptionSpec{}, fmt.Errorf("%w: trojan inbound %q has empty password", ErrMissingCredential, inb.Tag())
		}
		spec.Protocol = contracts.ProtocolTrojan
		spec.Password = inb.SharedCred.Password
		node := &codec.TrojanNode{
			NodeName: nodeName,
			Host:     req.Host,
			Port:     port,
			Password: inb.SharedCred.Password,
		}
		spec.URI = node.Encode()

	case contracts.ProtocolShadowsocks:
		if inb.SharedCred.Password == "" {
			return contracts.SubscriptionSpec{}, fmt.Errorf("%w: shadowsocks inbound %q has empty password", ErrMissingCredential, inb.Tag())
		}
		method := inb.SharedCred.Cipher
		if method == "" {
			method = defaultSSMethod
		}
		spec.Protocol = contracts.ProtocolShadowsocks
		spec.Password = inb.SharedCred.Password
		node := &codec.ShadowsocksNode{
			NodeName: nodeName,
			Host:     req.Host,
			Port:     port,
			Password: inb.SharedCred.Password,
			Method:   method,
		}
		spec.URI = node.Encode()
		// The clash converter reads `method` from Extensions to set the
		// shadowsocks cipher in the Clash proxy entry. Expose it even when
		// we fell back to the default so upstream never has to re-derive.
		spec.Extensions["method"] = method

	default:
		return contracts.SubscriptionSpec{}, fmt.Errorf("%w: %q", ErrProtocolNotSupported, inb.Protocol())
	}

	return spec, nil
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
				if sec.TLS.CertSource == "self_signed" || sec.TLS.SkipCertVerify {
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
				if sec.TLS.CertSource == "self_signed" || sec.TLS.SkipCertVerify {
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
