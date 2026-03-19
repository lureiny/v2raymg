package certmgmtlego

import (
	"crypto"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

const (
	accountsFolder  = "accounts"
	keysFolder      = "keys"
	accountFileName = "account.json"
)

// accountDirPath returns the per-CA, per-email account directory, matching lego conventions.
func accountDirPath(basePath, caURL, email string) (string, error) {
	serverURL, err := url.Parse(caURL)
	if err != nil {
		return "", fmt.Errorf("parse CA URL: %w", err)
	}
	serverPath := strings.NewReplacer(":", "_", "/", string(os.PathSeparator)).Replace(serverURL.Host)
	return filepath.Join(basePath, accountsFolder, serverPath, email), nil
}

// SaveAccount persists the ACME account data to disk.
// Layout:
//
//	<basePath>/accounts/<ca-host>/<email>/account.json
//	<basePath>/accounts/<ca-host>/<email>/keys/<email>.key
func SaveAccount(basePath, caURL, email string, data *domain.AccountData) error {
	dir, err := accountDirPath(basePath, caURL, email)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrStorageIO, err)
	}
	keysDir := filepath.Join(dir, keysFolder)
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return fmt.Errorf("%w: mkdir: %v", domain.ErrStorageIO, err)
	}

	// Write account.json
	jsonBytes, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return fmt.Errorf("%w: marshal account: %v", domain.ErrStorageIO, err)
	}
	accountPath := filepath.Join(dir, accountFileName)
	if err := os.WriteFile(accountPath, jsonBytes, 0600); err != nil {
		return fmt.Errorf("%w: write account.json: %v", domain.ErrStorageIO, err)
	}

	// Write key file
	if len(data.PrivateKeyPEM) > 0 {
		keyPath := filepath.Join(keysDir, email+".key")
		if err := os.WriteFile(keyPath, data.PrivateKeyPEM, 0600); err != nil {
			return fmt.Errorf("%w: write key: %v", domain.ErrStorageIO, err)
		}
	}
	return nil
}

// LoadAccount reads and deserializes the ACME account from disk.
func LoadAccount(basePath, caURL, email string) (*domain.AccountData, error) {
	dir, err := accountDirPath(basePath, caURL, email)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrStorageIO, err)
	}
	accountPath := filepath.Join(dir, accountFileName)
	raw, err := os.ReadFile(accountPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no account yet
		}
		return nil, fmt.Errorf("%w: read account.json: %v", domain.ErrStorageIO, err)
	}
	var data domain.AccountData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("%w: unmarshal account: %v", domain.ErrStorageIO, err)
	}
	// Load key PEM if not embedded in JSON
	if len(data.PrivateKeyPEM) == 0 {
		keyPath := filepath.Join(dir, keysFolder, email+".key")
		keyPEM, err := os.ReadFile(keyPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: read key: %v", domain.ErrStorageIO, err)
		}
		data.PrivateKeyPEM = keyPEM
	}
	return &data, nil
}

// GetOrCreateAccountKey loads the private key from disk, or generates and saves a new one.
func GetOrCreateAccountKey(basePath, caURL, email string, keyType certcrypto.KeyType) (crypto.PrivateKey, error) {
	dir, err := accountDirPath(basePath, caURL, email)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrStorageIO, err)
	}
	keysDir := filepath.Join(dir, keysFolder)
	keyPath := filepath.Join(keysDir, email+".key")

	// Try to load existing key
	raw, err := os.ReadFile(keyPath)
	if err == nil {
		key, err := certcrypto.ParsePEMPrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("parse existing key: %w", err)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: read key: %v", domain.ErrStorageIO, err)
	}

	// Generate a new key
	key, err := certcrypto.GeneratePrivateKey(keyType)
	if err != nil {
		return nil, fmt.Errorf("generate private key: %w", err)
	}
	pemBytes := certcrypto.PEMEncode(key)

	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return nil, fmt.Errorf("%w: mkdir keys: %v", domain.ErrStorageIO, err)
	}
	if err := os.WriteFile(keyPath, pemBytes, 0600); err != nil {
		return nil, fmt.Errorf("%w: write key: %v", domain.ErrStorageIO, err)
	}
	return key, nil
}
