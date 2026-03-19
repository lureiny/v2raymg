// Package xray provides Xray adapter implementation.
package xray

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/curve25519"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// FallbackSpec describes a fallback destination.
type FallbackSpec struct {
	Name string // fallback name (optional)
	Type string // fallback type: "tcp", "unix", "serve"
	Alpn string
	Path string
	Dest string
	Xver int
}

// Adapter maps domain models to Xray native config.
type Adapter struct{}

// NewAdapter creates a new Xray adapter.
func NewAdapter() *Adapter {
	return &Adapter{}
}

// ValidateInbound validates an inbound spec for xray.
func (a *Adapter) ValidateInbound(spec contracts.InboundSpec) error {
	return spec.Validate()
}

// MapUsers maps user specs to Xray native user format.
func (a *Adapter) MapUsers(users []contracts.UserSpec, protocol contracts.Protocol) ([]map[string]any, error) {
	if len(users) == 0 {
		return nil, nil
	}

	result := make([]map[string]any, 0, len(users))
	for _, user := range users {
		userMap := map[string]any{
			"email": user.Username,
			"level": user.Level,
		}

		switch protocol {
		case contracts.ProtocolVMess:
			if uuid, ok := user.GetExtension("uuid"); ok {
				if uuidStr, ok := uuid.(string); ok {
					userMap["id"] = uuidStr
				}
			}
			if alterID, ok := user.GetExtension("alter_id"); ok {
				if alterIDUint, ok := alterID.(uint32); ok {
					userMap["alterId"] = alterIDUint
				}
			}
		case contracts.ProtocolVLess:
			if uuid, ok := user.GetExtension("uuid"); ok {
				if uuidStr, ok := uuid.(string); ok {
					userMap["id"] = uuidStr
				}
			}
			if flow, ok := user.GetExtension("flow"); ok {
				if flowStr, ok := flow.(string); ok {
					userMap["flow"] = flowStr
				}
			}
		case contracts.ProtocolTrojan:
			if user.Password != "" {
				userMap["password"] = user.Password
			}
		case contracts.ProtocolShadowsocks:
			if user.Password != "" {
				userMap["password"] = user.Password
			}
			if method, ok := user.GetExtension("method"); ok {
				if methodStr, ok := method.(string); ok {
					userMap["method"] = methodStr
				}
			}
		}

		result = append(result, userMap)
	}

	return result, nil
}

