package mihomo

import (
	"fmt"
	"sort"

	"github.com/lureiny/v2raymg/pkg/proxy/core/chain"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"gopkg.in/yaml.v3"
)

// BuildConfig renders a complete mihomo config.yaml with the provided base
// settings and the current set of v2raymg-managed inbounds as top-level
// listeners[], plus any 1:1 chained outbound proxies and IN-NAME routing rules.
//
// Inbounds are emitted in stable tag order so the output is deterministic —
// this matters for (a) diff-friendly on-disk configs and (b) any test that
// byte-compares marshaled output. mihomo's PatchInboundListeners is
// name-keyed so the physical order in yaml doesn't affect runtime behavior.
//
// A nil or empty inbounds slice produces a config with listeners: []. The
// base struct's existing Listeners field is overwritten with this slice;
// callers should not prepopulate it.
func BuildConfig(base mihomoInitialConfig, inbounds []*MihomoInbound) ([]byte, error) {
	sorted := append(([]*MihomoInbound)(nil), inbounds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Tag() < sorted[j].Tag() })

	proxies := make([]any, 0, len(base.Proxies))
	proxies = append(proxies, base.Proxies...)
	var chainRules []string

	listeners := make([]any, 0, len(sorted))
	for _, inb := range sorted {
		entry, err := BuildListener(inb)
		if err != nil {
			return nil, fmt.Errorf("mihomo: build listener for %q: %w", inb.Tag(), err)
		}
		listeners = append(listeners, entry)

		if inb.GetInboundType() == contracts.InboundTypeChain {
			proxyName := chainProxyName(inb.Tag())
			spec, err := chain.ParseURI(inb.GetChainOutboundURI())
			if err != nil {
				return nil, fmt.Errorf("mihomo: parse chain uri for %q: %w", inb.Tag(), err)
			}
			proxyDict, err := chain.RenderMihomoProxy(proxyName, spec)
			if err != nil {
				return nil, fmt.Errorf("mihomo: render chain proxy for %q: %w", inb.Tag(), err)
			}
			proxies = append(proxies, proxyDict)
			chainRules = append(chainRules, "IN-NAME,"+inb.Tag()+","+proxyName)
		}
	}

	rules := make([]string, 0, len(chainRules)+len(base.Rules))
	rules = append(rules, chainRules...)
	rules = append(rules, base.Rules...)

	if base.ProxyGroups == nil {
		base.ProxyGroups = []any{}
	}
	base.Proxies = proxies
	base.Rules = rules
	base.Listeners = listeners

	data, err := yaml.Marshal(&base)
	if err != nil {
		return nil, fmt.Errorf("mihomo: marshal config: %w", err)
	}
	return data, nil
}
