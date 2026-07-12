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

func TestEncryptDecryptAES_Roundtrip(t *testing.T) {
	key := bytes.Repeat([]byte("A"), 32)
	plaintext := []byte("secret message for testing")

	encrypted, err := rpc.EncryptWithAES(plaintext, key, nil)
	if err != nil {
		t.Fatalf("EncryptWithAES: %v", err)
	}
	if bytes.Equal(encrypted, plaintext) {
		t.Error("encrypted data should differ from plaintext")
	}

	decrypted, err := rpc.DecryptWithAES(encrypted, key, nil)
	if err != nil {
		t.Fatalf("DecryptWithAES: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptWithAES_RandomNonce(t *testing.T) {
	key := bytes.Repeat([]byte("B"), 32)
	plaintext := []byte("same message")

	enc1, err := rpc.EncryptWithAES(plaintext, key, nil)
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	enc2, err := rpc.EncryptWithAES(plaintext, key, nil)
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}
	if bytes.Equal(enc1, enc2) {
		t.Error("two encryptions of same plaintext should produce different ciphertext")
	}

	dec1, _ := rpc.DecryptWithAES(enc1, key, nil)
	dec2, _ := rpc.DecryptWithAES(enc2, key, nil)
	if !bytes.Equal(dec1, plaintext) || !bytes.Equal(dec2, plaintext) {
		t.Error("both should decrypt to original plaintext")
	}
}

func TestDecryptWithAES_TamperedData(t *testing.T) {
	key := bytes.Repeat([]byte("C"), 32)
	plaintext := []byte("authenticated message")

	encrypted, err := rpc.EncryptWithAES(plaintext, key, nil)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	tampered := make([]byte, len(encrypted))
	copy(tampered, encrypted)
	tampered[len(tampered)-1] ^= 0xFF

	_, err = rpc.DecryptWithAES(tampered, key, nil)
	if err == nil {
		t.Error("expected error when decrypting tampered ciphertext")
	}
}

func TestDecryptWithAES_TooShort(t *testing.T) {
	key := bytes.Repeat([]byte("D"), 32)
	_, err := rpc.DecryptWithAES([]byte("short"), key, nil)
	if err == nil {
		t.Error("expected error for ciphertext shorter than nonce + tag")
	}
}

func TestDecryptWithAES_Empty(t *testing.T) {
	key := bytes.Repeat([]byte("E"), 32)
	_, err := rpc.DecryptWithAES([]byte{}, key, nil)
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestEncryptDecryptAES_EmptyPlaintext(t *testing.T) {
	key := bytes.Repeat([]byte("E"), 32)
	encrypted, err := rpc.EncryptWithAES([]byte{}, key, nil)
	if err != nil {
		t.Fatalf("EncryptWithAES(empty): %v", err)
	}
	decrypted, err := rpc.DecryptWithAES(encrypted, key, nil)
	if err != nil {
		t.Fatalf("DecryptWithAES(empty): %v", err)
	}
	if len(decrypted) != 0 {
		t.Errorf("expected empty plaintext, got %d bytes", len(decrypted))
	}
}

func TestDecryptWithAES_WrongKey(t *testing.T) {
	key1 := bytes.Repeat([]byte("F"), 32)
	key2 := bytes.Repeat([]byte("G"), 32)

	encrypted, err := rpc.EncryptWithAES([]byte("secret"), key1, nil)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	_, err = rpc.DecryptWithAES(encrypted, key2, nil)
	if err == nil {
		t.Error("expected error when decrypting with wrong key")
	}
}

func TestEncryptWithAES_InvalidKey(t *testing.T) {
	_, err := rpc.EncryptWithAES([]byte("data"), []byte("short"), nil)
	if err == nil {
		t.Error("expected error for invalid key length")
	}
}

func TestGetRpcKeyByToken_HKDF(t *testing.T) {
	// Always 32 bytes, deterministic, and different tokens -> different keys.
	long := rpc.GetRpcKeyByToken("abcdefghijklmnopqrstuvwxyz1234567890")
	if len(long) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(long))
	}
	if !bytes.Equal(long, rpc.GetRpcKeyByToken("abcdefghijklmnopqrstuvwxyz1234567890")) {
		t.Error("key derivation must be deterministic (both sides must agree)")
	}
	if bytes.Equal(long, rpc.GetRpcKeyByToken("a-different-token-value")) {
		t.Error("different tokens must derive different keys")
	}

	// A weak/short token no longer degrades to a low-entropy or constant key
	// (the old scheme padded with 0x20). Empty and short are distinct and are
	// NOT the old constant.
	empty := rpc.GetRpcKeyByToken("")
	short := rpc.GetRpcKeyByToken("short")
	if bytes.Equal(empty, short) {
		t.Error("empty and short tokens must not derive the same key")
	}
	if bytes.Equal(empty, bytes.Repeat([]byte{0x20}, 32)) {
		t.Error("empty token must not derive the old constant 0x20*32 key")
	}
}

// TestEncryptDecryptAES_AADMismatch verifies GCM rejects a decrypt with a
// different AAD than was used to encrypt — the property that binds the
// protocol version + message type in the codec.
func TestEncryptDecryptAES_AADMismatch(t *testing.T) {
	key := bytes.Repeat([]byte("K"), 32)
	enc, err := rpc.EncryptWithAES([]byte("payload"), key, []byte("aad-A"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := rpc.DecryptWithAES(enc, key, []byte("aad-B")); err == nil {
		t.Error("decrypt with a different AAD must fail")
	}
	if _, err := rpc.DecryptWithAES(enc, key, []byte("aad-A")); err != nil {
		t.Errorf("decrypt with the matching AAD must succeed, got %v", err)
	}
}

func TestEncryptMessageCodec_Name(t *testing.T) {
	codec := rpc.NewEncryptMessageCodec("test-token-placeholder")
	if codec.Name() != "EncryptMessageCodec" {
		t.Errorf("Name: got %q, want %q", codec.Name(), "EncryptMessageCodec")
	}
}
