package xray

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutorConfig_Decode_GRPCPort(t *testing.T) {
	cases := []struct {
		name string
		val  any
		has  bool
		want string
	}{
		{name: "unset uses default", has: false, want: "127.0.0.1:62789"},
		{name: "zero keeps default", val: 0, has: true, want: "127.0.0.1:62789"},
		{name: "negative keeps default", val: -1, has: true, want: "127.0.0.1:62789"},
		{name: "yaml int", val: 35309, has: true, want: "127.0.0.1:35309"},
		{name: "int64", val: int64(35310), has: true, want: "127.0.0.1:35310"},
		{name: "uint64", val: uint64(35311), has: true, want: "127.0.0.1:35311"},
		{name: "json float64", val: float64(35312), has: true, want: "127.0.0.1:35312"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgMap := map[string]any{}
			if tc.has {
				cfgMap["grpc_port"] = tc.val
			}
			var cfg ExecutorConfig
			require.NoError(t, cfg.Decode(cfgMap))
			assert.Equal(t, tc.want, cfg.GRPCAPIAddress)
		})
	}
}

func TestXrayAPI_AddInbound_RejectsNilProxySettings(t *testing.T) {
	api := NewXrayAPI("127.0.0.1:1", false)
	err := api.AddInbound([]byte(`{"tag":"broken"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing proxy settings")
}
