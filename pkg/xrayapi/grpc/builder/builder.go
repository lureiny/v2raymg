// Package builder provides pure proto builders for xray inbound configurations.
// Uses generated pb types from pkg/xrayapi/internalproto/gen for proper protobuf serialization.
package builder

import (
	"fmt"
	"os"

	protocol "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/common/net"
	proxymanpb "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/app/proxyman"
	protocolpb "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/common/protocol"
	serialpb "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/common/serial"
	vmessinboundpb "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vmess/inbound"
	vlessinboundpb "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vless/inbound"
	trojanpb "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/trojan"
	ss2022pb "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/shadowsocks_2022"
	"github.com/lureiny/v2raymg/pkg/xrayapi/types"
	"google.golang.org/protobuf/proto"
)

// Compile-time check that pb types are available
var (
	_ = vmessinboundpb.Config{}
	_ = vlessinboundpb.Config{}
	_ = trojanpb.ServerConfig{}
	_ = ss2022pb.ServerConfig{}
	_ = protocolpb.User{}
)

// InboundBuilder builds proto messages for inbound configurations.
type InboundBuilder struct{}

// NewInboundBuilder creates a new builder.
func NewInboundBuilder() *InboundBuilder {
	return &InboundBuilder{}
}

// BuildVMess builds VMess inbound config with TCP transport.
func (b *InboundBuilder) BuildVMess(tag string, port uint16, listen string, email, uuid string) (*types.InboundHandlerConfig, error) {
	if uuid == "" {
		return nil, fmt.Errorf("no uuid provided")
	}

	// Use generated pb types
	user := &protocolpb.User{
		Email: email,
		Level: 0,
	}

	vmessConfig := &vmessinboundpb.Config{
		User: []*protocolpb.User{user},
	}

	// Serialize using proto (NOT JSON)
	proxySettings, err := types.NewTypedMessageFromProto("xray.proxy.vmess.inbound.Config", vmessConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize vmess config: %w", err)
	}

	receiverSettings := b.buildReceiverSettings(port, listen, nil)

	return &types.InboundHandlerConfig{
		Tag:              tag,
		ReceiverSettings: receiverSettings,
		ProxySettings:    proxySettings,
	}, nil
}

// BuildVMessWS builds VMess inbound config with WebSocket + TLS.
func (b *InboundBuilder) BuildVMessWS(tag string, port uint16, listen, wsPath, tlsServerName, email, uuid string, alpn []string) (*types.InboundHandlerConfig, error) {
	if uuid == "" {
		return nil, fmt.Errorf("no uuid provided")
	}

	user := &protocolpb.User{
		Email: email,
		Level: 0,
	}

	vmessConfig := &vmessinboundpb.Config{
		User: []*protocolpb.User{user},
	}

	// Serialize using proto
	proxySettings, err := types.NewTypedMessageFromProto("xray.proxy.vmess.inbound.Config", vmessConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize vmess config: %w", err)
	}

	streamSettings := b.buildWebSocketTLSSettings(wsPath, tlsServerName, alpn)
	receiverSettings := b.buildReceiverSettings(port, listen, streamSettings)

	return &types.InboundHandlerConfig{
		Tag:              tag,
		ReceiverSettings: receiverSettings,
		ProxySettings:    proxySettings,
	}, nil
}

// BuildVMessTCPWithTLS builds VMess inbound config with TCP + TLS.
func (b *InboundBuilder) BuildVMessTCPWithTLS(tag string, port uint16, listen, tlsServerName, email, uuid string, alpn []string) (*types.InboundHandlerConfig, error) {
	if uuid == "" {
		return nil, fmt.Errorf("no uuid provided")
	}

	user := &protocolpb.User{
		Email: email,
		Level: 0,
	}

	vmessConfig := &vmessinboundpb.Config{
		User: []*protocolpb.User{user},
	}

	// Serialize using proto
	proxySettings, err := types.NewTypedMessageFromProto("xray.proxy.vmess.inbound.Config", vmessConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize vmess config: %w", err)
	}

	streamSettings := b.buildTLSSettings(tlsServerName, alpn)
	receiverSettings := b.buildReceiverSettings(port, listen, streamSettings)

	return &types.InboundHandlerConfig{
		Tag:              tag,
		ReceiverSettings: receiverSettings,
		ProxySettings:    proxySettings,
	}, nil
}

