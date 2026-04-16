// Package types provides type definitions and helpers for xray gRPC API.
// All protobuf types are generated locally under pkg/xrayapi/internalproto/gen/.
package types

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdnet "net"
	"os"

	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/app/proxyman"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/common/net"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/common/protocol"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/common/serial"
	httpcfg "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/http"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/shadowsocks_2022"
	sockscfg "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/socks"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/trojan"
	vlessaccount "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vless"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vless/inbound"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vmess"
	vmessinbound "github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vmess/inbound"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/tls"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/reality"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/websocket"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/tcp"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/grpc"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/httpupgrade"
	"google.golang.org/protobuf/proto"
)

// encodeSplitHTTPAsProto manually encodes SplitHTTP config as protobuf wire format.
// This avoids importing the generated splithttp package which has xray-core dependency conflicts.
// Field numbers: 1=host, 2=path, 3=mode, 4=headers
func encodeSplitHTTPAsProto(host, path, mode string, headers map[string]string) []byte {
	var buf []byte

	// Field 1: host (string, wire type 2 = length-delimited)
	if host != "" {
		buf = append(buf, encodeTagWire(1, 2)...)
		buf = append(buf, encodeVarint(uint64(len(host)))...)
		buf = append(buf, host...)
	}

	// Field 2: path (string, wire type 2 = length-delimited)
	if path != "" {
		buf = append(buf, encodeTagWire(2, 2)...)
		buf = append(buf, encodeVarint(uint64(len(path)))...)
		buf = append(buf, path...)
	}

	// Field 3: mode (string, wire type 2 = length-delimited)
	if mode != "" {
		buf = append(buf, encodeTagWire(3, 2)...)
		buf = append(buf, encodeVarint(uint64(len(mode)))...)
		buf = append(buf, mode...)
	}

	// Field 4: headers (map<string,string>, wire type 2 = length-delimited)
	// In protobuf3, maps are encoded as: field 4, length-delimited, containing key-value pairs
	if len(headers) > 0 {
		var headersBuf []byte
		for k, v := range headers {
			// Key-value pair encoded as nested message (field 1 = key, field 2 = value)
			var kvBuf []byte
			kvBuf = append(kvBuf, encodeTagWire(1, 2)...)
			kvBuf = append(kvBuf, encodeVarint(uint64(len(k)))...)
			kvBuf = append(kvBuf, k...)
			kvBuf = append(kvBuf, encodeTagWire(2, 2)...)
			kvBuf = append(kvBuf, encodeVarint(uint64(len(v)))...)
			kvBuf = append(kvBuf, v...)
			headersBuf = append(headersBuf, kvBuf...)
		}
		buf = append(buf, encodeTagWire(4, 2)...)
		buf = append(buf, encodeVarint(uint64(len(headersBuf)))...)
		buf = append(buf, headersBuf...)
	}

	return buf
}

// encodeTagWire encodes a protobuf field tag (field number + wire type)
func encodeTagWire(fieldNum uint64, wireType uint64) []byte {
	tag := (fieldNum << 3) | wireType
	return encodeVarint(tag)
}

// encodeVarint encodes a uint64 as a protobuf varint
func encodeVarint(v uint64) []byte {
	var buf []byte
	for v >= 0x80 {
		buf = append(buf, byte(v|0x80))
		v >>= 7
	}
	buf = append(buf, byte(v))
	return buf
}

// InboundHandlerConfig is an alias to the generated protobuf InboundHandlerConfig.
// This is kept for backward compatibility - new code should use *proxyman.InboundHandlerConfig.
type InboundHandlerConfig = proxyman.InboundHandlerConfig

// TypedMessage is an alias to serial.TypedMessage for backward compatibility.
type TypedMessage = serial.TypedMessage

// NewTypedMessage creates a TypedMessage from a proto.Message.
// This is the primary path - input MUST be proto.Message, serialized via proto.Marshal.
// Returns error if input is not proto.Message (fail fast).
// NewTypedMessage creates a serial.TypedMessage from a proto.Message.
// This is the primary path - input MUST be proto.Message, serialized via proto.Marshal.
// Returns error if input is not proto.Message (fail fast).
func NewTypedMessage(typeName string, msg proto.Message) (*serial.TypedMessage, error) {
	if msg == nil {
		return nil, fmt.Errorf("message cannot be nil")
	}
	// Verify msg is actually a proto.Message by checking if it's a pointer
	// proto.Message is an interface, nil check above handles nil case
	valueBytes, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal proto message: %w", err)
	}
	return &serial.TypedMessage{
		Type:  typeName,
		Value: valueBytes,
	}, nil
}

// NewTypedMessageRaw creates a TypedMessage with raw bytes.
// NewTypedMessageRaw creates a serial.TypedMessage with raw bytes.
func NewTypedMessageRaw(typeName string, value []byte) *serial.TypedMessage {
	return &serial.TypedMessage{
		Type:  typeName,
		Value: value,
	}
}

