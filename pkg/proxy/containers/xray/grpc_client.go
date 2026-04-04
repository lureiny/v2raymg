// Package xray provides Xray container implementation.
package xray

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/app/proxyman"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/app/proxyman/command"
	"github.com/lureiny/v2raymg/pkg/xrayapi/types"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/common/serial"
	proxycfg "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/shadowsocks_2022"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/trojan"
	vlessaccount "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vless"
	vlessinbound "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vless/inbound"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vmess"
	vmessinbound "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vmess/inbound"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/tls"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/reality"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/websocket"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/tcp"
	grpcpkg "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// statsRegex matches xray stats output pattern.
var statsRegex = regexp.MustCompile(`(user|inbound|outbound)>>>(\S+)>>>traffic>>>(downlink|uplink)`)

// typeRegistry maps protobuf type names to proto.Message factory functions.
// This is used to deserialize TypedMessage.Value for debug output.
var typeRegistry = map[string]func() proto.Message{
	"xray.app.proxyman.ReceiverConfig":              func() proto.Message { return &proxyman.ReceiverConfig{} },
	"xray.proxy.vmess.inbound.Config":              func() proto.Message { return &vmessinbound.Config{} },
	"xray.proxy.vless.inbound.Config":              func() proto.Message { return &vlessinbound.Config{} },
	"xray.proxy.trojan.ServerConfig":               func() proto.Message { return &trojan.ServerConfig{} },
	"xray.proxy.shadowsocks_2022.ServerConfig":    func() proto.Message { return &proxycfg.ServerConfig{} },
	"xray.transport.internet.StreamConfig":        func() proto.Message { return &internet.StreamConfig{} },
	"xray.transport.internet.tls.Config":           func() proto.Message { return &tls.Config{} },
	"xray.transport.internet.reality.Config":       func() proto.Message { return &reality.Config{} },
	"xray.transport.internet.websocket.Config":     func() proto.Message { return &websocket.Config{} },
	"xray.transport.internet.tcp.Config":           func() proto.Message { return &tcp.Config{} },
	"xray.transport.internet.grpc.Config":           func() proto.Message { return &grpcpkg.Config{} },
	"xray.proxy.vmess.Account":                      func() proto.Message { return &vmess.Account{} },
	"xray.proxy.vless.Account":                      func() proto.Message { return &vlessaccount.Account{} },
	"xray.proxy.trojan.Account":                    func() proto.Message { return &trojan.Account{} },
}

// XrayAPI provides gRPC-based runtime operations for Xray.
type XrayAPI struct {
	address string
	debug   bool
}

// NewXrayAPI creates a new Xray API client.
func NewXrayAPI(address string, debug ...bool) *XrayAPI {
	if address == "" {
		address = "127.0.0.1:62789"
	}
	enableDebug := len(debug) > 0 && debug[0]
	return &XrayAPI{address: address, debug: enableDebug}
}

// WaitForReady waits for the xray gRPC service to be ready.
func (api *XrayAPI) WaitForReady(timeout time.Duration) error {
	start := time.Now()
	baseDelay := 50 * time.Millisecond
	maxDelay := 200 * time.Millisecond

	for {
		conn, err := grpc.Dial(api.address,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithTimeout(200*time.Millisecond),
		)
		if err == nil {
			conn.Close()
			return nil
		}

		if time.Since(start) >= timeout {
			return fmt.Errorf("timeout waiting for gRPC service: %w", err)
		}

		delay := baseDelay
		baseDelay = baseDelay * 2
		if delay > maxDelay {
			delay = maxDelay
		}
		time.Sleep(delay)
	}
}