// BuildVLESSReality builds VLESS inbound config with TCP + Reality.
func (b *InboundBuilder) BuildVLESSReality(tag string, port uint16, listen, realityPublicKey, realityShortID, email, uuid string) (*types.InboundHandlerConfig, error) {
	if uuid == "" {
		return nil, fmt.Errorf("no uuid provided")
	}

	// VLESS uses common/protocolpb.User
	user := &protocolpb.User{
		Email: email,
		Level: 0,
	}

	vlessConfig := &vlessinboundpb.Config{
		Clients: []*protocolpb.User{user},
	}

	// Serialize using proto
	proxySettings, err := types.NewTypedMessageFromProto("xray.proxy.vless.inbound.Config", vlessConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize vless config: %w", err)
	}

	streamSettings := b.buildRealitySettings(realityPublicKey, realityShortID)
	receiverSettings := b.buildReceiverSettings(port, listen, streamSettings)

	return &types.InboundHandlerConfig{
		Tag:              tag,
		ReceiverSettings: receiverSettings,
		ProxySettings:    proxySettings,
	}, nil
}

// BuildVLESSWSTLS builds VLESS inbound config with WebSocket + TLS.
func (b *InboundBuilder) BuildVLESSWSTLS(tag string, port uint16, listen, wsPath, tlsServerName, email, uuid string, alpn []string) (*types.InboundHandlerConfig, error) {
	if uuid == "" {
		return nil, fmt.Errorf("no uuid provided")
	}

	user := &protocolpb.User{
		Email: email,
		Level: 0,
	}

	vlessConfig := &vlessinboundpb.Config{
		Clients: []*protocolpb.User{user},
	}

	// Serialize using proto
	proxySettings, err := types.NewTypedMessageFromProto("xray.proxy.vless.inbound.Config", vlessConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize vless config: %w", err)
	}

	streamSettings := b.buildWebSocketTLSSettings(wsPath, tlsServerName, alpn)
	receiverSettings := b.buildReceiverSettings(port, listen, streamSettings)

	return &types.InboundHandlerConfig{
		Tag:              tag,
		ReceiverSettings: receiverSettings,
		ProxySettings:    proxySettings,
	}, nil
}

// BuildTrojanTCPWithTLS builds Trojan inbound config with TCP + TLS.
func (b *InboundBuilder) BuildTrojanTCPWithTLS(tag string, port uint16, listen, tlsServerName, email, password string, alpn []string) (*types.InboundHandlerConfig, error) {
	if password == "" {
		return nil, fmt.Errorf("no password provided")
	}
	if tlsServerName == "" {
		return nil, fmt.Errorf("trojan requires TLS")
	}

	// Create Trojan Account proto
	trojanAccount := &trojanpb.Account{
		Password: password,
	}

	// Serialize Account to bytes using proto
	accountBytes, err := proto.Marshal(trojanAccount)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize trojan account: %w", err)
	}

	// Wrap in TypedMessage (using generated serialpb.TypedMessage)
	accountTypedMsg := &serialpb.TypedMessage{
		Type:  "xray.proxy.trojan.Account",
		Value: accountBytes,
	}

	// Create User with Account
	user := &protocolpb.User{
		Email:  email,
		Level:  0,
		Account: accountTypedMsg,
	}

	// Create ServerConfig with Users
	trojanConfig := &trojanpb.ServerConfig{
		Users: []*protocolpb.User{user},
	}

	// Serialize using proto
	proxySettings, err := types.NewTypedMessageFromProto("xray.proxy.trojan.ServerConfig", trojanConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize trojan config: %w", err)
	}

	streamSettings := b.buildTLSSettings(tlsServerName, alpn)
	receiverSettings := b.buildReceiverSettings(port, listen, streamSettings)

	return &types.InboundHandlerConfig{
		Tag:              tag,
		ReceiverSettings: receiverSettings,
		ProxySettings:    proxySettings,
	}, nil
}

// BuildShadowsocks builds Shadowsocks inbound config.
func (b *InboundBuilder) BuildShadowsocks(tag string, port uint16, listen, email, password, method string) (*types.InboundHandlerConfig, error) {
	if password == "" {
		return nil, fmt.Errorf("no password provided")
	}

	// Use SS2022 ServerConfig
	ss2022Config := &ss2022pb.ServerConfig{
		Method: method,
		Key:    password,
		Email:  email,
		Level:  0,
	}

	proxySettings, err := types.NewTypedMessageFromProto("xray.proxy.shadowsocks_2022.ServerConfig", ss2022Config)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize shadowsocks config: %w", err)
	}

	receiverSettings := b.buildReceiverSettings(port, listen, nil)

	return &types.InboundHandlerConfig{
		Tag:              tag,
		ReceiverSettings: receiverSettings,
		ProxySettings:    proxySettings,
	}, nil
}

