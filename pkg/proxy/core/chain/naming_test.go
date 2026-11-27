package chain

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestResolveSubscriptionNodeName_Chain(t *testing.T) {
	vmessJSON, _ := json.Marshal(map[string]any{
		"v": "2", "ps": "VMess-Home-Exit", "add": "1.2.3.4", "port": 443,
		"id": "11111111-1111-4111-8111-111111111111", "aid": 0, "net": "ws",
	})
	vmessURI := "vmess://" + base64.StdEncoding.EncodeToString(vmessJSON)

	tests := []struct {
		name        string
		reqNodeName string
		tag         string
		meta        contracts.InboundMeta
		want        string
	}{
		{
			name:        "chain explicit remark overrides uri fragment and ignores reqNodeName",
			reqNodeName: "EdgeNode",
			tag:         "ch-1",
			meta: contracts.InboundMeta{
				InboundType:      contracts.InboundTypeChain,
				Remark:           "CustomExit",
				ChainOutboundURI: "socks5://1.2.3.4:1080#BackendFrag",
			},
			want: "CustomExit",
		},
		{
			name:        "chain falls back to fragment when remark empty and ignores reqNodeName",
			reqNodeName: "EdgeNode",
			tag:         "ch-2",
			meta: contracts.InboundMeta{
				InboundType:      contracts.InboundTypeChain,
				ChainOutboundURI: "hy2://pw@1.2.3.4:28443?sni=bing.com#SH%20Home%20Line",
			},
			want: "SH Home Line",
		},
		{
			name:        "chain falls back to vmess ps when remark empty",
			reqNodeName: "EdgeNode",
			tag:         "ch-3",
			meta: contracts.InboundMeta{
				InboundType:      contracts.InboundTypeChain,
				ChainOutboundURI: vmessURI,
			},
			want: "VMess-Home-Exit",
		},
		{
			name:        "chain falls back to tag when both remark and uri name are empty",
			reqNodeName: "EdgeNode",
			tag:         "ch-notag",
			meta: contracts.InboundMeta{
				InboundType:      contracts.InboundTypeChain,
				ChainOutboundURI: "socks5://1.2.3.4:1080",
			},
			want: "ch-notag",
		},
		{
			name:        "direct with remark prepends reqNodeName",
			reqNodeName: "EdgeNode",
			tag:         "dir-1",
			meta: contracts.InboundMeta{
				InboundType: contracts.InboundTypeDirect,
				Remark:      "DirectVIP",
			},
			want: "EdgeNode-DirectVIP",
		},
		{
			name:        "direct with remark and empty reqNodeName",
			reqNodeName: "",
			tag:         "dir-1",
			meta: contracts.InboundMeta{
				InboundType: contracts.InboundTypeDirect,
				Remark:      "DirectVIP",
			},
			want: "DirectVIP",
		},
		{
			name:        "direct without remark uses reqNodeName",
			reqNodeName: "EdgeNode",
			tag:         "dir-2",
			meta:        contracts.InboundMeta{InboundType: contracts.InboundTypeDirect},
			want:        "EdgeNode",
		},
		{
			name:        "direct without remark or reqNodeName falls back to tag",
			reqNodeName: "",
			tag:         "dir-2",
			meta:        contracts.InboundMeta{InboundType: contracts.InboundTypeDirect},
			want:        "dir-2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveSubscriptionNodeName(tc.reqNodeName, tc.tag, tc.meta); got != tc.want {
				t.Fatalf("ResolveSubscriptionNodeName(%q, %q, %+v) = %q, want %q",
					tc.reqNodeName, tc.tag, tc.meta, got, tc.want)
			}
		})
	}
}