// NewTypedMessageFromProto creates a TypedMessage from a proto.Message.
// This uses proper protobuf serialization instead of JSON.
// NewTypedMessageFromProto creates a serial.TypedMessage from a proto.Message.
// This uses proper protobuf serialization instead of JSON.
func NewTypedMessageFromProto(typeName string, msg proto.Message) (*serial.TypedMessage, error) {
	valueBytes, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return &serial.TypedMessage{
		Type:  typeName,
		Value: valueBytes,
	}, nil
}

// InboundHandlerConfigToJSON converts InboundHandlerConfig to JSON bytes.
// Uses standard JSON marshal for simplicity and compatibility.
func InboundHandlerConfigToJSON(cfg *InboundHandlerConfig) ([]byte, error) {
	return json.Marshal(cfg)
}

// SerializeToJSON serializes any object to JSON bytes.
func SerializeToJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// DeserializeFromJSON deserializes JSON bytes into the given object.
func DeserializeFromJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// FlexPort is a string type that also accepts JSON numbers (int/float) for the "port" field.
// xray native config uses integer ports; internal config uses string ports.
type FlexPort string

func (p *FlexPort) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*p = FlexPort(s)
		return nil
	}
	// Try number
	var n float64
	if err := json.Unmarshal(data, &n); err == nil {
		*p = FlexPort(fmt.Sprintf("%d", int(n)))
		return nil
	}
	return fmt.Errorf("FlexPort: cannot unmarshal %s", string(data))
}

// InboundConfigJSON represents the JSON structure for inbound config parsing.
type InboundConfigJSON struct {
	Tag            string                   `json:"tag"`
	Protocol       string                   `json:"protocol"`
	Port           FlexPort                 `json:"port"`
	Listen         string                   `json:"listen,omitempty"`
	Settings       map[string]interface{}   `json:"settings,omitempty"`
	StreamSettings map[string]interface{}   `json:"streamSettings,omitempty"`
	Sniffing       map[string]interface{}   `json:"sniffing,omitempty"`
	Fallbacks      []map[string]interface{} `json:"fallbacks,omitempty"`
}

// ParseInboundConfig parses JSON config into the generated protobuf InboundHandlerConfig.
// It uses proper protobuf serialization for TypedMessage values.
// Returns the generated protobuf type for use with gRPC.
func ParseInboundConfig(jsonData []byte) (*proxyman.InboundHandlerConfig, error) {
	var inbound InboundConfigJSON
	if err := json.Unmarshal(jsonData, &inbound); err != nil {
		return nil, err
	}

	// Build receiver settings using proper protobuf
	receiverConfig, err := buildReceiverConfigProto(string(inbound.Port), inbound.Listen, inbound.StreamSettings, inbound.Sniffing)
	if err != nil {
		return nil, fmt.Errorf("failed to build receiver config: %w", err)
	}

	// Build proxy settings using proper protobuf
	var proxySettings *serial.TypedMessage
	if inbound.Settings != nil {
		proxyType := getProtocolSettingsType(inbound.Protocol)
		proxyConfig, err := buildProxySettingsProto(proxyType, inbound.Protocol, inbound.Settings)
		if err != nil {
			return nil, fmt.Errorf("failed to build proxy settings: %w", err)
		}
		proxySettings = proxyConfig
	}

	// SniffingSettings is now handled in buildReceiverConfigProto

	return &proxyman.InboundHandlerConfig{
		Tag:              inbound.Tag,
		ReceiverSettings: receiverConfig,
		ProxySettings:    proxySettings,
	}, nil
}

// buildReceiverConfigProto builds a proper protobuf ReceiverConfig.
func buildReceiverConfigProto(port, listen string, streamSettings map[string]interface{}, sniffing map[string]interface{}) (*serial.TypedMessage, error) {
	// Parse port range
	var portFrom, portTo uint32
	fmt.Sscanf(port, "%d", &portFrom)
	portTo = portFrom

	receiverConfig := &proxyman.ReceiverConfig{
		PortList: &net.PortList{
			Range: []*net.PortRange{
				{
					From: portFrom,
					To:   portTo,
				},
			},
		},
	}

	// Set listen address if specified
	if listen != "" && listen != "0.0.0.0" && listen != "::" {
		ip := stdnet.ParseIP(listen)
		if ip != nil {
			// Valid IP address
			receiverConfig.Listen = &net.IPOrDomain{
				Address: &net.IPOrDomain_Ip{
					Ip: ip,
				},
			}
		} else {
			// Domain name
			receiverConfig.Listen = &net.IPOrDomain{
				Address: &net.IPOrDomain_Domain{
					Domain: listen,
				},
			}
		}
	}

	// Build StreamSettings if provided
	if streamSettings != nil {
		streamConfig, err := buildStreamConfigProto(streamSettings)
		if err != nil {
			return nil, fmt.Errorf("failed to build stream config: %w", err)
		}
		receiverConfig.StreamSettings = streamConfig
	}

	// Build SniffingSettings if provided
	if sniffing != nil {
		sniffingConfig, err := buildSniffingConfigProtoDirect(sniffing)
		if err != nil {
			return nil, fmt.Errorf("failed to build sniffing config: %w", err)
		}
		receiverConfig.SniffingSettings = sniffingConfig
	}

	return NewTypedMessage("xray.app.proxyman.ReceiverConfig", receiverConfig)
}

