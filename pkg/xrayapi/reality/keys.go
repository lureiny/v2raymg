// Package reality provides Reality protocol key generation and management.
// This is a standalone implementation without depending on xray-core.
package reality

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/curve25519"
)

// KeyPair represents a Reality X25519 key pair.
type KeyPair struct {
	Private string `json:"private"` // Base64-encoded private key
	Public  string `json:"public"`  // Base64-encoded public key
}

// SavedKey represents a persisted Reality key.
type SavedKey struct {
	Tag       string `json:"tag"`
	Private   string `json:"private"`
	Public    string `json:"public"`
	CreatedAt int64  `json:"created_at"`
}

// KeyStore manages persistent storage of Reality keys.
type KeyStore struct {
	keysFile string
}

// NewKeyStore creates a new Reality key store.
func NewKeyStore(keysFile string) *KeyStore {
	return &KeyStore{keysFile: keysFile}
}

// GenerateKeyPair generates a proper X25519 key pair for Reality.
// Returns the private key and public key as base64-encoded strings.
//
// This uses the standard curve25519 scalar multiplication:
// - Generate a random 32-byte scalar
// - Apply X25519 clamping (per RFC 7748)
// - Compute public key using ScalarBaseMult
func GenerateKeyPair() (privateKey, publicKey string, err error) {
	// Generate a random 32-byte scalar for X25519
	var privateKeyBytes [32]byte
	if _, err = rand.Read(privateKeyBytes[:]); err != nil {
		return "", "", fmt.Errorf("failed to generate random scalar: %w", err)
	}

	// Apply X25519 clamping (per RFC 7748):
	// - Clear bits 0, 1, 2 of the first byte (byte[0] &= 248)
	// - Clear bit 7 of the last byte (byte[31] &= 127)
	// - Set bit 6 of the last byte (byte[31] |= 64)
	privateKeyBytes[0] &= 248
	privateKeyBytes[31] &= 127
	privateKeyBytes[31] |= 64

	// Generate public key from private key using curve25519
	var publicKeyBytes [32]byte
	curve25519.ScalarBaseMult(&publicKeyBytes, &privateKeyBytes)

	privateKey = base64.StdEncoding.EncodeToString(privateKeyBytes[:])
	publicKey = base64.StdEncoding.EncodeToString(publicKeyBytes[:])

	return privateKey, publicKey, nil
}

// GenerateKeyPairWithDefaults generates a key pair and returns only the private key.
// This matches the signature expected by xray inbound configuration.
func GenerateKeyPairWithDefaults() (string, error) {
	privateKey, _, err := GenerateKeyPair()
	return privateKey, err
}

// LoadOrGenerate loads an existing key from file or generates a new one.
// Returns private key, public key, and whether a new key was generated.
func (ks *KeyStore) LoadOrGenerate(tag string) (privateKey, publicKey string, newlyGenerated bool, err error) {
	// Try to load existing keys
	keys, err := ks.loadKeys()
	if err != nil {
		keys = make(map[string]SavedKey)
	}

	// Check if key exists for this tag
	if existing, ok := keys[tag]; ok {
		return existing.Private, existing.Public, false, nil
	}

	// Generate new key pair
	privateKey, publicKey, err = GenerateKeyPair()
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

// GetPublicKey retrieves the public key for a specific tag.
func (ks *KeyStore) GetPublicKey(tag string) (string, error) {
	keys, err := ks.loadKeys()
	if err != nil {
		return "", err
	}
	if key, ok := keys[tag]; ok {
		return key.Public, nil
	}
	return "", nil
}

// GetAllPublicKeys returns all saved public keys.
func (ks *KeyStore) GetAllPublicKeys() (map[string]string, error) {
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

// DeleteKey removes a saved key.
func (ks *KeyStore) DeleteKey(tag string) error {
	keys, err := ks.loadKeys()
	if err != nil {
		return err
	}
	delete(keys, tag)
	return ks.saveKeys(keys)
}

// ListTags returns all saved key tags.
func (ks *KeyStore) ListTags() ([]string, error) {
	keys, err := ks.loadKeys()
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(keys))
	for tag := range keys {
		tags = append(tags, tag)
	}
	return tags, nil
}

func (ks *KeyStore) loadKeys() (map[string]SavedKey, error) {
	data, err := os.ReadFile(ks.keysFile)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]SavedKey), nil
		}
		return nil, fmt.Errorf("failed to read keys file: %w", err)
	}
	var keys map[string]SavedKey
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("failed to parse keys file: %w", err)
	}
	return keys, nil
}

func (ks *KeyStore) saveKeys(keys map[string]SavedKey) error {
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
