package protocolparams

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// parseVLESS fills in the VLESS-specific slot of a ProtocolParams from the
// raw request map.
//
// Contract:
//
//   - KeyUUID is required — either caller-supplied or fabricated by
//     FillDefaults (T-M-P1-10 extends FillDefaults to fill vless uuid).
//   - KeyFlow is optional — empty is canonical plain mode; callers that
//     want xtls-rprx-vision pass it explicitly. Validation of
//     flow×transport combinations is left to the adapter; parseVLESS
//     only enforces the shape.
//   - KeyDecryption is optional — defaults to "none" if unset. mihomo
//     accepts only "none" today; callers should not set anything else.
//   - Transport defaults to TCP when KeyTransport is absent (equivalent
//     to "tcp" in mihomo's listener schema — no explicit "network" key).
//   - Security may be none / tls / reality. Flow=xtls-rprx-vision is
//     conventional for reality but not enforced here.
//
// Phase 1 ships all six transport×security combos listed in the plan;
// mihomo's own listener schema rejects unsupported ones at yaml load
// time, which is the last line of defence.
func parseVLESS(raw map[string]any, pp *ProtocolParams) error {
	uuid, err := requireString(raw, KeyUUID)
	if err != nil {
		return fmt.Errorf("vless: %w", err)
	}

	transport, err := parseTransport(raw)
	if err != nil {
		return fmt.Errorf("vless: %w", err)
	}
	if transport == nil {
		transport = &TransportSpec{Kind: contracts.TransportTCP}
	}
	// mihomo Alpha's VlessOption has no httpupgrade or h2 field (only
	// ws-path / grpc-service-name / xhttp-config). Reject here rather
	// than deeper in the adapter so the caller sees a pointed error
	// instead of a generic "not supported by mihomo listener". xray
	// supports these combos; this gate is mihomo-specific — move it
	// to the mihomo adapter when a second consumer wants them.
	switch transport.Kind {
	case contracts.TransportHTTPUpgrade:
		return fmt.Errorf("%w: vless transport=httpupgrade not supported by mihomo listener; use ws or xhttp",
			ErrInvalidCombination)
	case "h2":
		return fmt.Errorf("%w: vless transport=h2 not supported by mihomo listener; use xhttp instead",
			ErrInvalidCombination)
	}

	security, err := parseSecurity(raw)
	if err != nil {
		return fmt.Errorf("vless: %w", err)
	}

	decryption := optionalString(raw, KeyDecryption)
	if decryption == "" {
		decryption = "none"
	}

	pp.Transport = transport
	pp.Security = security
	pp.VLESS = &VLESSParams{
		UUID:       uuid,
		Flow:       optionalString(raw, KeyFlow),
		Decryption: decryption,
	}
	return nil
}