// AddInbound adds an inbound to the running xray instance via gRPC.
// Uses typed gRPC stub for proper protobuf serialization.
func (api *XrayAPI) AddInbound(nativeInboundJSON []byte) error {
	// Parse JSON to extract key fields from native JSON (for debug output)
	var nativeMap map[string]interface{}
	if err := json.Unmarshal(nativeInboundJSON, &nativeMap); err == nil {
		if api.debug {
			// Output 1: Original nativeInboundJSON (compact single line)
			compactJSON, _ := json.Marshal(nativeMap)
			fmt.Printf("[DEBUG] AddInbound: nativeInboundJSON = %s\n", string(compactJSON))

			// Output 2: ParseInboundConfig key fields (tag/port/protocol)
			fmt.Printf("[DEBUG] AddInbound: parsed config - tag=%v, port=%v, protocol=%v\n",
				nativeMap["tag"], nativeMap["port"], nativeMap["protocol"])

			// Print streamSettings and tlsSettings from native JSON
			if ss, ok := nativeMap["streamSettings"].(map[string]interface{}); ok {
				fmt.Printf("[DEBUG] AddInbound: streamSettings.security = %v\n", ss["security"])
			}
			if ts, ok := nativeMap["tlsSettings"].(map[string]interface{}); ok {
				fmt.Printf("[DEBUG] AddInbound: tlsSettings - certFile=%v, keyFile=%v, allowInsecure=%v\n",
					ts["certificates"], ts["serverName"], ts["alpn"])
			}
		}
	}

	// Parse JSON to protobuf config format
	inboundConfig, err := types.ParseInboundConfig(nativeInboundJSON)
	if err != nil {
		return fmt.Errorf("failed to parse inbound config: %w", err)
	}

	// Create typed request
	req := &command.AddInboundRequest{
		Inbound: inboundConfig,
	}

	// Output 3: protojson serialization of req.Inbound (for detailed inspection)
	if api.debug {
		protoJSON, err := protojson.MarshalOptions{
			Multiline:       true,
			EmitUnpopulated: false,
		}.Marshal(req.Inbound)
		if err == nil {
			fmt.Printf("[DEBUG] AddInbound: req.Inbound (protojson) =\n%s\n", string(protoJSON))
		}

		// Output 4: Deserialize ReceiverSettings for readable debug output
		if req.Inbound.ReceiverSettings != nil {
			api.debugDumpTypedMessage("ReceiverSettings", req.Inbound.ReceiverSettings)
		}

		// Output 5: Deserialize ProxySettings for readable debug output
		if req.Inbound.ProxySettings != nil {
			api.debugDumpTypedMessage("ProxySettings", req.Inbound.ProxySettings)
		}

		// Output 6: TLS effectiveness pre-assertion (using proper protobuf deserialization)
		api.checkTLSEffectivenessV2(req.Inbound)
	}

	// Initial delay to allow xray gRPC service to stabilize
	time.Sleep(500 * time.Millisecond)

	if api.debug {
		fmt.Printf("[DEBUG] AddInbound: attempting to add inbound to %s\n", api.address)
	}

	// Retry logic for gRPC connection - reduced for faster failure
	maxRetries := 3
	baseDelay := 200 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		if api.debug {
			fmt.Printf("[DEBUG] AddInbound: attempt %d/%d\n", attempt+1, maxRetries)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, err := grpc.DialContext(ctx, api.address,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:    10 * time.Second,
				Timeout: 5 * time.Second,
			}),
		)
		if err != nil {
			cancel()
			if attempt < maxRetries-1 {
				delay := baseDelay * time.Duration(attempt+1)
				time.Sleep(delay)
				continue
			}
			return fmt.Errorf("failed to connect to xray API: %w", err)
		}

		// Create typed gRPC client
		client := command.NewHandlerServiceClient(conn)

		// Use typed method call with context
		_, err = client.AddInbound(metadata.NewOutgoingContext(ctx, metadata.Pairs()), req)
		conn.Close()
		cancel()

		if err == nil {
			if api.debug {
				fmt.Printf("[DEBUG] AddInbound: SUCCESS on attempt %d\n", attempt+1)
			}
			return nil
		}

		if api.debug {
			fmt.Printf("[DEBUG] AddInbound: attempt %d failed after timeout: %v\n", attempt+1, err)
		}

		// Check if error is retryable
		errMsg := err.Error()
		isRetryable := strings.Contains(errMsg, "connection error") ||
			strings.Contains(errMsg, "use of closed network connection") ||
			strings.Contains(errMsg, " UNAVAILABLE") ||
			strings.Contains(errMsg, "error reading server preface") ||
			strings.Contains(errMsg, "cannot assign requested address")

		if !isRetryable || attempt >= maxRetries-1 {
			errMsg := formatGRPCError(err)
			return fmt.Errorf("failed to add inbound: %s", errMsg)
		}

		delay := baseDelay * time.Duration(attempt+1)
		time.Sleep(delay)
	}

	return fmt.Errorf("failed to add inbound after %d attempts", maxRetries)
}