// buildProxySettingsProto builds the appropriate protobuf config based on protocol.
func buildProxySettingsProto(typeName, protocol string, settings map[string]interface{}) (*serial.TypedMessage, error) {
	switch protocol {
	case "vmess":
		return buildVMessInboundConfig(settings)
	case "vless":
		return buildVLessInboundConfig(settings)
	case "trojan":
		return buildTrojanInboundConfig(settings)
	case "shadowsocks":
		return buildShadowsocksInboundConfig(settings)
	case "socks":
		return buildSocksInboundConfig(settings)
	case "http":
		return buildHTTPInboundConfig(settings)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

// buildVMessInboundConfig builds VMess inbound protobuf config.
func buildVMessInboundConfig(settings map[string]interface{}) (*serial.TypedMessage, error) {
	config := &vmessinbound.Config{}

	// Handle clients if present
	if clients, ok := settings["clients"].([]interface{}); ok {
		for _, c := range clients {
			if clientMap, ok := c.(map[string]interface{}); ok {
				user, err := buildVMessUser(clientMap)
				if err != nil {
					continue
				}
				if user != nil {
					config.User = append(config.User, user)
				}
			}
		}
	}

	// Handle default settings
	if def, ok := settings["default"].(map[string]interface{}); ok {
		if level, ok := def["level"].(float64); ok {
			config.Default = &vmessinbound.DefaultConfig{
				Level: uint32(level),
			}
		}
	}

	return NewTypedMessage("xray.proxy.vmess.inbound.Config", config)
}

// buildVMessUser builds a VMess User from map.
func buildVMessUser(clientMap map[string]interface{}) (*protocol.User, error) {
	user := &protocol.User{}

	// Build VMess account
	var account *vmess.Account
	if id, ok := clientMap["id"].(string); ok {
		account = &vmess.Account{
			Id: id,
		}
	} else {
		account = &vmess.Account{
			Id: "00000000-0000-0000-0000-000000000000",
		}
	}

	// Set user level and email
	if level, ok := clientMap["level"].(float64); ok {
		user.Level = uint32(level)
	}
	if email, ok := clientMap["email"].(string); ok {
		user.Email = email
	}

	// Wrap account in TypedMessage
	accountBytes, err := proto.Marshal(account)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal account: %w", err)
	}
	user.Account = &serial.TypedMessage{
		Type:  "xray.proxy.vmess.Account",
		Value: accountBytes,
	}

	return user, nil
}

// buildVLessInboundConfig builds VLESS inbound protobuf config.
func buildVLessInboundConfig(settings map[string]interface{}) (*serial.TypedMessage, error) {
	config := &inbound.Config{}

	// Handle clients if present
	if clients, ok := settings["clients"].([]interface{}); ok {
		for _, c := range clients {
			if clientMap, ok := c.(map[string]interface{}); ok {
				user := &protocol.User{}

				// Set user level and email
				if level, ok := clientMap["level"].(float64); ok {
					user.Level = uint32(level)
				}
				if email, ok := clientMap["email"].(string); ok {
					user.Email = email
				}

				// Create VLESS account using protobuf serialization
				if id, ok := clientMap["id"].(string); ok {
					account := &vlessaccount.Account{Id: id}
					// Handle flow (e.g., "xtls-rprx-vision")
					if flow, ok := clientMap["flow"].(string); ok && flow != "" {
						account.Flow = flow
					}
					accountBytes, err := proto.Marshal(account)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal VLESS account: %w", err)
					}
					user.Account = &serial.TypedMessage{
						Type:  "xray.proxy.vless.Account",
						Value: accountBytes,
					}
				}

				config.Clients = append(config.Clients, user)
			}
		}
	}

	// Handle fallbacks if present
	if fallbacks, ok := settings["fallbacks"].([]interface{}); ok {
		for _, f := range fallbacks {
			if fbMap, ok := f.(map[string]interface{}); ok {
				fallback := &inbound.Fallback{}

				if name, ok := fbMap["name"].(string); ok {
					fallback.Name = name
				}
				if alpn, ok := fbMap["alpn"].(string); ok {
					fallback.Alpn = alpn
				}
				if path, ok := fbMap["path"].(string); ok {
					fallback.Path = path
				}
				if fbType, ok := fbMap["type"].(string); ok {
					fallback.Type = fbType
				}
				if dest, ok := fbMap["dest"].(string); ok {
					fallback.Dest = dest
				}
				if xver, ok := fbMap["xver"].(float64); ok {
					fallback.Xver = uint64(xver)
				}

				config.Fallbacks = append(config.Fallbacks, fallback)
			}
		}
	}

	// Handle decryption if present
	if decryption, ok := settings["decryption"].(string); ok {
		config.Decryption = decryption
	} else {
		// Default to "none" if not specified
		config.Decryption = "none"
	}

	return NewTypedMessageFromProto("xray.proxy.vless.inbound.Config", config)
}

