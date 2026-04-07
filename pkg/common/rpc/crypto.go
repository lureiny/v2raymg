package rpc

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// PKCS7Padding pads plaintext to a multiple of blockSize using PKCS7.
func PKCS7Padding(plaintext []byte, blockSize int) []byte {
	paddingNum := blockSize - len(plaintext)%blockSize
	plaintextWithPadding := bytes.Repeat([]byte{byte(paddingNum)}, paddingNum)
	return append(plaintext, plaintextWithPadding...)
}

// PKCS7UnPadding removes PKCS7 padding.
func PKCS7UnPadding(plaintextWithPadding []byte) ([]byte, error) {
	length := len(plaintextWithPadding)
	if length == 0 {
		return nil, fmt.Errorf("empty data")
	}
	unpaddingNum := int(plaintextWithPadding[length-1])
	if length-unpaddingNum < 0 {
		return nil, fmt.Errorf("invalid data")
	}
	return plaintextWithPadding[:(length - unpaddingNum)], nil
}

// EncryptWithAES encrypts plaintext using AES-256-CBC.
//
// This is the legacy encryption format, kept for backward compatibility during
// rolling upgrades. A future version will switch to AES-256-GCM (EncryptWithAESGCM).
func EncryptWithAES(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	blockSize := block.BlockSize()
	plaintextWithPadding := PKCS7Padding(plaintext, blockSize)
	blockMode := cipher.NewCBCEncrypter(block, key[:blockSize])
	encryptedData := make([]byte, len(plaintextWithPadding))
	blockMode.CryptBlocks(encryptedData, plaintextWithPadding)
	return encryptedData, nil
}

// EncryptWithAESGCM encrypts plaintext using AES-256-GCM with a random nonce.
// Output format: nonce (12 bytes) + ciphertext + GCM tag (16 bytes).
//
// This will replace EncryptWithAES once all nodes are upgraded.
func EncryptWithAESGCM(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptWithAES decrypts data using a try-GCM-then-fallback-CBC strategy.
//
// It first attempts AES-256-GCM decryption (nonce + ciphertext + tag format).
// If GCM authentication fails, it falls back to legacy AES-256-CBC decryption.
// This allows seamless rolling upgrades: new nodes can receive from both
// old (CBC) and new (GCM) senders.
func DecryptWithAES(encryptedData, key []byte) ([]byte, error) {
	if len(encryptedData) == 0 {
		return nil, fmt.Errorf("empty data")
	}

	// Try GCM first.
	if plaintext, err := decryptGCM(encryptedData, key); err == nil {
		return plaintext, nil
	}

	// Fallback to CBC.
	return decryptCBC(encryptedData, key)
}

func decryptGCM(encryptedData, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(encryptedData) < nonceSize+gcm.Overhead() {
		return nil, fmt.Errorf("ciphertext too short for GCM")
	}

	nonce, ciphertext := encryptedData[:nonceSize], encryptedData[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func decryptCBC(encryptedData, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	blockSize := block.BlockSize()
	if len(encryptedData)%blockSize != 0 {
		return nil, fmt.Errorf("ciphertext length not multiple of block size")
	}

	blockMode := cipher.NewCBCDecrypter(block, key[:blockSize])
	plaintextWithPadding := make([]byte, len(encryptedData))
	blockMode.CryptBlocks(plaintextWithPadding, encryptedData)
	return PKCS7UnPadding(plaintextWithPadding)
}
