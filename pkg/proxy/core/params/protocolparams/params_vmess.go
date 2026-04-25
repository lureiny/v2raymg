package protocolparams

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// parseVMess fills the VMess slot of a ProtocolParams from the raw request map.
//
// Contract:
//
//   - KeyUUID is required — caller-supplied or fabricated by FillDefaults.
//   - KeyAlterID is optional — default 0 (xray parity: buildSettings vmess
//     emits clients[] with only id + alterId; a missing alter_id is treated
//     as 0, matching mihomo's VmessUser default).
//   - KeyCipher is optional — no default. Neither xray nor mihomo VMess
//     listeners accept a server-side cipher field; VMess URIs carry no
//     cipher either. The clash converter hardcodes "auto" on the client
//     side. Leaving Cipher empty when unspecified keeps this layer
//     byte-identical with xray's handling.
//   - Transport defaults to TCP when KeyTransport is absent. mihomo
//     VmessOption only accepts tcp / ws / grpc — httpupgrade, xhttp,
//     splithttp and h2 are rejected here with ErrInvalidCombination so
//     callers see a pointed error instead of a later profilegen default.
//     Move the gate to the adapter layer if a second consumer wants them.
//   - Security may be none / tls / reality — identical to vless.
func parseVMess(raw map[string]any, pp *ProtocolParams) error {
	uuid, err := requireString(raw, KeyUUID)
	if err != nil {
		return fmt.Errorf("vmess: %w", err)
	}

	transport, err := parseTransport(raw)
	if err != nil {
		return fmt.Errorf("vmess: %w", err)
	}
	if transport == nil {
		transport = &TransportSpec{Kind: contracts.TransportTCP}
	}
	// mihomo Alpha's VmessOption exposes ws-path / grpc-service-name /
	// reality-config only — no httpupgrade, xhttp, splithttp, or h2 field.
	// Reject early; the mihomo-specific gate mirrors parseVLESS.
	switch transport.Kind {
	case contracts.TransportHTTPUpgrade:
		return fmt.Errorf("%w: vmess transport=httpupgrade not supported by mihomo listener; use ws or grpc",
			ErrInvalidCombination)
	case contracts.TransportXHTTP, contracts.TransportSplitHTTP:
		return fmt.Errorf("%w: vmess transport=%s not supported by mihomo listener; use ws or grpc",
			ErrInvalidCombination, transport.Kind)
	case "h2":
		return fmt.Errorf("%w: vmess transport=h2 not supported by mihomo listener; use ws or grpc",
			ErrInvalidCombination)
	}

	security, err := parseSecurity(raw)
	if err != nil {
		return fmt.Errorf("vmess: %w", err)
	}

	alterID, _ := optionalInt(raw, KeyAlterID)

	pp.Transport = transport
	pp.Security = security
	pp.VMess = &VMessParams{
		UUID:    uuid,
		AlterID: alterID,
		Cipher:  optionalString(raw, KeyCipher),
	}
	return nil
}