// ToProvider converts contracts.InboundSpec to native Xray config.
func (a *Adapter) ToProvider(spec contracts.InboundSpec) (NativeInbound, error) {
	if err := spec.Validate(); err != nil {
		return NativeInbound{}, err
	}

	// Validate Reality security: must be used with VLESS protocol only
	security := ""
	if v, ok := spec.Extensions["security"].(string); ok {
		security = v
	}
	if security == "reality" && spec.Protocol != contracts.ProtocolVLess {
		return NativeInbound{}, fmt.Errorf("reality is only supported with VLESS protocol, got %s", spec.Protocol)
	}

	// Get xray-specific fields from Extensions
	extensions := spec.Extensions
	getString := func(key string) string {
		if v, ok := extensions[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	getBool := func(key string) bool {
		if v, ok := extensions[key]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
		return false
	}

	listenAddr := getString("listen_addr")
	sniffingEnabled := getBool("sniffing_enabled")
	// Transport and Security are passed to buildStreamSettings via extensions

	// Build base inbound config
	// Note: xray expects port as string for gRPC API
	native := map[string]interface{}{
		"protocol": spec.Protocol.String(),
		"port":     fmt.Sprintf("%d", spec.Port),
		"tag":      spec.Tag,
	}

	// Only add listen if it's not empty
	if listenAddr != "" {
		native["listen"] = listenAddr
	}

	// Build protocol-specific settings
	settings := a.buildSettings(spec)
	if settings != nil {
		// Check if users are provided in extensions
		if usersIface, ok := spec.Extensions["users"]; ok {
			if users, ok := usersIface.([]contracts.UserSpec); ok && len(users) > 0 {
				// Map users to native format and add to settings
				nativeUsers, err := a.MapUsers(users, spec.Protocol)
				if err != nil {
					return NativeInbound{}, fmt.Errorf("failed to map users: %w", err)
				}
				if len(nativeUsers) > 0 {
					// Add clients to settings based on protocol
					switch spec.Protocol {
					case contracts.ProtocolVMess, contracts.ProtocolVLess, contracts.ProtocolTrojan:
						settings["clients"] = nativeUsers
					case contracts.ProtocolShadowsocks:
						// Shadowsocks single-user mode: use method/password at settings level
						// Get method from spec.Extensions first, then fall back to user extension
						method := getString("method")
						password := ""
						if method == "" && len(users) > 0 {
							// Fall back to first user's method
							if m, ok := users[0].GetExtension("method"); ok {
								if methodStr, ok := m.(string); ok {
									method = methodStr
								}
							}
						}
						// Get password from first user
						if len(users) > 0 {
							password = users[0].Password
						}
						// Set method and password at settings level (single-user mode)
						if method != "" {
							settings["method"] = method
						}
						if password != "" {
							settings["password"] = password
						}
						// Do NOT add clients for Shadowsocks single-user mode
					}
				}
			}
		}
		native["settings"] = settings
	}

	// Validate Reality shortId parameters before building stream settings
	if security == "reality" {
		if err := validateRealityShortIds(extensions); err != nil {
			return NativeInbound{}, err
		}
	}

	// Build stream settings for non-tcp transports or when TLS is enabled
	streamSettings, err := a.buildStreamSettings(extensions)
	if err != nil {
		return NativeInbound{}, err
	}
	// Only add streamSettings if it's not empty
	if streamSettings != nil && len(streamSettings) > 0 {
		native["streamSettings"] = streamSettings
	}

	// Add sniffing
	if sniffingEnabled {
		sniffing := map[string]interface{}{
			"enabled":      true,
			"destOverride": []string{"http", "tls"}, // default value
		}
		// Allow custom destOverride from extensions
		if destOverride, ok := extensions["sniffing_dest_override"].([]string); ok {
			sniffing["destOverride"] = destOverride
		}
		// Support metadataOnly
		if metadataOnly, ok := extensions["sniffing_metadata_only"].(bool); ok {
			sniffing["metadataOnly"] = metadataOnly
		}
		// Support domainsExcluded
		if domainsExcluded, ok := extensions["sniffing_domains_excluded"].([]string); ok {
			sniffing["domainsExcluded"] = domainsExcluded
		}
		native["sniffing"] = sniffing
	}

	// Add fallbacks from extensions
	if fallbacks, ok := extensions["fallbacks"]; ok {
		if fbList, ok := fallbacks.([]FallbackSpec); ok {
			var fallbackMaps []map[string]interface{}
			for _, fb := range fbList {
				f := map[string]interface{}{
					"dest": fb.Dest,
				}
				if fb.Type != "" {
					f["type"] = fb.Type
				}
				if fb.Alpn != "" {
					f["alpn"] = fb.Alpn
				}
				if fb.Path != "" {
					f["path"] = fb.Path
				}
				if fb.Xver > 0 {
					f["xver"] = fb.Xver
				}
				fallbackMaps = append(fallbackMaps, f)
			}
			native["fallbacks"] = fallbackMaps
		}
	}

	// Convert to JSON
	nativeJSON, err := json.Marshal(native)
	if err != nil {
		return NativeInbound{}, fmt.Errorf("failed to marshal native inbound: %w", err)
	}

	return NativeInbound{JSON: nativeJSON}, nil
}

// FromProvider parses Xray config to domain model.
func (a *Adapter) FromProvider(native NativeInbound) (contracts.InboundSpec, UnmappedWarnings, error) {
	// Unmarshal the native config
	var inMap map[string]any
	if err := json.Unmarshal(native.JSON, &inMap); err != nil {
		return contracts.InboundSpec{}, nil, fmt.Errorf("failed to unmarshal native inbound: %w", err)
	}

	warnings := UnmappedWarnings{}

	// Extract basic fields
	tag, _ := inMap["tag"].(string)
	portStr, _ := inMap["port"].(string)
	var port uint32
	if portStr != "" {
		fmt.Sscanf(portStr, "%d", &port)
	}

	protocolStr, _ := inMap["protocol"].(string)
	protocol := contracts.Protocol(protocolStr)

	// Create spec with Extensions for xray-specific fields
	extensions := make(map[string]any)

	// Extract listen address
	if listen, ok := inMap["listen"].(string); ok {
		extensions["listen_addr"] = listen
	}

	// Extract sniffing
	if sniffing, ok := inMap["sniffing"].(map[string]any); ok {
		if enabled, ok := sniffing["enabled"].(bool); ok {
			extensions["sniffing_enabled"] = enabled
		}
	}

	// Extract stream settings
	if streamSettings, ok := inMap["streamSettings"].(map[string]any); ok {
		if network, ok := streamSettings["network"].(string); ok {
			extensions["transport"] = network
		}
		if security, ok := streamSettings["security"].(string); ok {
			extensions["security"] = security
		}
	}

	spec := contracts.InboundSpec{
		Tag:        tag,
		Port:       port,
		Protocol:   protocol,
		Extensions: extensions,
	}

	return spec, warnings, nil
}

func (a *Adapter) buildSettings(spec contracts.InboundSpec) map[string]interface{} {
	switch spec.Protocol {
	case contracts.ProtocolVMess:
		return map[string]interface{}{
			"clients": []map[string]interface{}{},
		}
	case contracts.ProtocolVLess:
		return map[string]interface{}{
			"clients":    []map[string]interface{}{},
			"decryption": "none",
		}
	case contracts.ProtocolTrojan:
		return map[string]interface{}{
			"clients": []map[string]interface{}{},
		}
	case contracts.ProtocolShadowsocks:
		// Default empty settings - method/password will be set from user/spec
		return map[string]interface{}{}
	case contracts.ProtocolSOCKS5:
		// SOCKS5 - basic settings with UDP disabled
		// Users should be added via AddUser gRPC API after inbound is created
		return map[string]interface{}{
			"udp": false,
		}
	case contracts.ProtocolHTTP:
		// HTTP proxy - basic settings
		// Users should be added via AddUser gRPC API after inbound is created
		return map[string]interface{}{}
	}
	return nil
}

func (a *Adapter) buildStreamSettings(extensions map[string]any) (map[string]interface{}, error) {
	// Get values from extensions
	getString := func(key string) string {
		if v, ok := extensions[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	getStringSliceFromExt := func(key string) []string {
		if v, ok := extensions[key]; ok {
			if s, ok := v.([]string); ok {
				return s
			}
		}
		return nil
	}
	getBool := func(key string) bool {
		if v, ok := extensions[key]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
		return false
	}

	transport := getString("transport")
	security := getString("security")

	// Normalize http transport naming for Xray:
	// - input "http" (friendly) maps to Xray network "h2"
	// - input "h2" stays "h2"
	network := transport
	if transport == "http" {
		network = "h2"
	}

	// If no transport and no security, no stream settings needed
	if network == "" && security == "" {
		return nil, nil
	}

	// If transport is tcp and no security, no stream settings needed
	if (network == "tcp" || network == "") && security == "" {
		return nil, nil
	}

	stream := map[string]interface{}{}

	// Only add network if it's not empty
	if network != "" {
		stream["network"] = network
	}

	// Add security field
	if security != "" {
		stream["security"] = security
	}

	// Add transport settings
	if transport == "ws" {
		wsPath := getString("ws_path")
		streamSettings := map[string]interface{}{}
		if wsPath != "" {
			streamSettings["path"] = wsPath
		}
		stream["wsSettings"] = streamSettings
	}

	// Add gRPC settings
	if transport == "grpc" {
		grpcServiceName := getString("grpc_service_name")
		if grpcServiceName != "" {
			stream["grpcSettings"] = map[string]interface{}{
				"serviceName": grpcServiceName,
			}
		}
	}

	// Add HTTP/2 settings (Xray network "h2")
	if network == "h2" || transport == "http" {
		httpSettings := map[string]interface{}{}
		if p := getString("http_path"); p != "" {
			httpSettings["path"] = p
		}
		if h := getStringSliceFromExt("http_host"); len(h) > 0 {
			httpSettings["host"] = h
		}
		if len(httpSettings) > 0 {
			stream["httpSettings"] = httpSettings
		}
	}

	// Add SplitHTTP settings
	// Note: xhttp is an alias for splithttp in xray, use splithttpSettings key
	if transport == "xhttp" || transport == "splithttp" {
		splithttpSettings := map[string]interface{}{}
		// Mode: auto, zero, one, two
		if mode := getString("xhttp_mode"); mode != "" {
			splithttpSettings["mode"] = mode
		}
		if path := getString("xhttp_path"); path != "" {
			splithttpSettings["path"] = path
		}
		if hosts := getStringSliceFromExt("xhttp_host"); len(hosts) > 0 {
			splithttpSettings["host"] = hosts[0] // Take first host
		}
		if len(splithttpSettings) > 0 {
			stream["splithttpSettings"] = splithttpSettings
		}
	}

	// Add TLS settings
	if security == "tls" || security == "xtls" {
		tlsSettings := map[string]interface{}{}
		serverName := getString("server_name")
		if serverName != "" {
			tlsSettings["serverName"] = serverName
		}
		alpn := getStringSliceFromExt("alpn")
		if len(alpn) > 0 {
			tlsSettings["alpn"] = alpn
		}

		// Add uTLS fingerprint support
		// Common fingerprints: "chrome", "firefox", "safari", "ios", "android", "edge", "randomized"
		fingerprint := getString("utls_fingerprint")
		if fingerprint != "" {
			tlsSettings["fingerprint"] = fingerprint
		}

		// Read certificate from extensions (cert_file/key_file paths or cert_pem/key_pEM content)
		certFile := getString("cert_file")
		keyFile := getString("key_file")
		certPEM := getString("cert_pem")
		keyPEM := getString("key_pem")

		if certFile != "" && keyFile != "" {
			// Use certificate file paths
			tlsSettings["certificates"] = []map[string]interface{}{
				{
					"certificateFile": certFile,
					"keyFile":         keyFile,
				},
			}
		} else if certPEM != "" && keyPEM != "" {
			// Use certificate content directly (inline)
			tlsSettings["certificates"] = []map[string]interface{}{
				{
					"certificate": certPEM,
					"key":         keyPEM,
				},
			}
		}

		// Allow insecure for self-signed certs (usually needed for demo/testing)
		allowInsecure := getBool("tls_allow_insecure")
		if allowInsecure {
			tlsSettings["allowInsecure"] = true
		}

		if len(tlsSettings) > 0 {
			stream["tlsSettings"] = tlsSettings
		}
	}

	// Add Reality settings
	if security == "reality" {
		realitySettings := map[string]interface{}{}
		realityTarget := getString("reality_target")
		if realityTarget != "" {
			realitySettings["target"] = realityTarget
		} else {
			// Backward compat: use dest if target not set
			realityDest := getString("reality_dest")
			if realityDest != "" {
				realitySettings["dest"] = realityDest
			}
		}
		realityServerNames := getStringSliceFromExt("reality_server_names")
		if len(realityServerNames) > 0 {
			realitySettings["serverNames"] = realityServerNames
		}
		realityShortIDs := getStringSliceFromExt("reality_short_ids")
		if len(realityShortIDs) > 0 {
			realitySettings["shortIds"] = realityShortIDs
		} else {
			// Backward compat: single shortId
			realityShortID := getString("reality_short_id")
			if realityShortID != "" {
				realitySettings["shortId"] = realityShortID
			} else {
				// Auto-generate random shortId when not provided
				if generatedSID, err := generateRandomRealityShortIDHex(); err == nil {
					realitySettings["shortIds"] = []string{generatedSID}
					// Store in extensions for persistence (so subscription can use it)
					extensions["reality_short_ids"] = []string{generatedSID}
				}
			}
		}

		// Reality key management strategy:
		// 1. If external provides only one key (private OR public), return error
		// 2. If external provides both, validate format and optionally check if they match
		// 3. If neither provided, auto-generate a key pair
		realityPrivateKey := getString("reality_private_key")
		realityPublicKey := getString("reality_public_key")

		// External key validation: must provide both or neither
		if (realityPrivateKey != "" && realityPublicKey == "") || (realityPrivateKey == "" && realityPublicKey != "") {
			return nil, fmt.Errorf("reality_private_key and reality_public_key must be provided together")
		}

		// Validate external key format if provided
		if realityPrivateKey != "" && realityPublicKey != "" {
			if err := validateBase64Key(realityPrivateKey); err != nil {
				return nil, fmt.Errorf("invalid reality_private_key: %w", err)
			}
			if err := validateBase64Key(realityPublicKey); err != nil {
				return nil, fmt.Errorf("invalid reality_public_key: %w", err)
			}
			// TODO: Optionally verify if the key pair matches by deriving public from private
			// This requires computing the public key from the private key and comparing
		}

		// Auto-generate key pair if neither provided
		if realityPrivateKey == "" && realityPublicKey == "" {
			if pk, pubk, err := GenerateRealityKeyPairWithPublic(); err == nil {
				realityPrivateKey = pk
				realityPublicKey = pubk
			}
		}

		// Write to native realitySettings
		if realityPrivateKey != "" {
			realitySettings["privateKey"] = realityPrivateKey
		}
		// Note: publicKey is not a standard xray field, but we track it in extensions for persistence
		// Optional: show
		if show, ok := extensions["reality_show"].(bool); ok {
			realitySettings["show"] = show
		}
		// Optional: xver
		if xver, ok := extensions["reality_xver"].(int); ok {
			realitySettings["xver"] = xver
		}
		// Optional: maxClientVer
		if maxClientVer, ok := extensions["reality_max_client_ver"].(string); ok {
			realitySettings["maxClientVer"] = maxClientVer
		}
		// Optional: minClientVer
		if minClientVer, ok := extensions["reality_min_client_ver"].(string); ok {
			realitySettings["minClientVer"] = minClientVer
		}
		// Optional: maxTimeDiff
		if maxTimeDiff, ok := extensions["reality_max_time_diff"].(int64); ok {
			realitySettings["maxTimeDiff"] = maxTimeDiff
		}
		// Optional: mldsa65Seed
		if mldsa65Seed, ok := extensions["reality_mldsa65_seed"].(string); ok {
			realitySettings["mldsa65Seed"] = mldsa65Seed
		}
		if len(realitySettings) > 0 {
			stream["realitySettings"] = realitySettings
		}
	}

	return stream, nil
}

// GenerateSelfSignedCert generates a self-signed certificate for TLS.
// This is useful for demo/testing purposes where real certificates are not available.
// Returns certPEM, keyPEM strings that can be used in xray TLS settings.
func GenerateSelfSignedCert(commonName string) (certPEM, keyPEM string, err error) {
	if commonName == "" {
		commonName = "localhost"
	}

	// Generate RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // Valid for 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{commonName, "localhost"},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode to PEM format
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}))

	return certPEM, keyPEM, nil
}

// LoadCertFromFile loads a certificate from file path.
// This is a helper for loading pre-generated certificates.
func LoadCertFromFile(certFile, keyFile string) (string, string, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return "", "", fmt.Errorf("failed to read cert file: %w", err)
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return "", "", fmt.Errorf("failed to read key file: %w", err)
	}
	return string(certPEM), string(keyPEM), nil
}