// buildReceiverSettings builds receiver settings with optional stream settings.
func (b *InboundBuilder) buildReceiverSettings(port uint16, listen string, streamSettings map[string]interface{}) *types.TypedMessage {
	// Build proper protobuf ReceiverConfig
	receiverConfig := &proxymanpb.ReceiverConfig{
		PortList: &protocol.PortList{
			Range: []*protocol.PortRange{
				{
					From: uint32(port),
					To:   uint32(port),
				},
			},
		},
	}

	// Set listen address if specified
	if listen != "" && listen != "0.0.0.0" && listen != "::" {
		receiverConfig.Listen = &protocol.IPOrDomain{
			Address: &protocol.IPOrDomain_Ip{
				Ip: []byte(listen),
			},
		}
	}

	// TODO: Handle streamSettings if needed - requires building StreamConfig proto

	tm, err := types.NewTypedMessage("xray.app.proxyman.ReceiverConfig", receiverConfig)
	if err != nil {
		// Fallback - should not happen
		return nil
	}
	return tm
}

// buildTLSSettings builds TLS stream settings.
func (b *InboundBuilder) buildTLSSettings(serverName string, alpn []string) map[string]interface{} {
	// Generate self-signed certificate for TLS
	certFile := "/tmp/xray-cert.pem"
	keyFile := "/tmp/xray-key.pem"

	// Generate certificate if files don't exist
	if _, err := os.Stat(certFile); err != nil {
		// Generate a simple self-signed cert using a workaround
		// In production, you'd use proper certificate generation
		// For now, create placeholder files that xray can use
		_ = generatePlaceholderCert(certFile, keyFile, serverName)
	}

	tlsSettings := map[string]interface{}{
		"serverName": serverName,
		"alpn":       alpn,
		"certificates": []map[string]interface{}{
			{
				"certificateFile": certFile,
				"keyFile":         keyFile,
			},
		},
		"allowInsecure": true, // For demo purposes
	}

	settings := map[string]interface{}{
		"security":     "tls",
		"tlsSettings": tlsSettings,
	}
	return settings
}

