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

// EncryptWithAES encrypts plaintext using AES-256-GCM with a random nonce.
// Output format: nonce (12 bytes) + ciphertext + GCM tag (16 bytes).
func EncryptWithAES(plaintext, key []byte) ([]byte, error) {
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

// DecryptWithAES decrypts data using AES-256-GCM.
func DecryptWithAES(encryptedData, key []byte) ([]byte, error) {
	if len(encryptedData) == 0 {
		return nil, fmt.Errorf("empty data")
	}

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
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := encryptedData[:nonceSize], encryptedData[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
