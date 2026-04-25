package http

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// TestFastAddValidProtocolsMatchesGetBuilderType locks the three-layer
// protocol whitelist alignment:
//   - fastAddValidProtocols (HTTP handler pre-filter)
//   - getBuilderType mapping (HTTP → proto)
//   - end_node_inbound.go's InboundBuilderType switch (RPC decode)
//
// The reviewer flagged a P0 drift where fastAddValidProtocols missed
// hy2/tuic/anytls even though the other two layers were wired up. Lock
// the alignment in both directions: every whitelisted protocol must have
// a real builder type, and every non-transport BuilderType value must be
// reachable from some whitelisted protocol alias.
func TestFastAddValidProtocolsMatchesGetBuilderType(t *testing.T) {
	// Direction 1: whitelist → builder.
	for p := range fastAddValidProtocols {
		if getBuilderType(p) == 0 {
			t.Errorf("fastAddValidProtocols contains %q but getBuilderType returns UnknowBuilderType", p)
		}
	}
	// Direction 2: every listed whitelist entry covers at least the
	// canonical protocols that have downstream switches in
	// end_node_inbound.go FastAddInbound.
	expected := []string{"vless", "vmess", "trojan", "shadowsocks", "hysteria2", "tuic", "anytls"}
	for _, p := range expected {
		if !fastAddValidProtocols[p] {
			t.Errorf("expected protocol %q missing from fastAddValidProtocols", p)
		}
	}
	// "ss" is an accepted alias for shadowsocks.
	if !fastAddValidProtocols["ss"] {
		t.Error("ss alias missing from fastAddValidProtocols")
	}
}

// TestGetBuilderType ensures the protocol string → BuilderType mapping
// covers every protocol the HTTP FastAdd API accepts. Missing a mapping
// here means the FastAdd request gets `UnknowBuilderType` (0) on the
// wire, which the end-node switch treats as "unsupport protocol" and
// rejects with code 1020. Table-driven so new protocols must extend both
// sides of the enum at once.
func TestGetBuilderType(t *testing.T) {
	cases := []struct {
		input string
		want  proto.BuilderType
	}{
		// protocols
		{"vless", proto.BuilderType_VLESSSettingBuilderType},
		{"vmess", proto.BuilderType_VMESSSettingBuilderType},
		{"trojan", proto.BuilderType_TrojanSettingBuilderType},
		{"ss", proto.BuilderType_SSSettingBuilderType},
		{"shadowsocks", proto.BuilderType_SSSettingBuilderType},
		{"hysteria2", proto.BuilderType_Hysteria2SettingBuilderType},
		{"tuic", proto.BuilderType_TUICSettingBuilderType},
		{"anytls", proto.BuilderType_AnyTLSSettingBuilderType},
		// case-insensitive for protocols
		{"VLESS", proto.BuilderType_VLESSSettingBuilderType},
		{"Hysteria2", proto.BuilderType_Hysteria2SettingBuilderType},
		// transports (unchanged; asserted so a refactor can't silently drop them)
		{"tcp", proto.BuilderType_TCPBuilderType},
		{"ws", proto.BuilderType_WSBuilderType},
		{"grpc", proto.BuilderType_GrpcBuilderType},
		{"http", proto.BuilderType_HttpBuilderType},
		// unknown
		{"unknown", proto.BuilderType_UnknowBuilderType},
		{"", proto.BuilderType_UnknowBuilderType},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := getBuilderType(tc.input)
			if got != tc.want {
				t.Errorf("getBuilderType(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
