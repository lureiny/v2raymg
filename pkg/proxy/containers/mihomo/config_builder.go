package mihomo

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// BuildConfig renders a complete mihomo config.yaml with the provided base
// settings and the current set of v2raymg-managed inbounds as top-level
// listeners[].
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

	listeners := make([]any, 0, len(sorted))
	for _, inb := range sorted {
		entry, err := BuildListener(inb)
		if err != nil {
			return nil, fmt.Errorf("mihomo: build listener for %q: %w", inb.Tag(), err)
		}
		listeners = append(listeners, entry)
	}
	base.Listeners = listeners

	data, err := yaml.Marshal(&base)
	if err != nil {
		return nil, fmt.Errorf("mihomo: marshal config: %w", err)
	}
	return data, nil
}