// buildTrojanInboundConfig builds Trojan inbound protobuf config.
func buildTrojanInboundConfig(settings map[string]interface{}) (*serial.TypedMessage, error) {
	config := &trojan.ServerConfig{}

	// Handle clients if present
	if clients, ok := settings["clients"].([]interface{}); ok {
		for _, c := range clients {
			if clientMap, ok := c.(map[string]interface{}); ok {
				user := &protocol.User{}

				// Set user level and email
				if level, ok := clientMap["level"].(float64); ok {
					user.Level = uint32(level)
				}
				if email, ok := clientMap["email"].(string); ok {
					user.Email = email
				}

				// Create Trojan account using protobuf serialization
				if password, ok := clientMap["password"].(string); ok {
					account := &trojan.Account{Password: password}
					accountBytes, err := proto.Marshal(account)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal Trojan account: %w", err)
					}
					user.Account = &serial.TypedMessage{
						Type:  "xray.proxy.trojan.Account",
						Value: accountBytes,
					}
				}

				config.Users = append(config.Users, user)
			}
		}
	}

	// Handle fallbacks if present
	if fallbacks, ok := settings["fallbacks"].([]interface{}); ok {
		for _, f := range fallbacks {
			if fbMap, ok := f.(map[string]interface{}); ok {
				fallback := &trojan.Fallback{}

				if name, ok := fbMap["name"].(string); ok {
					fallback.Name = name
				}
				if alpn, ok := fbMap["alpn"].(string); ok {
					fallback.Alpn = alpn
				}
				if path, ok := fbMap["path"].(string); ok {
					fallback.Path = path
				}
				if fbType, ok := fbMap["type"].(string); ok {
					fallback.Type = fbType
				}
				if dest, ok := fbMap["dest"].(string); ok {
					fallback.Dest = dest
				}
				if xver, ok := fbMap["xver"].(float64); ok {
					fallback.Xver = uint64(xver)
				}

				config.Fallbacks = append(config.Fallbacks, fallback)
			}
		}
	}

	return NewTypedMessageFromProto("xray.proxy.trojan.ServerConfig", config)
}

// buildShadowsocksInboundConfig builds Shadowsocks 2022 inbound protobuf config.
func buildShadowsocksInboundConfig(settings map[string]interface{}) (*serial.TypedMessage, error) {
	config := &shadowsocks_2022.ServerConfig{}

	// Handle method if present
	if method, ok := settings["method"].(string); ok {
		config.Method = method
	}

	// Handle password if present
	if password, ok := settings["password"].(string); ok {
		config.Key = password
	}

	// Handle network if present
	if network, ok := settings["network"].(string); ok {
		if network == "tcp" {
			config.Network = []net.Network{net.Network_TCP}
		} else if network == "udp" {
			config.Network = []net.Network{net.Network_UDP}
		} else if network == "tcp,udp" {
			config.Network = []net.Network{net.Network_TCP, net.Network_UDP}
		}
	}

	// Handle email if present
	if email, ok := settings["email"].(string); ok {
		config.Email = email
	}

	// Handle level if present
	if level, ok := settings["level"].(float64); ok {
		config.Level = int32(level)
	}

	return NewTypedMessage("xray.proxy.shadowsocks_2022.ServerConfig", config)
}

// normalizeNetworkName maps user-friendly network names to xray internal names.
func normalizeNetworkName(network string) string {
	switch network {
	case "ws":
		return "websocket"
	case "h2", "http":
		// Keep "h2" as-is for Xray inbound acceptance
		// Note: Xray's inbound config accepts "h2" but not "http" as transport protocol
		return "h2"
	case "xhttp":
		// xhttp is an alias for splithttp in xray
		return "splithttp"
	case "splithttp", "h3":
		// Keep splithttp as-is (h3 is HTTP,/3 but in xray context it's used for splithttp)
		return "splithttp"
	default:
		return network
	}
}

