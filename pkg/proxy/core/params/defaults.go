// Package params normalises the map-of-strings/any payload that RPC FastAdd
// handlers hand down to a container's FastAddInbound. Two jobs:
//
//  1. Protocol credential fill-in. Missing vmess uuid / trojan password / ss
//     password+cipher are replaced with freshly generated values. This
//     matches xray's long-standing "caller picks a protocol + port, we
//     generate the rest" UX and pulls mihomo up to parity without touching
//     its adapter layer (which remains strict-by-design).
//
//  2. TLS cert-source normalisation for protocols where TLS is mandatory
//     (currently trojan). Callers can present the cert in any of four forms:
//
//     - cert_file + key_file  absolute paths on disk, caller-managed
//     - certificate + key     PEM content; materialised under scratchDir
//     - domain                looked up via CertManager
//     - self_signed: true     generated here; materialised under scratchDir
//
//     On success, params["cert_file"] and params["key_file"] both hold
//     absolute paths. Every downstream container consumes a single shape.
//
// The layer is container-agnostic by design: the rule is "what does
// protocol X require", not "what does container Y want". Per-container
// constraints (e.g. mihomo's SAFE_PATHS requiring cert files under DataDir)
// are handled by the caller picking an appropriate scratchDir and wiring
// whatever env/config the container needs to accept externally-supplied
// paths.
package params

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CertManager is the minimum surface FillDefaults needs to resolve the
// "domain" cert source. Satisfied by pkg/certmgmt/service.Manager's
// GetCert method (via a small adapter if the return type differs). Pass
// nil to disable domain lookup — a domain param will then be rejected
// with a clear error.
type CertManager interface {
	GetCert(domain string) *CertRecord
}

// CertRecord is the minimal view FillDefaults needs from certmgmt. Using
// a local struct keeps this package's dependency graph flat — callers
// adapt pkg/certmgmt/domain.CertificateRecord to this shape in a few lines.
type CertRecord struct {
	CertFile string
	KeyFile  string
}

// FillDefaults runs the full normalisation pass on params for the given
// protocol. scratchDir is where PEM-content and self-signed cert material
// is written; it must be writable. For containers that restrict cert paths
// (e.g. mihomo's SAFE_PATHS), scratchDir should be a directory the
// container trusts.
func FillDefaults(params map[string]any, protocol string, certMgr CertManager, scratchDir string) error {
	if params == nil {
		return fmt.Errorf("params: nil params map")
	}
	fillCredentials(params, protocol)
	if needsTLS(protocol, params) {
		if err := resolveCertSource(params, certMgr, scratchDir); err != nil {
			return err
		}
	}
	return nil
}

// needsTLS reports whether the protocol is one that requires ordinary TLS
// certificate material to be present after FillDefaults.
//
//   - trojan: mihomo's trojan listener refuses to start without
//     cert/reality/ss (observed in mihomo stage 11+ E2E). Trojan+Reality
//     carries its server auth in reality-config, so it must not be forced
//     through cert_file/key_file normalisation.
//   - hysteria2 / tuic / anytls: QUIC-over-TLS protocols that can't
//     bootstrap a session without a server certificate. mihomo's Alpha
//     struct tags for these listeners mark `certificate`/`private-key` as
//     required (non-omitempty) — see
//     pkg/proxy/core/params/protocolparams notes.
func needsTLS(protocol string, params map[string]any) bool {
	switch strings.ToLower(protocol) {
	case "vless", "vmess":
		security, _ := params["security"].(string)
		return strings.EqualFold(strings.TrimSpace(security), "tls")
	case "trojan":
		if security, _ := params["security"].(string); strings.EqualFold(strings.TrimSpace(security), "reality") {
			return false
		}
		return true
	case "hysteria2", "tuic", "anytls":
		return true
	}
	return false
}