// LoadOrGenerateCert loads a certificate from file, or generates a self-signed one if not found.
func LoadOrGenerateCert(certFile, keyFile, commonName string) (certPEM, keyPEM string, err error) {
	// Try to load from files first
	if certFile != "" && keyFile != "" {
		certPEM, keyPEM, err := LoadCertFromFile(certFile, keyFile)
		if err == nil {
			return certPEM, keyPEM, nil
		}
	}
	// Fall back to generating self-signed cert
	return GenerateSelfSignedCert(commonName)
}

// GenerateRealityKeyPair generates a proper X25519 key pair for Reality.
// Returns the private key as a base64-encoded string (xray expects this format).
// This uses the standard curve25519 scalar multiplication to generate a valid key.
//
// The generated key format matches what xray's "xray x25519" command produces:
// - Private key: 32 bytes, base64 encoded
// - Public key: 32 bytes, base64 encoded (returned as second value)
func GenerateRealityKeyPair() (string, error) {
	// Generate a random 32-byte scalar for X25519
	// This is the same method xray uses internally
	privateKey := [32]byte{}
	if _, err := rand.Read(privateKey[:]); err != nil {
		return "", fmt.Errorf("failed to generate random scalar: %w", err)
	}

	// Apply the standard X25519 clamping (per RFC 7748):
	// - Clear bits 0, 1, 2 of the first byte (byte[0] &= 248 = 11111000)
	// - Clear bit 7 of the last byte (byte[31] &= 127 = 01111111)
	// - Set bit 6 of the last byte (byte[31] |= 64 = 01000000)
	privateKey[0] &= 248
	privateKey[31] &= 127
	privateKey[31] |= 64

	// Convert to base64 string (xray expects base64 format for Reality private key)
	privateKeyB64 := base64.RawURLEncoding.EncodeToString(privateKey[:])
	return privateKeyB64, nil
}

