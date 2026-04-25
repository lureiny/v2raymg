package protocolparams

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// parseTrojan fills the Trojan slot of a ProtocolParams from the raw request map.
//
// Contract:
//
//   - KeyPassword is required — caller-supplied or fabricated by FillDefaults.
//   - Transport defaults to TCP. mihomo Alpha Trojan accepts tcp / ws / grpc;
//     httpupgrade, xhttp, splithttp, and h2 are rejected early.
//   - Security defaults to TLS. Trojan is TLS-based at the wire level; explicit
//     security=none is rejected. Reality is allowed and carries its key material
//     through SecuritySpec.Reality.
func parseTrojan(raw map[string]any, pp *ProtocolParams) error {
	password, err := requireString(raw, KeyPassword)
	if err != nil {
		return fmt.Errorf("trojan: %w", err)
	}

	transport, err := parseTransport(raw)
	if err != nil {
		return fmt.Errorf("trojan: %w", err)
	}
	if transport == nil {
		transport = &TransportSpec{Kind: contracts.TransportTCP}
	}
	switch transport.Kind {
	case contracts.TransportTCP, contracts.TransportWS, contracts.TransportGRPC:
		// ok
	case contracts.TransportHTTPUpgrade:
		return fmt.Errorf("%w: trojan transport=httpupgrade not supported by mihomo listener; use ws or grpc",
			ErrInvalidCombination)
	case contracts.TransportXHTTP, contracts.TransportSplitHTTP:
		return fmt.Errorf("%w: trojan transport=%s not supported by mihomo listener; use ws or grpc",
			ErrInvalidCombination, transport.Kind)
	case "h2":
		return fmt.Errorf("%w: trojan transport=h2 not supported by mihomo listener; use ws or grpc",
			ErrInvalidCombination)
	default:
		return fmt.Errorf("%w: trojan transport %q not supported by mihomo listener",
			ErrInvalidCombination, transport.Kind)
	}

	security, err := parseSecurity(raw)
	if err != nil {
		return fmt.Errorf("trojan: %w", err)
	}
	if security == nil {
		tls, err := parseTLS(raw)
		if err != nil {
			return fmt.Errorf("trojan: %w", err)
		}
		security = &SecuritySpec{Kind: contracts.SecurityTLS, TLS: tls}
	}
	switch security.Kind {
	case contracts.SecurityTLS, contracts.SecurityReality:
		// ok
	case contracts.SecurityNone:
		return fmt.Errorf("%w: trojan requires tls or reality security", ErrInvalidCombination)
	default:
		return fmt.Errorf("%w: trojan security %q not supported", ErrInvalidCombination, security.Kind)
	}

	pp.Transport = transport
	pp.Security = security
	pp.Trojan = &TrojanParams{Password: password}
	return nil
}
