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
// Unsupported protocols — anything outside the MVP trio (vmess / trojan /
// shadowsocks) — are logged and skipped rather than surfaced as hard errors.
// A mihomo container should never hold such an inbound (adapter rejects
// them upfront), but a corrupt-record path from a future protocol expansion
// could leak through; graceful skip avoids breaking subscriptions for
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
// Stays in sync with the MVP credential model:
//
//	vmess → UUID
//	trojan → Password
//	shadowsocks → Password + (Cipher || default)
//
// Adding a protocol (vless / hysteria2 / tuic) means (a) extending
// MihomoInbound.Validate + adapter, (b) adding a codec.<Proto>Node Encode
// path here. Both live in-package so a single PR can land the feature.
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
	case contracts.ProtocolVMess:
		if inb.SharedCred.UUID == "" {
			return contracts.SubscriptionSpec{}, fmt.Errorf("%w: vmess inbound %q has empty uuid", ErrMissingCredential, inb.Tag())
		}
		spec.Protocol = contracts.ProtocolVMess
		spec.Password = inb.SharedCred.UUID
		node := &codec.VMessNode{
			NodeName: nodeName,
			Host:     req.Host,
			Port:     port,
			UUID:     inb.SharedCred.UUID,
		}
		spec.URI = node.Encode()

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