// formatGRPCError extracts detailed error information from gRPC errors.
func formatGRPCError(err error) string {
	var sb strings.Builder
	if st, ok := status.FromError(err); ok {
		sb.WriteString(fmt.Sprintf("code=%s, message=%s", st.Code().String(), st.Message()))
		if len(st.Details()) > 0 {
			sb.WriteString(fmt.Sprintf(", details=%v", st.Details()))
		}
	} else {
		sb.WriteString(err.Error())
	}
	return sb.String()
}

// ListInbounds lists all inbounds from the running xray instance via gRPC.
func (api *XrayAPI) ListInbounds() ([]*proxyman.InboundHandlerConfig, error) {
	conn, err := grpc.Dial(api.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to xray API: %w", err)
	}
	defer conn.Close()

	client := command.NewHandlerServiceClient(conn)
	rsp, err := client.ListInbounds(
		metadata.NewOutgoingContext(context.Background(), metadata.Pairs()),
		&command.ListInboundsRequest{},
	)
	if err != nil {
		return nil, fmt.Errorf("ListInbounds gRPC failed: %w", err)
	}
	return rsp.GetInbounds(), nil
}

// RemoveInbound removes an inbound from the running xray instance via gRPC.
func (api *XrayAPI) RemoveInbound(tag string) error {
	// Create typed request
	req := &command.RemoveInboundRequest{
		Tag: tag,
	}

	conn, err := grpc.Dial(api.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to xray API: %w", err)
	}
	defer conn.Close()

	// Create typed gRPC client
	client := command.NewHandlerServiceClient(conn)

	// Use typed method call
	_, err = client.RemoveInbound(metadata.NewOutgoingContext(context.Background(), metadata.Pairs()), req)
	return err
}

// AddUser adds a user to an existing inbound via gRPC.
func (api *XrayAPI) AddUser(tag string, user contracts.UserSpec) error {
	var accountType string
	var accountProto proto.Message

	// Use AuthToken as the credential; default to VMess with a placeholder UUID.
	// This method is currently unused in the production path (users are managed via forward ports).
	accountType = "xray.proxy.trojan.Account"
	password := user.AuthToken
	if password == "" {
		password = "password"
	}
	accountProto = &trojan.Account{Password: password}

	// Build typed operation using types package
	operation, err := types.NewAddUserOperation(user.Username, 0, accountType, accountProto)
	if err != nil {
		return fmt.Errorf("failed to create add user operation: %w", err)
	}

	// Create typed AlterInboundRequest
	alterReq := &command.AlterInboundRequest{
		Tag:       tag,
		Operation: operation,
	}

	conn, err := grpc.Dial(api.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to xray API: %w", err)
	}
	defer conn.Close()

	// Create typed gRPC client
	client := command.NewHandlerServiceClient(conn)

	// Use typed method call
	_, err = client.AlterInbound(metadata.NewOutgoingContext(context.Background(), metadata.Pairs()), alterReq)
	if err != nil {
		errMsg := formatGRPCError(err)
		return fmt.Errorf("failed to add user: %s", errMsg)
	}
	return nil
}

// RemoveUser removes a user from an inbound via gRPC.
func (api *XrayAPI) RemoveUser(tag, email string) error {
	// Build typed operation
	operation, err := types.NewRemoveUserOperation(email)
	if err != nil {
		return fmt.Errorf("failed to create remove user operation: %w", err)
	}

	// Create typed AlterInboundRequest
	alterReq := &command.AlterInboundRequest{
		Tag:       tag,
		Operation: operation,
	}

	conn, err := grpc.Dial(api.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to xray API: %w", err)
	}
	defer conn.Close()

	// Create typed gRPC client
	client := command.NewHandlerServiceClient(conn)

	// Use typed method call
	_, err = client.AlterInbound(metadata.NewOutgoingContext(context.Background(), metadata.Pairs()), alterReq)
	if err != nil {
		errMsg := formatGRPCError(err)
		return fmt.Errorf("failed to remove user: %s", errMsg)
	}
	return nil
}