func fillCredentials(params map[string]any, protocol string) {
	switch strings.ToLower(protocol) {
	case "vmess", "vless":
		// Both vmess and vless are uuid-based. xray's inbound_adapter
		// and mihomo's adapter both expect a UUID string; generating one
		// when the caller omits it matches the existing vmess UX.
		if isEmpty(params, "uuid") {
			params["uuid"] = uuid.NewString()
		}
	case "trojan":
		if isEmpty(params, "password") {
			params["password"] = randomHex(16)
		}
	case "shadowsocks", "ss":
		// Cipher must be set before password so randomSSPassword can pick
		// the right key length (SIP022 requires base64-encoded 16 or 32 bytes).
		if isEmpty(params, "cipher") {
			// AEAD-2022 default. Matches the subscription converter's
			// fallback in pkg/proxy/core/subscription/converter/clash.go
			// convertShadowsocks, so listener and client-side subscription
			// agree on the same cipher when the caller didn't pick one.
			params["cipher"] = "2022-blake3-aes-256-gcm"
		}
		if isEmpty(params, "password") {
			cipher, _ := params["cipher"].(string)
			params["password"] = randomSSPassword(cipher)
		}
	case "hysteria2":
		// Hysteria2 auth is a single password (mihomo Alpha
		// Hysteria2Option.Users is map[string]string but v2raymg uses the
		// shared-credential model — one inbound carries one listener
		// password and all users are distinguished at the forward layer,
		// mirroring the trojan / ss model).
		if isEmpty(params, "password") {
			params["password"] = randomHex(16)
		}
	case "tuic":
		// TUIC v5 identifies a client by (uuid, password); v4 uses token
		// only. FillDefaults seeds both so the caller's choice of v5 vs
		// v4 doesn't need to gate the default path. Adapters that speak
		// v4 ignore the password.
		if isEmpty(params, "uuid") {
			params["uuid"] = uuid.NewString()
		}
		if isEmpty(params, "password") {
			params["password"] = randomHex(16)
		}
	case "anytls":
		if isEmpty(params, "password") {
			params["password"] = randomHex(16)
		}
	}
}

// resolveCertSource reads the cert-source fields in priority order and
// writes cert_file/key_file into params. Callers that already provided
// cert_file + key_file pass through untouched. Any other form is
// materialised to two files on disk under scratchDir and the paths written
// back into params. Returns an error if no cert source was provided (the
// trojan gate).
func resolveCertSource(params map[string]any, certMgr CertManager, scratchDir string) error {
	// Every case below writes params["cert_source"] alongside
	// cert_file/key_file. Downstream containers (mihomo) use it to decide
	// whether the files are v2raymg-managed ("pem"/"self_signed", safe to
	// delete on inbound removal) or externally-managed ("file"/"domain",
	// must not be touched).

	// 1. Explicit paths — trust caller, do nothing beyond tagging source.
	if cf, _ := params["cert_file"].(string); cf != "" {
		if kf, _ := params["key_file"].(string); kf != "" {
			params["cert_source"] = "file"
			return nil
		}
		return fmt.Errorf("params: cert_file provided without key_file")
	}
	if kf, _ := params["key_file"].(string); kf != "" {
		return fmt.Errorf("params: key_file provided without cert_file")
	}

	// 2. PEM content — write to scratchDir, return paths.
	certPEM, _ := params["certificate"].(string)
	keyPEM, _ := params["key"].(string)
	if certPEM != "" || keyPEM != "" {
		if certPEM == "" || keyPEM == "" {
			return fmt.Errorf("params: certificate and key PEM must both be provided")
		}
		cp, kp, err := writePEMToScratch(scratchDir, certPEM, keyPEM)
		if err != nil {
			return fmt.Errorf("params: materialise PEM content: %w", err)
		}
		params["cert_file"] = cp
		params["key_file"] = kp
		params["cert_source"] = "pem"
		return nil
	}

	// 3. Domain lookup via certmgr.
	if d, ok := params["domain"].(string); ok && d != "" {
		if certMgr == nil {
			return fmt.Errorf("params: domain %q provided but no CertManager configured", d)
		}
		rec := certMgr.GetCert(d)
		if rec == nil {
			return fmt.Errorf("params: no certificate found for domain %q; issue one before adding the inbound", d)
		}
		if rec.CertFile == "" || rec.KeyFile == "" {
			return fmt.Errorf("params: CertManager returned an incomplete record for domain %q", d)
		}
		params["cert_file"] = rec.CertFile
		params["key_file"] = rec.KeyFile
		params["cert_source"] = "domain"
		return nil
	}

	// 4. Self-signed on demand.
	if ss, _ := params["self_signed"].(bool); ss {
		serverName, _ := params["server_name"].(string)
		if serverName == "" {
			serverName = "localhost"
		}
		cp, kp, err := generateSelfSigned(scratchDir, serverName)
		if err != nil {
			return fmt.Errorf("params: self-signed cert generation: %w", err)
		}
		params["cert_file"] = cp
		params["key_file"] = kp
		params["cert_source"] = "self_signed"
		return nil
	}

	return fmt.Errorf("params: TLS-required protocol but no cert source " +
		"(provide one of cert_file+key_file, certificate+key, domain, self_signed=true)")
}