// GenerateRealityKeyPairWithPublic generates a proper X25519 key pair for Reality,
// returning both the private key and public key as base64-encoded strings.
// Uses base64.RawURLEncoding (URL-safe, no padding) to match xray's expected format.
func GenerateRealityKeyPairWithPublic() (privateKeyB64, publicKeyB64 string, err error) {
	// Generate a random 32-byte scalar for X25519
	privateKey := [32]byte{}
	if _, err = rand.Read(privateKey[:]); err != nil {
		return "", "", fmt.Errorf("failed to generate random scalar: %w", err)
	}

	// Apply X25519 clamping
	privateKey[0] &= 248
	privateKey[31] &= 127
	privateKey[31] |= 64

	// Generate public key from private key using curve25519
	var publicKey [32]byte
	curve25519.ScalarBaseMult(&publicKey, &privateKey)

	// Use RawURLEncoding (URL-safe, no padding) to match xray's expected format
	privateKeyB64 = base64.RawURLEncoding.EncodeToString(privateKey[:])
	publicKeyB64 = base64.RawURLEncoding.EncodeToString(publicKey[:])

	return privateKeyB64, publicKeyB64, nil
}

// DeriveRealityPublicKey derives the X25519 public key from a base64-encoded private key.
// This is used by AddInboundNative to compute the public key for subscription URI generation.
func DeriveRealityPublicKey(privateKeyB64 string) (string, error) {
	privateKeyBytes, err := base64.RawURLEncoding.DecodeString(privateKeyB64)
	if err != nil {
		return "", fmt.Errorf("failed to decode private key: %w", err)
	}
	if len(privateKeyBytes) != 32 {
		return "", fmt.Errorf("invalid private key length: %d (expected 32)", len(privateKeyBytes))
	}
	var privArr [32]byte
	copy(privArr[:], privateKeyBytes)
	var pubArr [32]byte
	curve25519.ScalarBaseMult(&pubArr, &privArr)
	return base64.RawURLEncoding.EncodeToString(pubArr[:]), nil
}