// QueryStats queries traffic statistics from xray via gRPC.
// Note: StatsService is not available in generated proto, using JSON fallback.
func (api *XrayAPI) QueryStats(pattern string, reset bool) (map[string]*contracts.Stats, error) {
	conn, err := grpc.Dial(api.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to xray API: %w", err)
	}
	defer conn.Close()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs())

	type QueryStatsRequest struct {
		Pattern string `json:"pattern"`
		Reset   bool   `json:"reset"`
	}
	reqData, _ := json.Marshal(QueryStatsRequest{Pattern: pattern, Reset: reset})

	var response []byte
	err = conn.Invoke(ctx, "/xray.app.stats.command.HandlerService/QueryStats", reqData, &response)
	if err != nil {
		return nil, err
	}

	type Stat struct {
		Name    string `json:"name"`
		Counter int64  `json:"value"`
	}
	type QueryStatsResponse struct {
		Stat []*Stat `json:"stat"`
	}

	var resp QueryStatsResponse
	if err := json.Unmarshal(response, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse stats response: %w", err)
	}

	result := make(map[string]*contracts.Stats)
	for _, s := range resp.Stat {
		parts := strings.Split(s.Name, ">>>")
		if len(parts) < 4 {
			continue
		}
		kind := parts[0]
		user := parts[1]
		direction := parts[3]

		key := kind + ">>>" + user
		if _, ok := result[key]; !ok {
			result[key] = &contracts.Stats{}
		}

		if direction == "uplink" {
			result[key].Uplink = s.Counter
		} else if direction == "downlink" {
			result[key].Downlink = s.Counter
		}
	}

	return result, nil
}

// HTTPStats queries stats via xray's HTTP API (fallback).
func (api *XrayAPI) HTTPStats(pattern string) (map[string]*contracts.Stats, error) {
	url := fmt.Sprintf("http://%s/stats/query?pattern=%s", api.address, pattern)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP stats query failed: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	stats := make(map[string]*contracts.Stats)
	if stat, ok := result["stat"].([]interface{}); ok {
		for _, s := range stat {
			if m, ok := s.(map[string]interface{}); ok {
				name, _ := m["name"].(string)
				value, _ := m["value"].(float64)
				parts := strings.Split(name, ">>>")
				if len(parts) < 4 {
					continue
				}
				kind := parts[0]
				user := parts[1]
				direction := parts[3]

				key := kind + ">>>" + user
				if _, ok := stats[key]; !ok {
					stats[key] = &contracts.Stats{}
				}

				if direction == "uplink" {
					stats[key].Uplink = int64(value)
				} else if direction == "downlink" {
					stats[key].Downlink = int64(value)
				}
			}
		}
	}

	return stats, nil
}

// checkTLSEffectiveness checks if TLS configuration is properly set for inbounds
// that expect TLS (tag contains "-tls" but lacks proper TLS config).
func (api *XrayAPI) checkTLSEffectiveness(inbound *proxyman.InboundHandlerConfig) {
	if inbound == nil {
		return
	}

	tag := inbound.Tag
	expectsTLS := strings.Contains(tag, "-tls")

	// Check if ReceiverSettings contains TLS configuration
	hasTLSSecurity := false
	hasCertificates := false

	if inbound.ReceiverSettings != nil {
		// Try to parse as generic JSON to find TLS-related fields
		// The ReceiverSettings is a TypedMessage with type "xray.transport.internet.StreamConfig"
		if inbound.ReceiverSettings.Value != nil {
			var streamConfig map[string]interface{}
			if err := json.Unmarshal(inbound.ReceiverSettings.Value, &streamConfig); err == nil {
				// Check for security field in stream settings
				if security, ok := streamConfig["security"].(string); ok && security == "tls" {
					hasTLSSecurity = true
				}
				// Check for TLS settings
				if tlsSettings, ok := streamConfig["tlsSettings"].(map[string]interface{}); ok {
					if certs, ok := tlsSettings["certificates"].([]interface{}); ok && len(certs) > 0 {
						hasCertificates = true
					}
				}
			}
		}
	}

	// Print assertion result
	if expectsTLS && (!hasTLSSecurity || !hasCertificates) {
		log.Error("TLS effectiveness assertion failed",
			"tag", tag,
			"has_tls_security", hasTLSSecurity,
			"has_certificates", hasCertificates,
			"hint", "check cert_file/key_file are properly passed and parsed",
		)
	} else if api.debug && expectsTLS && hasTLSSecurity && hasCertificates {
		fmt.Printf("[DEBUG] TLS Effectiveness Assertion PASSED for tag=%s\n", tag)
	}
}