// buildStreamConfigProto builds StreamConfig protobuf from streamSettings map.
func buildStreamConfigProto(streamSettings map[string]interface{}) (*internet.StreamConfig, error) {
	streamConfig := &internet.StreamConfig{}

	// Set network protocol (tcp, ws, grpc, h2, etc.) - normalize to xray internal names
	if network, ok := streamSettings["network"].(string); ok {
		streamConfig.ProtocolName = normalizeNetworkName(network)
	}

	// Build TLS settings for both "tls" and "xtls"
	// Note: XTLS in xray-core uses TLS config internally. The flow (e.g., xtls-rprx-vision)
	// is handled via the client flow field, not the security type.
	if security, ok := streamSettings["security"].(string); ok && (security == "tls" || security == "xtls") {
		// Use TLS config for both tls and xtls security types
		// XTLS uses the same config structure as TLS
		streamConfig.SecurityType = "xray.transport.internet.tls.Config"

		if tlsSettings, ok := streamSettings["tlsSettings"].(map[string]interface{}); ok {
			tlsConfig := &tls.Config{}

			if serverName, ok := tlsSettings["serverName"].(string); ok {
				tlsConfig.ServerName = serverName
			}
			if allowInsecure, ok := tlsSettings["allowInsecure"].(bool); ok {
				tlsConfig.AllowInsecure = allowInsecure
			}
			if alpn, ok := tlsSettings["alpn"].([]interface{}); ok {
				for _, a := range alpn {
					if s, ok := a.(string); ok {
						tlsConfig.NextProtocol = append(tlsConfig.NextProtocol, s)
					}
				}
			}
			if fingerprint, ok := tlsSettings["fingerprint"].(string); ok {
				tlsConfig.Fingerprint = fingerprint
			}
			if rejectUnknownSni, ok := tlsSettings["rejectUnknownSni"].(bool); ok {
				tlsConfig.RejectUnknownSni = rejectUnknownSni
			}
			if minVersion, ok := tlsSettings["minVersion"].(string); ok {
				tlsConfig.MinVersion = minVersion
			}

			// Handle certificates - support both path (certificateFile/keyFile) and content (certificate/key)
			if certificates, ok := tlsSettings["certificates"].([]interface{}); ok {
				for _, cert := range certificates {
					if certMap, ok := cert.(map[string]interface{}); ok {
						cert := &tls.Certificate{}

						// Handle certificate content (string) -> Certificate bytes
						if certContent, ok := certMap["certificate"].(string); ok {
							cert.Certificate = []byte(certContent)
						}

						// Handle key content (string) -> Key bytes
						if keyContent, ok := certMap["key"].(string); ok {
							cert.Key = []byte(keyContent)
						}

						// Handle certificateFile path - read content AND set path
						if certFile, ok := certMap["certificateFile"].(string); ok {
							cert.CertificatePath = certFile
							// If certificate content not provided, read from file
							if cert.Certificate == nil {
								if data, err := os.ReadFile(certFile); err == nil {
									cert.Certificate = data
								}
							}
						}

						// Handle keyFile path - read content AND set path
						if keyFile, ok := certMap["keyFile"].(string); ok {
							cert.KeyPath = keyFile
							// If key content not provided, read from file
							if cert.Key == nil {
								if data, err := os.ReadFile(keyFile); err == nil {
									cert.Key = data
								}
							}
						}

						// Handle ocspStapling (uint64 in protobuf)
						if ocsp, ok := certMap["ocspStapling"].(float64); ok && ocsp > 0 {
							cert.OcspStapling = uint64(ocsp)
						} else if ocsp, ok := certMap["ocspStapling"].(int); ok && ocsp > 0 {
							cert.OcspStapling = uint64(ocsp)
						}

						tlsConfig.Certificate = append(tlsConfig.Certificate, cert)
					}
				}
			}

			// Wrap in TypedMessage
			tlsMsg, err := NewTypedMessage("xray.transport.internet.tls.Config", tlsConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to create TLS TypedMessage: %w", err)
			}
			streamConfig.SecuritySettings = append(streamConfig.SecuritySettings, tlsMsg)
		}
	}

	// Build Reality settings if security is "reality"
	if security, ok := streamSettings["security"].(string); ok && security == "reality" {
		streamConfig.SecurityType = "xray.transport.internet.reality.Config"

		if realitySettings, ok := streamSettings["realitySettings"].(map[string]interface{}); ok {
			realityConfig := &reality.Config{}

			if serverNames, ok := realitySettings["serverNames"].([]interface{}); ok {
				for _, s := range serverNames {
					if sn, ok := s.(string); ok {
						realityConfig.ServerNames = append(realityConfig.ServerNames, sn)
					}
				}
			}

			// PrivateKey: decode from base64 (supports both RawURLEncoding and StdEncoding)
			if privateKey, ok := realitySettings["privateKey"].(string); ok && privateKey != "" {
				// Try RawURLEncoding first (URL-safe, no padding) - matches adapter output
				decoded, err := base64.RawURLEncoding.DecodeString(privateKey)
				if err != nil {
					// Fall back to StdEncoding (standard base64 with padding)
					decoded, err = base64.StdEncoding.DecodeString(privateKey)
					if err != nil {
						return nil, fmt.Errorf("failed to decode PrivateKey: %w", err)
					}
				}
				if len(decoded) != 32 {
					return nil, fmt.Errorf("invalid PrivateKey length: %d (expected 32)", len(decoded))
				}
				realityConfig.PrivateKey = decoded
			}

			// PublicKey: decode from base64
			if publicKey, ok := realitySettings["publicKey"].(string); ok && publicKey != "" {
				decoded, err := base64.RawURLEncoding.DecodeString(publicKey)
				if err != nil {
					decoded, err = base64.StdEncoding.DecodeString(publicKey)
					if err != nil {
						return nil, fmt.Errorf("failed to decode PublicKey: %w", err)
					}
				}
				if len(decoded) != 32 {
					return nil, fmt.Errorf("invalid PublicKey length: %d (expected 32)", len(decoded))
				}
				realityConfig.PublicKey = decoded
			}

			// ShortId (singular): must be valid hex, max 8 bytes when decoded
			if shortId, ok := realitySettings["shortId"].(string); ok && shortId != "" {
				decoded, err := hex.DecodeString(shortId)
				if err != nil {
					return nil, fmt.Errorf("invalid ShortId: must be valid hex string: %w", err)
				}
				if len(decoded) > 8 {
					return nil, fmt.Errorf("invalid ShortId length: %d (max 8)", len(decoded))
				}
				realityConfig.ShortId = decoded
			}

			if fingerprint, ok := realitySettings["fingerprint"].(string); ok {
				realityConfig.Fingerprint = fingerprint
			}
			if serverName, ok := realitySettings["serverName"].(string); ok {
				realityConfig.ServerName = serverName
			}

			// Handle dest/target (required for reality)
			if dest, ok := realitySettings["dest"].(string); ok {
				realityConfig.Dest = dest
			} else if target, ok := realitySettings["target"].(string); ok {
				realityConfig.Dest = target
			}

			// Handle shortIds array ([]interface{} -> [][]byte)
			// Each element must be valid hex, max 8 bytes when decoded
			if shortIds, ok := realitySettings["shortIds"].([]interface{}); ok {
				for _, sid := range shortIds {
					if sidStr, ok := sid.(string); ok {
						// Must be valid hex - return error instead of silent fallback
						decoded, err := hex.DecodeString(sidStr)
						if err != nil {
							return nil, fmt.Errorf("invalid shortId %q: must be valid hex string: %w", sidStr, err)
						}
						if len(decoded) > 8 {
							return nil, fmt.Errorf("invalid shortId %q length: %d (max 8)", sidStr, len(decoded))
						}
						realityConfig.ShortIds = append(realityConfig.ShortIds, decoded)
					}
				}
			}

			// Type: set network type for reality (required by xray for dialing)
			// If not provided, default to "tcp"
			if realityType, ok := realitySettings["type"].(string); ok && realityType != "" {
				realityConfig.Type = realityType
			} else {
				realityConfig.Type = "tcp"
			}

			// Wrap in TypedMessage
			realityMsg, err := NewTypedMessage("xray.transport.internet.reality.Config", realityConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to create Reality TypedMessage: %w", err)
			}
			streamConfig.SecuritySettings = append(streamConfig.SecuritySettings, realityMsg)
		}
	}

	// Note: XTLS handling is now integrated into the TLS block above
	// When security=="xtls", we use tls.Config with SecurityType="xray.transport.internet.tls.Config"
	// The flow (e.g., xtls-rprx-vision) is handled via client flow field, not here

	// Build TransportSettings based on network protocol
	network := streamConfig.ProtocolName
	if network == "" {
		network = "tcp"
	}

	switch network {
	case "ws", "websocket":
		if wsSettings, ok := streamSettings["wsSettings"].(map[string]interface{}); ok {
			wsConfig := &websocket.Config{}
			if path, ok := wsSettings["path"].(string); ok {
				wsConfig.Path = path
			}
			if host, ok := wsSettings["host"].(string); ok {
				wsConfig.Host = host
			}
			if headers, ok := wsSettings["headers"].(map[string]interface{}); ok {
				wsConfig.Header = make(map[string]string)
				for k, v := range headers {
					if s, ok := v.(string); ok {
						wsConfig.Header[k] = s
					}
				}
			}

			wsMsg, err := NewTypedMessage("xray.transport.internet.websocket.Config", wsConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to create WebSocket TypedMessage: %w", err)
			}
			streamConfig.TransportSettings = append(streamConfig.TransportSettings, &internet.TransportConfig{
				ProtocolName: "websocket",
				Settings:     wsMsg,
			})
		}
	case "grpc":
		if grpcSettings, ok := streamSettings["grpcSettings"].(map[string]interface{}); ok {
			grpcConfig := &grpc.Config{}
			if serviceName, ok := grpcSettings["serviceName"].(string); ok {
				grpcConfig.ServiceName = serviceName
			}
			if multiMode, ok := grpcSettings["multiMode"].(bool); ok {
				grpcConfig.MultiMode = multiMode
			}

			grpcMsg, err := NewTypedMessage("xray.transport.internet.grpc.encoding.Config", grpcConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to create gRPC TypedMessage: %w", err)
			}
			streamConfig.TransportSettings = append(streamConfig.TransportSettings, &internet.TransportConfig{
				ProtocolName: "grpc",
				Settings:     grpcMsg,
			})
		}
	case "h2", "http":
		// HTTP/2 uses TCP transport with HTTP settings
		if tcpSettings, ok := streamSettings["httpSettings"].(map[string]interface{}); ok {
			tcpConfig := &tcp.Config{}

			// HTTP settings are typically in the tcpConfig's header settings
			if host, ok := tcpSettings["host"].(string); ok {
				// Store in a custom field or as headers
				tcpConfig.HeaderSettings = &serial.TypedMessage{
					Type: "xray.transport.internet.http.Config",
					Value: []byte(fmt.Sprintf(`{"host":["%s"]}`, host)),
				}
			}

			tcpMsg, err := NewTypedMessage("xray.transport.internet.tcp.Config", tcpConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to create TCP TypedMessage: %w", err)
			}
			streamConfig.TransportSettings = append(streamConfig.TransportSettings, &internet.TransportConfig{
				ProtocolName: "tcp",
				Settings:     tcpMsg,
			})
		}
	case "tcp":
		// Basic TCP - can have header settings for obfuscation
		if tcpSettings, ok := streamSettings["tcpSettings"].(map[string]interface{}); ok {
			tcpConfig := &tcp.Config{}
			if acceptProxyProtocol, ok := tcpSettings["acceptProxyProtocol"].(bool); ok {
				tcpConfig.AcceptProxyProtocol = acceptProxyProtocol
			}

			tcpMsg, err := NewTypedMessage("xray.transport.internet.tcp.Config", tcpConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to create TCP TypedMessage: %w", err)
			}
			streamConfig.TransportSettings = append(streamConfig.TransportSettings, &internet.TransportConfig{
				ProtocolName: "tcp",
				Settings:     tcpMsg,
			})
		}
	case "httpupgrade":
		if huSettings, ok := streamSettings["httpupgradeSettings"].(map[string]interface{}); ok {
			huConfig := &httpupgrade.Config{}
			if path, ok := huSettings["path"].(string); ok {
				huConfig.Path = path
			}
			if host, ok := huSettings["host"].(string); ok {
				huConfig.Host = host
			}
			if headers, ok := huSettings["headers"].(map[string]interface{}); ok {
				huConfig.Header = make(map[string]string)
				for k, v := range headers {
					if s, ok := v.(string); ok {
						huConfig.Header[k] = s
					}
				}
			}
			if acceptPP, ok := huSettings["acceptProxyProtocol"].(bool); ok {
				huConfig.AcceptProxyProtocol = acceptPP
			}

			huMsg, err := NewTypedMessage("xray.transport.internet.httpupgrade.Config", huConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to create HTTPUpgrade TypedMessage: %w", err)
			}
			streamConfig.TransportSettings = append(streamConfig.TransportSettings, &internet.TransportConfig{
				ProtocolName: "httpupgrade",
				Settings:     huMsg,
			})
		}
	case "splithttp":
		// SplitHTTP transport - manually encoded as protobuf wire format (splithttp package not generated)
		if splithttpSettings, ok := streamSettings["splithttpSettings"].(map[string]interface{}); ok {
			// Extract fields
			var host, path, mode string
			var headers map[string]string

			if h, ok := splithttpSettings["host"].(string); ok {
				host = h
			}
			if p, ok := splithttpSettings["path"].(string); ok {
				path = p
			}
			if m, ok := splithttpSettings["mode"].(string); ok {
				mode = m
			}
			if hdrs, ok := splithttpSettings["headers"].(map[string]interface{}); ok {
				headers = make(map[string]string)
				for k, v := range hdrs {
					if s, ok := v.(string); ok {
						headers[k] = s
					}
				}
			}

			// Manually encode as protobuf wire format (field numbers: 1=host, 2=path, 3=mode, 4=headers)
			splithttpBytes := encodeSplitHTTPAsProto(host, path, mode, headers)

			// Use the proper xray type name with proto bytes
			splithttpMsg := &serial.TypedMessage{
				Type:  "xray.transport.internet.splithttp.Config",
				Value: splithttpBytes,
			}
			streamConfig.TransportSettings = append(streamConfig.TransportSettings, &internet.TransportConfig{
				ProtocolName: "splithttp",
				Settings:     splithttpMsg,
			})
		}
	}

	return streamConfig, nil
}