// generateRandomRealityShortIDHex generates a random 16-character hex string (8 bytes) for Reality shortId.
// Each character is randomly selected from 0-9a-f.
func generateRandomRealityShortIDHex() (string, error) {
	// Generate 8 random bytes
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	// Convert to hex string (16 characters)
	return hex.EncodeToString(b), nil
}

// RealityKeyStore manages persistent storage of Reality keys.
type RealityKeyStore struct {
	keysFile string
}

// NewRealityKeyStore creates a new RealityKeyStore.
func NewRealityKeyStore(keysFile string) *RealityKeyStore {
	return &RealityKeyStore{keysFile: keysFile}
}

// SavedKey represents a saved Reality key pair.
type SavedKey struct {
	Tag       string `json:"tag"`
	Private   string `json:"private"`
	Public    string `json:"public"`
	CreatedAt int64  `json:"created_at"`
}

// LoadOrGenerateRealityKey loads an existing Reality key from file or generates a new one.
// Returns private key, public key, and whether a new key was generated.
func (ks *RealityKeyStore) LoadOrGenerateRealityKey(tag string) (privateKey, publicKey string, newlyGenerated bool, err error) {
	// Try to load existing keys
	keys, err := ks.loadKeys()
	if err != nil {
		// If file doesn't exist, we'll create new keys
		keys = make(map[string]SavedKey)
	}

	// Check if key exists for this tag
	if existing, ok := keys[tag]; ok {
		return existing.Private, existing.Public, false, nil
	}

	// Generate new key pair
	privateKey, publicKey, err = GenerateRealityKeyPairWithPublic()
	if err != nil {
		return "", "", false, err
	}

	// Save the new key
	keys[tag] = SavedKey{
		Tag:       tag,
		Private:   privateKey,
		Public:    publicKey,
		CreatedAt: time.Now().Unix(),
	}

	if err = ks.saveKeys(keys); err != nil {
		// Log but don't fail - we can still use the generated key
		fmt.Printf("Warning: failed to save Reality key: %v\n", err)
	}

	return privateKey, publicKey, true, nil
}