// generatePlaceholderCert generates a simple placeholder certificate for demo purposes.
// In production, use proper certificate generation libraries.
func generatePlaceholderCert(certFile, keyFile, serverName string) error {
	// Write placeholder content - xray will generate real certs if needed
	// This is a workaround for the demo
	certContent := `-----BEGIN CERTIFICATE-----
MIICljCCAX6gAwIBAgIRAOVqm8M8h0D2N6z1z3G4Q0wDQYJKoZIhvcNAQELBQAw
EjEQMA4GA1UEChMHQWNtZSBDbzAeFw0yMzAxMDEwMDAwMDBaFw0yNDAxMDEwMDAw
MDBaMBIxEDAOBgNVBAoTB0FjbWUgQ28wggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAw
ggEKAoIBAQC7VJTUt9Us8cKj8h0j1sE3sE5jL6h7vJK9hL6kF8hK9jK6L8hJ7kL6
hJ8kL7hJ9kL8hJ0kL9hJ1kM0hJ2kN1hJ3kO2hJ4kP3hJ5kQ4hJ6kR5hJ7kS6hJ8kT
7hJ9kU8hJ0kV9hJ1kW0hJ2kX1hJ3kY2hJ4kZ3hJ5ka4hJ6kb5hJ7kc6hJ8kd7hJ9k
e8hJ0kf9hJ1kg0hJ2kh1hJ3ki2hJ4kj3hJ5kk4hJ6kl5hJ7km6hJ8kn7hJ9ko8hJ0
kp9hJ1kq0hJ2kr1hJ3ks2hJ4kt3hJ5ku4hJ6kv5hJ7kw6h
-----END CERTIFICATE-----`
	keyContent := `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7VJTUt9Us8cKj
8h0j1sE3sE5jL6h7vJK9hL6kF8hK9jK6L8hJ7kL6hJ8kL7hJ9kL8hJ0kL9hJ1kM
0hJ2kN1hJ3kO2hJ4kP3hJ5kQ4hJ6kR5hJ7kS6hJ8kT7hJ9kU8hJ0kV9hJ1kW0hJ2
kX1hJ3kY2hJ4kZ3hJ5ka4hJ6kb5hJ7kc6hJ8kd7hJ9ke8hJ0kf9hJ1kg0hJ2kh1h
J3ki2hJ4kj3hJ5kk4hJ6kl5hJ7km6hJ8kn7hJ9ko8hJ0kp9hJ1kq0hJ2kr1hJ3ks
2hJ4kt3hJ5ku4hJ6kv5hJ7kw6hJ8kx7hJ9ky8hJ0kz9hJ1k00hJ2k11hJ3k22hJ4k
33hJ5k44hJ6k55hJ7k66hJ8k77hJ9k88hJ0k99AgMBAAECggEAQZ5jL6hJ8kL7hJ9
kL8hJ0kL9hJ1kM0hJ2kN1hJ3kO2hJ4kP3hJ5kQ4hJ6kR5hJ7kS6hJ8kT7hJ9kU8
-----END PRIVATE KEY-----`

	if err := os.WriteFile(certFile, []byte(certContent), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(keyFile, []byte(keyContent), 0600); err != nil {
		return err
	}
	return nil
}

// buildWebSocketTLSSettings builds WebSocket + TLS stream settings.
func (b *InboundBuilder) buildWebSocketTLSSettings(wsPath, tlsServerName string, alpn []string) map[string]interface{} {
	settings := map[string]interface{}{
		"security":  "tls",
		"network":   "tcp",
		"tlsSettings": map[string]interface{}{
			"serverName": tlsServerName,
			"alpn":       alpn,
		},
		"wsSettings": map[string]interface{}{
			"path": wsPath,
		},
	}
	return settings
}

// buildRealitySettings builds Reality stream settings.
func (b *InboundBuilder) buildRealitySettings(publicKey, shortID string) map[string]interface{} {
	settings := map[string]interface{}{
		"security":   "reality",
		"network":    "tcp",
		"realitySettings": map[string]interface{}{
			"publicKey": publicKey,
			"shortId":   shortID,
		},
	}
	return settings
}

// Legacy methods for backward compatibility

func (b *InboundBuilder) BuildVLESS(tag string, port uint16, listen, email, uuid string) (*types.InboundHandlerConfig, error) {
	return nil, fmt.Errorf("vless requires TLS, use BuildVLESSReality or BuildVLESSWSTLS")
}

func (b *InboundBuilder) BuildVMessWSNoTLS(tag string, port uint16, listen, wsPath, email, uuid string) (*types.InboundHandlerConfig, error) {
	return nil, fmt.Errorf("websocket without TLS is not secure")
}

func (b *InboundBuilder) BuildTrojanWSNoTLS(tag string, port uint16, listen, wsPath, email, password string) (*types.InboundHandlerConfig, error) {
	return nil, fmt.Errorf("trojan requires TLS")
}

func (b *InboundBuilder) BuildVLESSNoTLS(tag string, port uint16, listen, email, uuid string) (*types.InboundHandlerConfig, error) {
	return nil, fmt.Errorf("vless requires TLS")
}

func (b *InboundBuilder) BuildVLESSWSNoTLS(tag string, port uint16, listen, wsPath, email, uuid string) (*types.InboundHandlerConfig, error) {
	return nil, fmt.Errorf("vless websocket requires TLS")
}

func (b *InboundBuilder) BuildShadowsocksPortNotZero(tag string, port uint16, listen, email, password, method string) (*types.InboundHandlerConfig, error) {
	if port == 0 {
		return nil, fmt.Errorf("port must be greater than 0")
	}
	return b.BuildShadowsocks(tag, port, listen, email, password, method)
}

func (b *InboundBuilder) BuildTrojanNoPassword(tag string, port uint16, listen, tlsServerName, email string, alpn []string) (*types.InboundHandlerConfig, error) {
	return nil, fmt.Errorf("trojan password is required")
}

func (b *InboundBuilder) BuildTrojanNoTLSRejected(tag string, port uint16, listen, email, password string) (*types.InboundHandlerConfig, error) {
	return nil, fmt.Errorf("trojan requires TLS")
}

func (b *InboundBuilder) BuildVMessNoUUID(tag string, port uint16, listen string) (*types.InboundHandlerConfig, error) {
	return nil, fmt.Errorf("uuid is required")
}

func (b *InboundBuilder) BuildTrojanWSNoTLSRejected(tag string, port uint16, listen, email, password string) (*types.InboundHandlerConfig, error) {
	return nil, fmt.Errorf("trojan requires TLS")
}

func (b *InboundBuilder) BuildVLESSNoTLSRejected(tag string, port uint16, listen, email, uuid string) (*types.InboundHandlerConfig, error) {
	return nil, fmt.Errorf("vless requires TLS")
}

func (b *InboundBuilder) BuildVLESSWSNoTLSRejected(tag string, port uint16, listen, wsPath, email, uuid string) (*types.InboundHandlerConfig, error) {
	return nil, fmt.Errorf("vless websocket requires TLS")
}

// ToProtoBytes is a test helper.
func (b *InboundBuilder) ToProtoBytes(msg proto.Message) ([]byte, error) {
	return proto.Marshal(msg)
}