// debugDumpTypedMessage deserializes and outputs a TypedMessage as readable JSON.
func (api *XrayAPI) debugDumpTypedMessage(label string, tm *serial.TypedMessage) {
	if tm == nil {
		fmt.Printf("[DEBUG] %s: (nil)\n", label)
		return
	}
	if tm.Value == nil {
		fmt.Printf("[DEBUG] %s: type=%s, value=(empty)\n", label, tm.Type)
		return
	}

	// Try to find the type in registry
	msgFactory, found := typeRegistry[tm.Type]
	if !found {
		// Fallback: output type and hex dump
		hexLen := len(tm.Value)
		hexPreview := ""
		if hexLen > 64 {
			hexPreview = hex.EncodeToString(tm.Value[:64]) + "..."
		} else {
			hexPreview = hex.EncodeToString(tm.Value)
		}
		fmt.Printf("[DEBUG] %s: type=%s, value=<%d bytes, hex: %s>\n",
			label, tm.Type, hexLen, hexPreview)
		return
	}

	// Deserialize the protobuf message
	msg := msgFactory()
	if err := proto.Unmarshal(tm.Value, msg); err != nil {
		fmt.Printf("[DEBUG] %s: type=%s, unmarshal error: %v\n", label, tm.Type, err)
		return
	}

	// Serialize as JSON (base output)
	jsonBytes, err := protojson.MarshalOptions{Multiline: true, EmitUnpopulated: false}.Marshal(msg)
	if err != nil {
		fmt.Printf("[DEBUG] %s: type=%s, json marshal error: %v\n", label, tm.Type, err)
		return
	}

	fmt.Printf("[DEBUG] %s (type=%s):\n%s\n", label, tm.Type, string(jsonBytes))

	// Recursively dump nested TypedMessage fields based on known types
	api.debugDumpNestedTypedMessages(label, msg)
}

// debugDumpNestedTypedMessages recursively handles nested TypedMessage fields in known types.
func (api *XrayAPI) debugDumpNestedTypedMessages(parentLabel string, msg proto.Message) {
	switch m := msg.(type) {
	// ReceiverConfig: StreamSettings -> SecuritySettings / TransportSettings
	case *proxyman.ReceiverConfig:
		if m.StreamSettings != nil {
			// SecuritySettings
			for i, ss := range m.StreamSettings.SecuritySettings {
				if ss != nil {
					api.debugDumpTypedMessage(fmt.Sprintf("%s.StreamSettings.SecuritySettings[%d]", parentLabel, i), ss)
				}
			}
			// TransportSettings
			for i, ts := range m.StreamSettings.TransportSettings {
				if ts != nil && ts.Settings != nil {
					api.debugDumpTypedMessage(fmt.Sprintf("%s.StreamSettings.TransportSettings[%d].%s",
						parentLabel, i, ts.ProtocolName), ts.Settings)
				}
			}
		}

	// VMess inbound Config: User[*].Account
	case *vmessinbound.Config:
		for i, u := range m.User {
			if u != nil && u.Account != nil {
				email := u.Email
				if email == "" {
					email = fmt.Sprintf("user%d", i)
				}
				api.debugDumpTypedMessage(fmt.Sprintf("%s.User[%d].%s.Account", parentLabel, i, email), u.Account)
			}
		}

	// VLESS inbound Config: Clients[*].Account
	case *vlessinbound.Config:
		for i, u := range m.Clients {
			if u != nil && u.Account != nil {
				email := u.Email
				if email == "" {
					email = fmt.Sprintf("client%d", i)
				}
				api.debugDumpTypedMessage(fmt.Sprintf("%s.Clients[%d].%s.Account", parentLabel, i, email), u.Account)
			}
		}

	// Trojan Server Config: Users[*].Account
	case *trojan.ServerConfig:
		for i, u := range m.Users {
			if u != nil && u.Account != nil {
				email := u.Email
				if email == "" {
					email = fmt.Sprintf("user%d", i)
				}
				api.debugDumpTypedMessage(fmt.Sprintf("%s.Users[%d].%s.Account", parentLabel, i, email), u.Account)
			}
		}

	// Shadowsocks 2022 Server Config: Users[*].Account
	case *proxycfg.ServerConfig:
		// SS2022 doesn't have nested TypedMessage accounts, skip
	}
}

