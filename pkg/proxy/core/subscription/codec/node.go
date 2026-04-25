// Package codec provides typed Encode/Decode for proxy subscription URIs.
//
// Each supported protocol (VLESS, VMess, Trojan, Shadowsocks, Hysteria2,
// TUIC, Snell) has a concrete Node struct that implements the Node interface.
// Callers Decode a raw URI once to get a typed struct, then access its fields
// directly or Encode it back to a URI for another client format.
//
// Usage:
//
//	node, err := codec.Decode("vless://uuid@host:443?security=tls&...")
//	if vl, ok := node.(*VLessNode); ok {
//	    // access vl.Security, vl.Transport, vl.RealityPublicKey, etc.
//	}
package codec

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// ErrUnsupportedScheme is returned by Decode when the input does not look like
// a URI we can dispatch — either because the scheme prefix is missing entirely
// or because no decoder is registered for it. Callers can test
// errors.Is(err, ErrUnsupportedScheme) to distinguish "not our URI" (typical:
// silently skip) from a real decode failure on a known scheme (typical: log
// and investigate).
var ErrUnsupportedScheme = errors.New("unsupported URI scheme")

// Node is the common interface implemented by every protocol node struct.
// It carries only what's needed to dispatch and re-encode a node; richer field
// access is done by type-asserting to the concrete struct.
type Node interface {
	Protocol() contracts.Protocol
	Encode() string
}

// Decode dispatches a proxy URI to the protocol decoder matched by its scheme.
// The scheme is extracted by hand (not url.Parse) as a cheap dispatch; each
// concrete decoder still does a full parse of its own input.
func Decode(uri string) (Node, error) {
	idx := strings.Index(uri, "://")
	if idx < 0 {
		return nil, fmt.Errorf("%w: missing scheme in %q", ErrUnsupportedScheme, uri)
	}
	scheme := strings.ToLower(uri[:idx])
	dec, ok := decoders[scheme]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedScheme, scheme)
	}
	return dec(uri)
}

var decoders = map[string]func(string) (Node, error){
	"vless":     func(u string) (Node, error) { return DecodeVLess(u) },
	"vmess":     func(u string) (Node, error) { return DecodeVMess(u) },
	"trojan":    func(u string) (Node, error) { return DecodeTrojan(u) },
	"ss":        func(u string) (Node, error) { return DecodeShadowsocks(u) },
	"hysteria2": func(u string) (Node, error) { return DecodeHysteria2(u) },
	"hy2":       func(u string) (Node, error) { return DecodeHysteria2(u) },
	"tuic":      func(u string) (Node, error) { return DecodeTuic(u) },
	"snell":     func(u string) (Node, error) { return DecodeSnell(u) },
}

// hostPort formats host:port; wraps IPv6 literals in brackets per RFC 3986.
func hostPort(host string, port uint32) string {
	if strings.Contains(host, ":") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}