// GetRealityPublicKey retrieves the public key for a specific tag.
// Returns empty string if key doesn't exist.
func (ks *RealityKeyStore) GetRealityPublicKey(tag string) (string, error) {
	keys, err := ks.loadKeys()
	if err != nil {
		return "", err
	}
	if key, ok := keys[tag]; ok {
		return key.Public, nil
	}
	return "", nil
}

// GetAllRealityPublicKeys returns all saved Reality public keys.
func (ks *RealityKeyStore) GetAllRealityPublicKeys() (map[string]string, error) {
	keys, err := ks.loadKeys()
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for tag, key := range keys {
		result[tag] = key.Public
	}
	return result, nil
}

// DeleteRealityKey removes a saved Reality key.
func (ks *RealityKeyStore) DeleteRealityKey(tag string) error {
	keys, err := ks.loadKeys()
	if err != nil {
		return err
	}
	delete(keys, tag)
	return ks.saveKeys(keys)
}

func (ks *RealityKeyStore) loadKeys() (map[string]SavedKey, error) {
	data, err := os.ReadFile(ks.keysFile)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]SavedKey), nil
		}
		return nil, err
	}
	var keys map[string]SavedKey
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("failed to parse keys file: %w", err)
	}
	return keys, nil
}

func (ks *RealityKeyStore) saveKeys(keys map[string]SavedKey) error {
	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	// Ensure directory exists
	dir := filepath.Dir(ks.keysFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(ks.keysFile, data, 0600)
}

// validateRealityShortId validates a Reality shortId string.
// A valid shortId must be exactly 16 hex characters.
// Returns an error with field name and details if invalid.
func validateRealityShortId(fieldName string, shortId string) error {
	if len(shortId) != 16 {
		return fmt.Errorf("invalid %s: length must be 16 hex characters, got %d (%q)", fieldName, len(shortId), shortId)
	}
	// Validate hex characters
	for i, c := range shortId {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return fmt.Errorf("invalid %s: non-hex character %c at position %d (%q)", fieldName, c, i, shortId)
		}
	}
	return nil
}

