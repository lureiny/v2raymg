package xray

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	chainOutboundPrefix = "__chain_out__:"
	chainRulePrefix     = "__chain_rule__:"
	defaultCLITimeout   = 15 * time.Second
)

func chainOutboundTag(inboundTag string) string {
	return chainOutboundPrefix + inboundTag
}

func chainRuleTag(inboundTag string) string {
	return chainRulePrefix + inboundTag
}

// cliRunner executes `binaryPath args...` with optional stdin data and returns
// combined stdout/stderr output.
type cliRunner func(ctx context.Context, binaryPath string, args []string, stdin []byte) ([]byte, error)

func execCLIRunner(ctx context.Context, binaryPath string, args []string, stdin []byte) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultCLITimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

// routingCLIClient wraps `xray api` subcommands for dynamic outbound and routing
// rule management using in-memory stdin pipes (zero temporary credential files).
type routingCLIClient struct {
	binaryPath string
	serverAddr string
	run        cliRunner
}

func newRoutingCLIClient(binaryPath, serverAddr string, runner ...cliRunner) *routingCLIClient {
	if strings.TrimSpace(serverAddr) == "" {
		serverAddr = "127.0.0.1:62789"
	}
	fn := execCLIRunner
	if len(runner) > 0 && runner[0] != nil {
		fn = runner[0]
	}
	return &routingCLIClient{
		binaryPath: binaryPath,
		serverAddr: serverAddr,
		run:        fn,
	}
}

// AddOutbound calls `xray api ado --server=<addr> stdin:` with `{"outbounds":[<outboundJSON>]}`.
func (c *routingCLIClient) AddOutbound(ctx context.Context, outboundJSON []byte) error {
	if strings.TrimSpace(c.binaryPath) == "" {
		return fmt.Errorf("xray routing cli: binary path is empty")
	}
	trimmed := bytes.TrimSpace(outboundJSON)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return fmt.Errorf("xray routing cli: invalid outbound JSON")
	}

	payload := struct {
		Outbounds []json.RawMessage `json:"outbounds"`
	}{
		Outbounds: []json.RawMessage{json.RawMessage(trimmed)},
	}
	stdinBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("xray routing cli: marshal ado payload: %w", err)
	}

	args := []string{"api", "ado", "--server=" + c.serverAddr, "stdin:"}
	out, err := c.run(ctx, c.binaryPath, args, stdinBytes)
	if err != nil {
		return fmt.Errorf("xray api ado failed: %w (%s)", err, summarizeCLIOutput(out))
	}
	return nil
}

// AddRule calls `xray api adrules -append --server=<addr> stdin:` with a wrapped field rule.
func (c *routingCLIClient) AddRule(ctx context.Context, ruleTag, inboundTag, outboundTag string) error {
	if strings.TrimSpace(c.binaryPath) == "" {
		return fmt.Errorf("xray routing cli: binary path is empty")
	}
	if strings.TrimSpace(ruleTag) == "" || strings.TrimSpace(inboundTag) == "" || strings.TrimSpace(outboundTag) == "" {
		return fmt.Errorf("xray routing cli: ruleTag, inboundTag, and outboundTag are required")
	}

	payload := map[string]any{
		"routing": map[string]any{
			"rules": []map[string]any{
				{
					"type":        "field",
					"ruleTag":     ruleTag,
					"inboundTag":  []string{inboundTag},
					"outboundTag": outboundTag,
				},
			},
		},
	}
	stdinBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("xray routing cli: marshal adrules payload: %w", err)
	}

	args := []string{"api", "adrules", "-append", "--server=" + c.serverAddr, "stdin:"}
	out, err := c.run(ctx, c.binaryPath, args, stdinBytes)
	if err != nil {
		return fmt.Errorf("xray api adrules failed: %w (%s)", err, summarizeCLIOutput(out))
	}
	return nil
}

// RemoveRule calls `xray api rmrules --server=<addr> <ruleTag>`.
func (c *routingCLIClient) RemoveRule(ctx context.Context, ruleTag string) error {
	if strings.TrimSpace(c.binaryPath) == "" {
		return fmt.Errorf("xray routing cli: binary path is empty")
	}
	if strings.TrimSpace(ruleTag) == "" {
		return fmt.Errorf("xray routing cli: ruleTag is required")
	}

	args := []string{"api", "rmrules", "--server=" + c.serverAddr, ruleTag}
	out, err := c.run(ctx, c.binaryPath, args, nil)
	if err != nil {
		return fmt.Errorf("xray api rmrules failed: %w (%s)", err, summarizeCLIOutput(out))
	}
	return nil
}

// RemoveOutbound calls `xray api rmo --server=<addr> <outboundTag>`.
func (c *routingCLIClient) RemoveOutbound(ctx context.Context, outboundTag string) error {
	if strings.TrimSpace(c.binaryPath) == "" {
		return fmt.Errorf("xray routing cli: binary path is empty")
	}
	if strings.TrimSpace(outboundTag) == "" {
		return fmt.Errorf("xray routing cli: outboundTag is required")
	}

	args := []string{"api", "rmo", "--server=" + c.serverAddr, outboundTag}
	out, err := c.run(ctx, c.binaryPath, args, nil)
	if err != nil {
		return fmt.Errorf("xray api rmo failed: %w (%s)", err, summarizeCLIOutput(out))
	}
	return nil
}

func summarizeCLIOutput(out []byte) string {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "no output"
	}
	if len(s) > 256 {
		return s[:256] + "..."
	}
	return s
}
