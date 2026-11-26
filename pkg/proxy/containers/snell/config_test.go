package snell

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnellConfig_Decode_Port(t *testing.T) {
	cases := []struct {
		name string
		val  any
		has  bool
		want int
	}{
		{name: "unset uses default", has: false, want: 16160},
		{name: "zero keeps default", val: 0, has: true, want: 16160},
		{name: "negative keeps default", val: -1, has: true, want: 16160},
		{name: "yaml int", val: 24433, has: true, want: 24433},
		{name: "int64", val: int64(24434), has: true, want: 24434},
		{name: "uint64", val: uint64(24435), has: true, want: 24435},
		{name: "json float64", val: float64(24436), has: true, want: 24436},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgMap := map[string]any{}
			if tc.has {
				cfgMap["port"] = tc.val
			}
			var cfg SnellConfig
			require.NoError(t, cfg.Decode(cfgMap))
			assert.Equal(t, tc.want, cfg.Port)
		})
	}
}