// validateRealityShortIds validates Reality shortId parameters.
// Checks both reality_short_ids ([]string) and reality_short_id (string, backward compat).
// Returns error with clear message if any value is invalid.
func validateRealityShortIds(extensions map[string]any) error {
	// Validate reality_short_ids ([]string)
	if v, ok := extensions["reality_short_ids"].([]string); ok {
		for i, sid := range v {
			if err := validateRealityShortId(fmt.Sprintf("reality_short_ids[%d]", i), sid); err != nil {
				return err
			}
		}
	}
	// Validate reality_short_id (single, backward compat)
	if v, ok := extensions["reality_short_id"].(string); ok && v != "" {
		if err := validateRealityShortId("reality_short_id", v); err != nil {
			return err
		}
	}
	return nil
}

// validateBase64Key validates a base64 or base64url encoded key.
// Returns error if the key is invalid (not 32 bytes decoded, invalid encoding).
func validateBase64Key(key string) error {
	if key == "" {
		return nil // Empty is allowed (will be auto-generated)
	}
	// Try RawURLEncoding first (URL-safe, no padding)
	_, err := base64.RawURLEncoding.DecodeString(key)
	if err == nil && len(key) == 43 { // 32 bytes = 43 base64url chars without padding
		return nil
	}
	// Try StdEncoding (standard base64)
	_, err = base64.StdEncoding.DecodeString(key)
	if err == nil {
		return nil
	}
	return fmt.Errorf("invalid key format: expected base64url or base64 encoded 32-byte key")
}