// buildSniffingConfigProtoDirect builds SniffingConfig protobuf (direct assignment, not TypedMessage).
func buildSniffingConfigProtoDirect(sniffing map[string]interface{}) (*proxyman.SniffingConfig, error) {
	config := &proxyman.SniffingConfig{}

	if enabled, ok := sniffing["enabled"].(bool); ok {
		config.Enabled = enabled
	}

	if destOverride, ok := sniffing["destOverride"].([]interface{}); ok {
		for _, d := range destOverride {
			if s, ok := d.(string); ok {
				config.DestinationOverride = append(config.DestinationOverride, s)
			}
		}
	}

	if metadataOnly, ok := sniffing["metadataOnly"].(bool); ok {
		config.MetadataOnly = metadataOnly
	}

	if domainsExcluded, ok := sniffing["domainsExcluded"].([]interface{}); ok {
		for _, d := range domainsExcluded {
			if s, ok := d.(string); ok {
				config.DomainsExcluded = append(config.DomainsExcluded, s)
			}
		}
	}

	if routeOnly, ok := sniffing["routeOnly"].(bool); ok {
		config.RouteOnly = routeOnly
	}

	return config, nil
}

// buildSniffingConfigProto builds SniffingConfig protobuf.
func buildSniffingConfigProto(sniffing map[string]interface{}) (*serial.TypedMessage, error) {
	config := &proxyman.SniffingConfig{}

	if enabled, ok := sniffing["enabled"].(bool); ok {
		config.Enabled = enabled
	}

	if destOverride, ok := sniffing["destOverride"].([]interface{}); ok {
		for _, d := range destOverride {
			if s, ok := d.(string); ok {
				config.DestinationOverride = append(config.DestinationOverride, s)
			}
		}
	}

	if metadataOnly, ok := sniffing["metadataOnly"].(bool); ok {
		config.MetadataOnly = metadataOnly
	}

	return NewTypedMessage("xray.app.proxyman.SniffingConfig", config)
}

