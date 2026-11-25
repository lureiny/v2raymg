package xray

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedCLICall struct {
	binary string
	args   []string
	stdin  []byte
}

func TestRoutingCLIClient_CommandsAndPayloads(t *testing.T) {
	var calls []recordedCLICall
	runner := func(ctx context.Context, binaryPath string, args []string, stdin []byte) ([]byte, error) {
		calls = append(calls, recordedCLICall{
			binary: binaryPath,
			args:   append([]string(nil), args...),
			stdin:  append([]byte(nil), stdin...),
		})
		return []byte("ok"), nil
	}

	cli := newRoutingCLIClient("/usr/bin/xray", "127.0.0.1:62789", runner)
	ctx := context.Background()

	// AddOutbound
	outJSON := []byte(`{"tag":"__chain_out__:hk1","protocol":"vless","domainStrategy":"AsIs"}`)
	require.NoError(t, cli.AddOutbound(ctx, outJSON))

	// AddRule
	require.NoError(t, cli.AddRule(ctx, chainRuleTag("hk1"), "hk1", chainOutboundTag("hk1")))

	// RemoveRule
	require.NoError(t, cli.RemoveRule(ctx, chainRuleTag("hk1")))

	// RemoveOutbound
	require.NoError(t, cli.RemoveOutbound(ctx, chainOutboundTag("hk1")))

	require.Len(t, calls, 4)

	// 1. ado
	assert.Equal(t, "/usr/bin/xray", calls[0].binary)
	assert.Equal(t, []string{"api", "ado", "--server=127.0.0.1:62789", "stdin:"}, calls[0].args)
	var adoPayload struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	require.NoError(t, json.Unmarshal(calls[0].stdin, &adoPayload))
	require.Len(t, adoPayload.Outbounds, 1)
	assert.Equal(t, "__chain_out__:hk1", adoPayload.Outbounds[0]["tag"])
	assert.Equal(t, "AsIs", adoPayload.Outbounds[0]["domainStrategy"])

	// 2. adrules -append
	assert.Equal(t, []string{"api", "adrules", "-append", "--server=127.0.0.1:62789", "stdin:"}, calls[1].args)
	var adrulesPayload struct {
		Routing struct {
			Rules []struct {
				Type        string   `json:"type"`
				RuleTag     string   `json:"ruleTag"`
				InboundTag  []string `json:"inboundTag"`
				OutboundTag string   `json:"outboundTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	require.NoError(t, json.Unmarshal(calls[1].stdin, &adrulesPayload))
	require.Len(t, adrulesPayload.Routing.Rules, 1)
	rule := adrulesPayload.Routing.Rules[0]
	assert.Equal(t, "field", rule.Type)
	assert.Equal(t, "__chain_rule__:hk1", rule.RuleTag)
	assert.Equal(t, []string{"hk1"}, rule.InboundTag)
	assert.Equal(t, "__chain_out__:hk1", rule.OutboundTag)

	// 3. rmrules
	assert.Equal(t, []string{"api", "rmrules", "--server=127.0.0.1:62789", "__chain_rule__:hk1"}, calls[2].args)
	assert.Nil(t, calls[2].stdin)

	// 4. rmo
	assert.Equal(t, []string{"api", "rmo", "--server=127.0.0.1:62789", "__chain_out__:hk1"}, calls[3].args)
	assert.Nil(t, calls[3].stdin)
}

func TestRoutingCLIClient_ValidationAndErrorHandling(t *testing.T) {
	runnerErr := errors.New("exit status 2")
	failRunner := func(ctx context.Context, binaryPath string, args []string, stdin []byte) ([]byte, error) {
		return []byte("rpc error: code = Unavailable"), runnerErr
	}

	cli := newRoutingCLIClient("/usr/bin/xray", "", failRunner)
	assert.Equal(t, "127.0.0.1:62789", cli.serverAddr)

	err := cli.AddOutbound(context.Background(), []byte(`{"tag":"x"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xray api ado failed")
	assert.Contains(t, err.Error(), "Unavailable")
	assert.NotContains(t, err.Error(), `"tag":"x"`)

	err = cli.AddOutbound(context.Background(), []byte(`not-json`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid outbound JSON")

	err = cli.AddRule(context.Background(), "", "in", "out")
	require.Error(t, err)

	err = cli.RemoveRule(context.Background(), "r1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rmrules")

	err = cli.RemoveOutbound(context.Background(), "o1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rmo")

	noBinCLI := newRoutingCLIClient("", "127.0.0.1:62789", failRunner)
	assert.Error(t, noBinCLI.RemoveRule(context.Background(), "r1"))
	assert.Error(t, noBinCLI.RemoveOutbound(context.Background(), "o1"))

	// Ensure naming conventions match design
	assert.True(t, strings.HasPrefix(chainOutboundTag("abc"), "__chain_out__:"))
	assert.True(t, strings.HasPrefix(chainRuleTag("abc"), "__chain_rule__:"))
}