// debugDumpStreamConfig recursively outputs StreamConfig with SecuritySettings.
func (api *XrayAPI) debugDumpStreamConfig(label string, sc *internet.StreamConfig) {
	if sc == nil {
		fmt.Printf("[DEBUG] %s: (nil)\n", label)
		return
	}

	// Output basic fields
	fmt.Printf("[DEBUG] %s: protocol_name=%s, security_type=%s\n",
		label, sc.ProtocolName, sc.SecurityType)

	// Recursively dump security settings
	for i, ss := range sc.SecuritySettings {
		api.debugDumpTypedMessage(fmt.Sprintf("%s.SecuritySettings[%d]", label, i), ss)
	}

	// Output transport settings (TransportConfig is not TypedMessage, just print basic info)
	for i, ts := range sc.TransportSettings {
		if ts != nil {
			fmt.Printf("[DEBUG] %s.TransportSettings[%d]: protocol_name=%s\n", label, i, ts.ProtocolName)
		}
	}
}

// checkTLSEffectivenessV2 checks TLS configuration using proper protobuf deserialization.
func (api *XrayAPI) checkTLSEffectivenessV2(inbound *proxyman.InboundHandlerConfig) {
	if inbound == nil || inbound.ReceiverSettings == nil {
		return
	}

	tag := inbound.Tag
	expectsTLS := strings.Contains(tag, "-tls")
	if !expectsTLS {
		return
	}

	// Deserialize ReceiverSettings
	receiverConfig := &proxyman.ReceiverConfig{}
	if err := proto.Unmarshal(inbound.ReceiverSettings.Value, receiverConfig); err != nil {
		if api.debug {
			fmt.Printf("[DEBUG] checkTLSEffectivenessV2: failed to unmarshal ReceiverConfig for %s: %v\n", tag, err)
		}
		return
	}

	// Check StreamConfig for TLS
	if receiverConfig.StreamSettings == nil {
		log.Error("TLS effectiveness assertion failed: StreamSettings is nil", "tag", tag)
		return
	}

	hasTLSSecurity := receiverConfig.StreamSettings.SecurityType == "tls" ||
		receiverConfig.StreamSettings.SecurityType == "xray.transport.internet.tls.Config"
	hasCertificates := false

	// Check SecuritySettings for TLS certificates
	for _, ss := range receiverConfig.StreamSettings.SecuritySettings {
		if ss == nil {
			continue
		}
		// Try to deserialize as tls.Config
		if ss.Type == "xray.transport.internet.tls.Config" {
			tlsConfig := &tls.Config{}
			if err := proto.Unmarshal(ss.Value, tlsConfig); err == nil {
				if len(tlsConfig.Certificate) > 0 {
					hasCertificates = true
					if api.debug {
						fmt.Printf("[DEBUG] TLS certificates found for %s: %d certificate(s)\n", tag, len(tlsConfig.Certificate))
					}
				}
			}
		}
	}

	// Print assertion result
	if !hasTLSSecurity || !hasCertificates {
		log.Error("TLS effectiveness assertion failed",
			"tag", tag,
			"has_tls_security", hasTLSSecurity,
			"has_certificates", hasCertificates,
			"hint", "check cert_file/key_file are properly passed and parsed",
		)
	} else if api.debug {
		fmt.Printf("[DEBUG] TLS Effectiveness Assertion PASSED for tag=%s\n", tag)
	}
}