// writePEMToScratch writes cert/key PEM strings to two files under scratchDir
// and returns their absolute paths. The file name is derived from a short
// hash of the cert content so repeated calls with the same PEM reuse the
// same files (idempotent, saves disk churn across reconciles).
func writePEMToScratch(scratchDir, certPEM, keyPEM string) (certFile, keyFile string, err error) {
	if err := os.MkdirAll(scratchDir, 0700); err != nil {
		return "", "", fmt.Errorf("mkdir scratch %q: %w", scratchDir, err)
	}
	// Use the first 16 hex chars of the cert PEM as a stable, collision-
	// resistant enough (for this use case) identifier. Two inbounds with
	// the same PEM share the same pair of files — safe because the content
	// is identical.
	digest := hashShort(certPEM)
	certFile = filepath.Join(scratchDir, "pem-"+digest+"-cert.pem")
	keyFile = filepath.Join(scratchDir, "pem-"+digest+"-key.pem")
	if err := writeFileIfChanged(certFile, []byte(certPEM), 0600); err != nil {
		return "", "", fmt.Errorf("write cert: %w", err)
	}
	if err := writeFileIfChanged(keyFile, []byte(keyPEM), 0600); err != nil {
		return "", "", fmt.Errorf("write key: %w", err)
	}
	return certFile, keyFile, nil
}

// generateSelfSigned creates a fresh self-signed RSA cert for commonName
// and materialises it under scratchDir. Each call produces a new key; the
// file name is randomised so concurrent FastAdds don't collide.
func generateSelfSigned(scratchDir, commonName string) (certFile, keyFile string, err error) {
	if err := os.MkdirAll(scratchDir, 0700); err != nil {
		return "", "", fmt.Errorf("mkdir scratch %q: %w", scratchDir, err)
	}
	certPEM, keyPEM, err := generateSelfSignedPEM(commonName)
	if err != nil {
		return "", "", err
	}
	// Include a random suffix so two concurrent FastAdds for the same CN
	// produce distinct files instead of racing each other.
	suffix := randomHex(4)
	certFile = filepath.Join(scratchDir, "ss-"+commonName+"-"+suffix+"-cert.pem")
	keyFile = filepath.Join(scratchDir, "ss-"+commonName+"-"+suffix+"-key.pem")
	if err := os.WriteFile(certFile, []byte(certPEM), 0600); err != nil {
		return "", "", fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(keyFile, []byte(keyPEM), 0600); err != nil {
		return "", "", fmt.Errorf("write key: %w", err)
	}
	return certFile, keyFile, nil
}

// generateSelfSignedPEM returns fresh self-signed RSA cert + key in PEM
// form, valid for 10 years, with SANs {commonName, "localhost"}. Lifted
// in spirit from pkg/proxy/containers/xray/inbound_adapter.go's
// GenerateSelfSignedCert to keep this package stdlib-only.
func generateSelfSignedPEM(commonName string) (certPEM, keyPEM string, err error) {
	if commonName == "" {
		commonName = "localhost"
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate private key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("generate serial: %w", err)
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dedupeStrings([]string{commonName, "localhost"}),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", fmt.Errorf("create certificate: %w", err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}))
	return certPEM, keyPEM, nil
}

// writeFileIfChanged writes data to path iff the current contents differ
// from data. Idempotent; avoids dirtying mtime on reconcile. Directory is
// assumed to exist already.
func writeFileIfChanged(path string, data []byte, perm os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytesEqual(existing, data) {
		return nil
	}
	return os.WriteFile(path, data, perm)
}

// ---------- small utilities ----------

func isEmpty(params map[string]any, key string) bool {
	v, ok := params[key]
	if !ok {
		return true
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return s == ""
}

// randomSSPassword generates a valid password for the given Shadowsocks cipher.
//
// SIP022 (2022-blake3-* family) requires a raw-key-material password encoded as
// standard base64:
//   - 2022-blake3-aes-256-gcm: 32-byte key → 44-char base64 string
//   - 2022-blake3-aes-128-gcm: 16-byte key → 24-char base64 string
//
// Classic AEAD ciphers (aes-256-gcm, chacha20-ietf-poly1305, …) accept any
// opaque string, so we continue to use a hex string for those.
func randomSSPassword(cipher string) string {
	if strings.HasPrefix(cipher, "2022-") {
		keyLen := 32
		if strings.Contains(cipher, "128") {
			keyLen = 16
		}
		key := make([]byte, keyLen)
		if _, err := rand.Read(key); err != nil {
			return fmt.Sprintf("fallback-%x", time.Now().UnixNano())
		}
		return base64.StdEncoding.EncodeToString(key)
	}
	return randomHex(16)
}

// randomHex returns 2n hex characters sourced from crypto/rand. Used for
// password generation and cert-file suffix uniqueness. On the vanishingly
// rare event of crypto/rand failure, falls back to a time-seeded
// placeholder so no code path ever hands back an empty secret.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback-%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// hashShort returns the first 16 hex chars of a SHA-like digest of s.
// Uses SHA-256 via stdlib; not collision-free in theory but for the cert
// content identifier use case (where collisions mean "reuse file — safe
// since content is identical") it's more than enough.
func hashShort(s string) string {
	// Avoid pulling crypto/sha256 into the top-level imports when the only
	// use is this local helper. The file-name doesn't need cryptographic
	// strength; use a simple FNV-1a fold over the bytes for speed.
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return fmt.Sprintf("%016x", h)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
