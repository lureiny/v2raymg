package rpc_test

import (
	"bytes"
	"testing"

	rpc "github.com/lureiny/v2raymg/pkg/common/rpc"
)

func TestPKCS7Padding_Various(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		blockSize int
	}{
		{"empty", []byte{}, 16},
		{"1 byte", []byte{0x01}, 16},
		{"15 bytes", bytes.Repeat([]byte{0xAA}, 15), 16},
		{"16 bytes (full block)", bytes.Repeat([]byte{0xBB}, 16), 16},
		{"17 bytes", bytes.Repeat([]byte{0xCC}, 17), 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			padded := rpc.PKCS7Padding(tt.input, tt.blockSize)
			if len(padded)%tt.blockSize != 0 {
				t.Errorf("padded length %d not multiple of block size %d", len(padded), tt.blockSize)
			}
			if len(padded) == 0 {
				t.Error("padded result should not be empty")
			}
		})
	}
}

func TestPKCS7UnPadding_Normal(t *testing.T) {
	original := []byte("hello world")
	padded := rpc.PKCS7Padding(original, 16)
	unpadded, err := rpc.PKCS7UnPadding(padded)
	if err != nil {
		t.Fatalf("PKCS7UnPadding: %v", err)
	}
	if !bytes.Equal(unpadded, original) {
		t.Errorf("got %v, want %v", unpadded, original)
	}
}

func TestPKCS7UnPadding_Empty(t *testing.T) {
	_, err := rpc.PKCS7UnPadding([]byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestPKCS7UnPadding_Invalid(t *testing.T) {
	// Last byte indicates more padding than data length
	_, err := rpc.PKCS7UnPadding([]byte{0xFF})
	if err == nil {
		t.Error("expected error for invalid padding")
	}
}

func TestEncryptDecryptAES_Roundtrip(t *testing.T) {
	key := bytes.Repeat([]byte("A"), 32) // 32-byte key for AES-256
	plaintext := []byte("secret message for testing")

	encrypted, err := rpc.EncryptWithAES(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptWithAES: %v", err)
	}
	if bytes.Equal(encrypted, plaintext) {
		t.Error("encrypted data should differ from plaintext")
	}

	decrypted, err := rpc.DecryptWithAES(encrypted, key)
	if err != nil {
		t.Fatalf("DecryptWithAES: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptWithAES_InvalidKey(t *testing.T) {
	_, err := rpc.EncryptWithAES([]byte("data"), []byte("short"))
	if err == nil {
		t.Error("expected error for invalid key length")
	}
}

func TestGetRpcKeyByToken_Long(t *testing.T) {
	token := "abcdefghijklmnopqrstuvwxyz1234567890" // 36 chars > 32
	key := rpc.GetRpcKeyByToken(token)
	if len(key) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(key))
	}
	if string(key) != token[:32] {
		t.Errorf("expected first 32 bytes of token")
	}
}

func TestGetRpcKeyByToken_Short(t *testing.T) {
	token := "short"
	key := rpc.GetRpcKeyByToken(token)
	if len(key)%32 != 0 {
		t.Errorf("expected padded to multiple of 32, got %d", len(key))
	}
	// First bytes should be the token
	if string(key[:len(token)]) != token {
		t.Error("key should start with the token")
	}
}

func TestEncryptMessageCodec_Name(t *testing.T) {
	codec := rpc.NewEncryptMessageCodec("test-token-placeholder")
	if codec.Name() != "EncryptMessageCodec" {
		t.Errorf("Name: got %q, want %q", codec.Name(), "EncryptMessageCodec")
	}
}
