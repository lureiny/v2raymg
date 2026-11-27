package converter

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

func protocolPrefix(spec contracts.SubscriptionSpec) string {
	switch spec.Protocol {
	case contracts.ProtocolVLess:
		return "VLESS"
	case contracts.ProtocolVMess:
		return "VMESS"
	case contracts.ProtocolTrojan:
		return "TROJAN"
	case contracts.ProtocolShadowsocks:
		return "SS"
	case contracts.ProtocolSnell:
		return "SNELL"
	case contracts.ProtocolHysteria2:
		return "HYSTERIA2"
	case contracts.ProtocolTUIC:
		return "TUIC"
	case contracts.ProtocolAnyTLS:
		return "ANYTLS"
	case contracts.ProtocolSOCKS5:
		return "SOCKS5"
	case contracts.ProtocolHTTP:
		return "HTTP"
	default:
		lower := strings.ToLower(strings.TrimSpace(spec.URI))
		if strings.HasPrefix(lower, "hysteria2://") || strings.HasPrefix(lower, "hy2://") {
			return "HYSTERIA2"
		}
		return strings.ToUpper(string(spec.Protocol))
	}
}

func formatDisplayName(spec contracts.SubscriptionSpec, protoPrefix string) string {
	name := spec.NodeName
	if name == "" {
		name = spec.InboundTag
	}
	if spec.IsExternal {
		if name != "" {
			return name
		}
		if spec.Host != "" {
			return spec.Host
		}
		return protoPrefix
	}
	if protoPrefix == "" {
		return name
	}
	return fmt.Sprintf("🌿 %s_%s", protoPrefix, name)
}

func baseDisplayName(spec contracts.SubscriptionSpec) string {
	return formatDisplayName(spec, protocolPrefix(spec))
}

// deduplicateDisplayNames suffixes every occurrence of a duplicate display name
// starting at _01, _02, ... while leaving unique names unchanged.
// Pre-reserving unique names prevents secondary collisions with pre-existing _01 suffixes.
func deduplicateDisplayNames(names []string) []string {
	counts := make(map[string]int, len(names))
	for _, n := range names {
		if n != "" {
			counts[n]++
		}
	}

	used := make(map[string]bool, len(names))
	for _, n := range names {
		if n != "" && counts[n] == 1 {
			used[n] = true
		}
	}

	nextIdx := make(map[string]int)
	out := make([]string, len(names))
	for i, n := range names {
		if n == "" {
			continue
		}
		if counts[n] == 1 {
			out[i] = n
			continue
		}
		idx := nextIdx[n] + 1
		for {
			candidate := fmt.Sprintf("%s_%02d", n, idx)
			if !used[candidate] {
				used[candidate] = true
				nextIdx[n] = idx
				out[i] = candidate
				break
			}
			idx++
		}
	}
	return out
}

func sanitizeSurgeName(name string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(name)
}

func encodeURIFragment(name string) string {
	return (&url.URL{Fragment: name}).EscapedFragment()
}

func extractRawFragment(rawURI string) string {
	if u, err := url.Parse(rawURI); err == nil && u.Fragment != "" {
		return u.Fragment
	}
	if idx := strings.IndexByte(rawURI, '#'); idx >= 0 && idx+1 < len(rawURI) {
		frag := rawURI[idx+1:]
		if unescaped, err := url.PathUnescape(frag); err == nil {
			return unescaped
		}
		return frag
	}
	return ""
}

func nodeNameFromCodecNode(node codec.Node) (name, host string) {
	switch n := node.(type) {
	case *codec.VLessNode:
		return n.NodeName, n.Host
	case *codec.VMessNode:
		return n.NodeName, n.Host
	case *codec.TrojanNode:
		return n.NodeName, n.Host
	case *codec.ShadowsocksNode:
		return n.NodeName, n.Host
	case *codec.Hysteria2Node:
		return n.NodeName, n.Host
	case *codec.TuicNode:
		return n.NodeName, n.Host
	case *codec.AnyTLSNode:
		return n.NodeName, n.Host
	case *codec.SnellNode:
		return n.NodeName, n.Host
	case *codec.SOCKS5Node:
		return n.NodeName, n.Host
	case *codec.HTTPNode:
		return n.NodeName, n.Host
	default:
		return "", ""
	}
}

func rewriteURIDisplayName(rawURI, newName string) string {
	if newName == "" {
		return rawURI
	}
	if strings.HasPrefix(strings.ToLower(rawURI), "vmess://") {
		body := rawURI[len("vmess://"):]
		var decoded []byte
		for _, enc := range []*base64.Encoding{
			base64.StdEncoding,
			base64.URLEncoding,
			base64.RawStdEncoding,
			base64.RawURLEncoding,
		} {
			if d, err := enc.DecodeString(body); err == nil {
				decoded = d
				break
			}
		}
		if decoded == nil {
			return rawURI
		}
		dec := json.NewDecoder(bytes.NewReader(decoded))
		dec.UseNumber()
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil || obj == nil {
			return rawURI
		}
		obj["ps"] = newName
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(obj); err != nil {
			return rawURI
		}
		data := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
		return "vmess://" + base64.StdEncoding.EncodeToString(data)
	}
	if strings.Contains(rawURI, "://") {
		if idx := strings.IndexByte(rawURI, '#'); idx >= 0 {
			rawURI = rawURI[:idx]
		}
		return rawURI + "#" + encodeURIFragment(newName)
	}
	return rawURI
}

func renderNamedURIs(specs []contracts.SubscriptionSpec) []string {
	kept := make([]contracts.SubscriptionSpec, 0, len(specs))
	baseNames := make([]string, 0, len(specs))

	for _, spec := range specs {
		if spec.URI == "" {
			continue
		}
		kept = append(kept, spec)

		eff := spec
		if eff.Protocol == "" && eff.NodeName == "" {
			if node, err := codec.Decode(eff.URI); err == nil {
				eff.Protocol = node.Protocol()
				eff.NodeName, eff.Host = nodeNameFromCodecNode(node)
			}
		}

		var baseName string
		switch {
		case eff.Protocol != "" || eff.NodeName != "" || eff.InboundTag != "":
			baseName = baseDisplayName(eff)
		case eff.IsExternal:
			baseName = extractRawFragment(eff.URI)
		default:
			baseName = ""
		}
		baseNames = append(baseNames, baseName)
	}

	finalNames := deduplicateDisplayNames(baseNames)
	out := make([]string, len(kept))
	for i, spec := range kept {
		out[i] = rewriteURIDisplayName(spec.URI, finalNames[i])
	}
	return out
}
