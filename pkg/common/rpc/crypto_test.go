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
	_, err := rpc.PKCS7UnPadding([]byte{0xFF})
	if err == nil {
		t.Error("expected error for invalid padding")
	}
}

// Test CBC encrypt + decrypt roundtrip (legacy path).
func TestEncryptDecryptAES_CBC_Roundtrip(t *testing.T) {
	key := bytes.Repeat([]byte("A"), 32)
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

// Test GCM encrypt + decrypt roundtrip.
func TestEncryptDecryptAES_GCM_Roundtrip(t *testing.T) {
	key := bytes.Repeat([]byte("B"), 32)
	plaintext := []byte("secret message for GCM testing")

	encrypted, err := rpc.EncryptWithAESGCM(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptWithAESGCM: %v", err)
	}

	decrypted, err := rpc.DecryptWithAES(encrypted, key)
	if err != nil {
		t.Fatalf("DecryptWithAES (GCM): %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted: got %q, want %q", decrypted, plaintext)
	}
}

// Test GCM produces different ciphertext each time (random nonce).
func TestEncryptWithAESGCM_RandomNonce(t *testing.T) {
	key := bytes.Repeat([]byte("C"), 32)
	plaintext := []byte("same message")

	enc1, err := rpc.EncryptWithAESGCM(plaintext, key)
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	enc2, err := rpc.EncryptWithAESGCM(plaintext, key)
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}
	if bytes.Equal(enc1, enc2) {
		t.Error("two encryptions of same plaintext should produce different ciphertext")
	}

	// Both should decrypt correctly
	dec1, _ := rpc.DecryptWithAES(enc1, key)
	dec2, _ := rpc.DecryptWithAES(enc2, key)
	if !bytes.Equal(dec1, plaintext) || !bytes.Equal(dec2, plaintext) {
		t.Error("both should decrypt to original plaintext")
	}
}

// Test GCM tamper detection.
func TestDecryptWithAES_GCM_TamperedData(t *testing.T) {
	key := bytes.Repeat([]byte("D"), 32)
	plaintext := []byte("authenticated message")

	encrypted, err := rpc.EncryptWithAESGCM(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	tampered := make([]byte, len(encrypted))
	copy(tampered, encrypted)
	tampered[len(tampered)-1] ^= 0xFF

	_, err = rpc.DecryptWithAES(tampered, key)
	if err == nil {
		t.Error("expected error when decrypting tampered ciphertext")
	}
}

// Test decrypt with empty data.
func TestDecryptWithAES_Empty(t *testing.T) {
	key := bytes.Repeat([]byte("E"), 32)
	_, err := rpc.DecryptWithAES([]byte{}, key)
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestEncryptWithAES_InvalidKey(t *testing.T) {
	_, err := rpc.EncryptWithAES([]byte("data"), []byte("short"))
	if err == nil {
		t.Error("expected error for invalid key length")
	}
}

func TestEncryptWithAESGCM_InvalidKey(t *testing.T) {
	_, err := rpc.EncryptWithAESGCM([]byte("data"), []byte("short"))
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