// buildSocksInboundConfig builds SOCKS5 inbound protobuf config.
// Supported settings fields: auth (none/password), accounts ([]{ user, pass }), udp, userLevel
func buildSocksInboundConfig(settings map[string]interface{}) (*serial.TypedMessage, error) {
	config := &sockscfg.ServerConfig{}

	if auth, ok := settings["auth"].(string); ok && auth == "password" {
		config.AuthType = sockscfg.AuthType_PASSWORD
	} else {
		config.AuthType = sockscfg.AuthType_NO_AUTH
	}

	if accounts, ok := settings["accounts"].([]interface{}); ok {
		config.Accounts = make(map[string]string)
		for _, a := range accounts {
			if m, ok := a.(map[string]interface{}); ok {
				user, _ := m["user"].(string)
				pass, _ := m["pass"].(string)
				if user != "" {
					config.Accounts[user] = pass
				}
			}
		}
	}

	if udp, ok := settings["udp"].(bool); ok {
		config.UdpEnabled = udp
	}

	if level, ok := settings["userLevel"].(float64); ok {
		config.UserLevel = uint32(level)
	}

	return NewTypedMessage("xray.proxy.socks.ServerConfig", config)
}

// buildHTTPInboundConfig builds HTTP inbound protobuf config.
// Supported settings fields: accounts ([]{ user, pass }), allowTransparent, userLevel
func buildHTTPInboundConfig(settings map[string]interface{}) (*serial.TypedMessage, error) {
	config := &httpcfg.ServerConfig{}

	if accounts, ok := settings["accounts"].([]interface{}); ok {
		config.Accounts = make(map[string]string)
		for _, a := range accounts {
			if m, ok := a.(map[string]interface{}); ok {
				user, _ := m["user"].(string)
				pass, _ := m["pass"].(string)
				if user != "" {
					config.Accounts[user] = pass
				}
			}
		}
	}

	if allow, ok := settings["allowTransparent"].(bool); ok {
		config.AllowTransparent = allow
	}

	if level, ok := settings["userLevel"].(float64); ok {
		config.UserLevel = uint32(level)
	}

	return NewTypedMessage("xray.proxy.http.ServerConfig", config)
}

