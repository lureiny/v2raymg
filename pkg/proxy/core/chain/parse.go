package chain

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

// ParseURI parses a backend proxy URI into a protocol-agnostic OutboundSpec.
// Domain names in Host are preserved verbatim (no DNS resolution) so dynamic
// DDNS backends continue resolving freshly at connection time.
func ParseURI(rawURI string) (*OutboundSpec, error) {
	trimmed := strings.TrimSpace(rawURI)
	if trimmed == "" {
		return nil, fmt.Errorf("chain: empty URI")
	}

	idx := strings.Index(trimmed, "://")
	if idx <= 0 {
		return nil, fmt.Errorf("chain: missing URI scheme")
	}
	scheme := strings.ToLower(trimmed[:idx])

	node, err := codec.Decode(trimmed)
	if err != nil {
		return nil, fmt.Errorf("chain: decode %s URI: %w", scheme, sanitizeParseError(err))
	}

	var spec *OutboundSpec
	switch n := node.(type) {
	case *codec.VLessNode:
		spec = &OutboundSpec{
			Scheme:           scheme,
			Protocol:         contracts.ProtocolVLess,
			Host:             n.Host,
			Port:             n.Port,
			Remark:           n.NodeName,
			UUID:             n.UUID,
			Flow:             n.Flow,
			Encryption:       n.Encryption,
			Transport:        n.Transport,
			Security:         n.Security,
			TLS:              n.Security == "tls",
			SNI:              n.SNI,
			ALPN:             parseQueryALPN(trimmed),
			Fingerprint:      n.Fingerprint,
			SkipCertVerify:   n.SkipCertVerify,
			RealityPublicKey: n.RealityPublicKey,
			RealityShortID:   n.RealityShortID,
			RealitySpiderX:   n.RealitySpiderX,
			WSPath:           n.WSPath,
			WSHost:           n.WSHost,
			GRPCServiceName:  n.GRPCServiceName,
			H2Path:           n.H2Path,
			H2Host:           n.H2Host,
			HTTPUpgradePath:  n.HTTPUpgradePath,
			HTTPUpgradeHost:  n.HTTPUpgradeHost,
			XHTTPPath:        n.XHTTPPath,
			XHTTPHost:        n.XHTTPHost,
			XHTTPMode:        n.XHTTPMode,
		}
	case *codec.VMessNode:
		spec = &OutboundSpec{
			Scheme:          scheme,
			Protocol:        contracts.ProtocolVMess,
			Host:            n.Host,
			Port:            n.Port,
			Remark:          n.NodeName,
			UUID:            n.UUID,
			AlterID:         n.AlterId,
			Method:          n.Cipher,
			Transport:       n.Transport,
			HeaderType:      n.HeaderType,
			Security:        n.Security,
			TLS:             n.Security == "tls",
			SNI:             n.SNI,
			ALPN:            slices.Clone(n.ALPN),
			Fingerprint:     n.Fingerprint,
			SkipCertVerify:  n.SkipCertVerify,
			WSPath:          n.WSPath,
			WSHost:          n.WSHost,
			H2Path:          n.H2Path,
			H2Host:          n.H2Host,
			GRPCServiceName: n.GRPCServiceName,
			HTTPUpgradePath: n.HTTPUpgradePath,
			HTTPUpgradeHost: n.HTTPUpgradeHost,
			XHTTPPath:       n.XHTTPPath,
			XHTTPHost:       n.XHTTPHost,
		}
	case *codec.TrojanNode:
		sec := n.Security
		if sec == "" {
			sec = "tls"
		}
		spec = &OutboundSpec{
			Scheme:           scheme,
			Protocol:         contracts.ProtocolTrojan,
			Host:             n.Host,
			Port:             n.Port,
			Remark:           n.NodeName,
			Password:         n.Password,
			Flow:             n.Flow,
			Transport:        n.Transport,
			Security:         sec,
			TLS:              sec == "tls",
			SNI:              n.SNI,
			ALPN:             parseQueryALPN(trimmed),
			Fingerprint:      n.Fingerprint,
			SkipCertVerify:   n.SkipCertVerify,
			RealityPublicKey: n.RealityPublicKey,
			RealityShortID:   n.RealityShortID,
			RealitySpiderX:   n.RealitySpiderX,
			WSPath:           n.WSPath,
			WSHost:           n.WSHost,
			GRPCServiceName:  n.GRPCServiceName,
			H2Path:           n.H2Path,
			H2Host:           n.H2Host,
			HTTPUpgradePath:  n.HTTPUpgradePath,
			HTTPUpgradeHost:  n.HTTPUpgradeHost,
		}
	case *codec.ShadowsocksNode:
		var opts map[string]string
		if len(n.PluginOpts) > 0 {
			opts = make(map[string]string, len(n.PluginOpts))
			for k, v := range n.PluginOpts {
				opts[k] = v
			}
		}
		spec = &OutboundSpec{
			Scheme:     scheme,
			Protocol:   contracts.ProtocolShadowsocks,
			Host:       n.Host,
			Port:       n.Port,
			Remark:     n.NodeName,
			Method:     n.Method,
			Password:   n.Password,
			Plugin:     n.Plugin,
			PluginOpts: opts,
		}
	case *codec.Hysteria2Node:
		spec = &OutboundSpec{
			Scheme:         scheme,
			Protocol:       contracts.ProtocolHysteria2,
			Host:           n.Host,
			Port:           n.Port,
			Remark:         n.NodeName,
			Password:       n.Password,
			Security:       "tls",
			TLS:            true,
			SNI:            n.SNI,
			SkipCertVerify: n.SkipCertVerify,
			ALPN:           slices.Clone(n.ALPN),
			Obfs:           n.Obfs,
			ObfsPassword:   n.ObfsPassword,
			Ports:          n.Ports,
			PinSHA256:      n.PinSHA256,
		}
	case *codec.TuicNode:
		spec = &OutboundSpec{
			Scheme:            scheme,
			Protocol:          contracts.ProtocolTUIC,
			Host:              n.Host,
			Port:              n.Port,
			Remark:            n.NodeName,
			UUID:              n.UUID,
			Password:          n.Password,
			Security:          "tls",
			TLS:               true,
			SNI:               n.SNI,
			SkipCertVerify:    n.SkipCertVerify,
			ALPN:              slices.Clone(n.ALPN),
			CongestionControl: n.CongestionControl,
			UDPRelayMode:      n.UDPRelayMode,
			DisableSNI:        n.DisableSNI,
		}
	case *codec.AnyTLSNode:
		spec = &OutboundSpec{
			Scheme:         scheme,
			Protocol:       contracts.ProtocolAnyTLS,
			Host:           n.Host,
			Port:           n.Port,
			Remark:         n.NodeName,
			Password:       n.Password,
			Security:       "tls",
			TLS:            true,
			SNI:            n.SNI,
			SkipCertVerify: n.SkipCertVerify,
			Fingerprint:    n.Fingerprint,
			ALPN:           slices.Clone(n.ALPN),
		}
	case *codec.SOCKS5Node:
		spec = &OutboundSpec{
			Scheme:   scheme,
			Protocol: contracts.ProtocolSOCKS5,
			Host:     n.Host,
			Port:     n.Port,
			Remark:   n.NodeName,
			Username: n.Username,
			Password: n.Password,
		}
	case *codec.HTTPNode:
		sec := ""
		if n.TLS {
			sec = "tls"
		}
		spec = &OutboundSpec{
			Scheme:         scheme,
			Protocol:       contracts.ProtocolHTTP,
			Host:           n.Host,
			Port:           n.Port,
			Remark:         n.NodeName,
			Username:       n.Username,
			Password:       n.Password,
			TLS:            n.TLS,
			Security:       sec,
			SNI:            n.SNI,
			SkipCertVerify: n.SkipCertVerify,
		}
	case *codec.SnellNode:
		spec = &OutboundSpec{
			Scheme:   scheme,
			Protocol: contracts.ProtocolSnell,
			Host:     n.Host,
			Port:     n.Port,
			Remark:   n.NodeName,
			Password: n.PSK,
			Obfs:     n.Obfs,
		}
	default:
		return nil, fmt.Errorf("chain: unsupported decoded node type %T", node)
	}

	return spec, nil
}

func parseQueryALPN(rawURI string) []string {
	u, err := url.Parse(rawURI)
	if err != nil {
		return nil
	}
	q := u.Query()
	if vs := q["alpn"]; len(vs) > 1 {
		out := make([]string, 0, len(vs))
		for _, v := range vs {
			if s := strings.TrimSpace(v); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	if raw := q.Get("alpn"); raw != "" {
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// sanitizeParseError strips raw URLs and host/authority strings from *url.Error
// and *net.AddrError so secrets embedded in malformed URIs never leak via error
// messages, RPC responses, or logs.
func sanitizeParseError(err error) error {
	if err == nil {
		return nil
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return sanitizeParseError(ue.Err)
	}
	var ae *net.AddrError
	if errors.As(err, &ae) && ae.Err != "" {
		return errors.New(ae.Err)
	}
	return err
}
