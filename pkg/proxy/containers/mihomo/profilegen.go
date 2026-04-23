package mihomo

import (
	"fmt"
	"strconv"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
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
	case contracts.ProtocolVMess:
		m["users"] = []map[string]any{
			{
				// alterId: mihomo's VmessUser tag is literally "alterId"
				// (camelCase), not "alter-id". Verified from
				// listener/inbound/vmess.go:VmessUser struct.
				"uuid":    inb.SharedCred.UUID,
				"alterId": 0,
			},
		}
	case contracts.ProtocolTrojan:
		m["users"] = []map[string]any{
			{"password": inb.SharedCred.Password},
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