func getProtocolSettingsType(protocol string) string {
	switch protocol {
	case "vmess":
		return "xray.proxy.vmess.inbound.Config"
	case "vless":
		return "xray.proxy.vless.inbound.Config"
	case "trojan":
		return "xray.proxy.trojan.ServerConfig"
	case "shadowsocks":
		return "xray.proxy.shadowsocks_2022.ServerConfig"
	case "socks":
		return "xray.proxy.socks.ServerConfig"
	case "http":
		return "xray.proxy.http.ServerConfig"
	default:
		return "xray.proxy.vmess.inbound.Config"
	}
}

// AlterInboundRequest represents the request for AlterInbound gRPC call.
type AlterInboundRequest struct {
	Tag       string               `json:"tag,omitempty"`
	Operation *serial.TypedMessage `json:"operation,omitempty"`
}

// AlterInboundRequestToJSON serializes the request to JSON.
func AlterInboundRequestToJSON(req *AlterInboundRequest) ([]byte, error) {
	return json.Marshal(req)
}

// NewAddUserOperation creates an AddUserOperation TypedMessage from protobuf messages.
// This is the primary path - all inputs must be proto.Message.
func NewAddUserOperation(email string, level uint32, accountType string, accountMsg proto.Message) (*serial.TypedMessage, error) {
	if accountMsg == nil {
		return nil, fmt.Errorf("account message cannot be nil")
	}

	// Marshal the account to bytes
	accountBytes, err := proto.Marshal(accountMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal account: %w", err)
	}

	// Build the user account with TypedMessage for account
	user := &protocol.User{
		Email: email,
		Level: level,
		Account: &serial.TypedMessage{
			Type:  accountType,
			Value: accountBytes,
		},
	}

	// Build AddUserOperation proto
	// For simplicity, we'll use a map approach that gets serialized
	// Note: This is a simplified implementation
	type AddUserOp struct {
		User *protocol.User `json:"user"`
	}
	op := &AddUserOp{User: user}

	// Marshal to JSON for now (this is for AlterInbound which may have different requirements)
	opJSON, _ := json.Marshal(op)
	return &serial.TypedMessage{
		Type:  "xray.app.proxyman.command.AddUserOperation",
		Value: opJSON,
	}, nil
}

// NewRemoveUserOperation creates a RemoveUserOperation TypedMessage.
func NewRemoveUserOperation(email string) (*serial.TypedMessage, error) {
	type RemoveUserOp struct {
		Email string `json:"email"`
	}
	op := &RemoveUserOp{Email: email}

	opJSON, _ := json.Marshal(op)
	return &serial.TypedMessage{
		Type:  "xray.app.proxyman.command.RemoveUserOperation",
		Value: opJSON,
	}, nil
}
