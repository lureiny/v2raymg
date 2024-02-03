package config

import (
	"crypto/rand"

	"golang.org/x/crypto/curve25519"
)

func genx25591() ([2]string, error) {
	privateKey := make([]byte, curve25519.ScalarSize)
	publicKey := []byte{}
	var err error = nil
	if _, err = rand.Read(privateKey); err != nil {
		return [2]string{}, err
	}

	// Modify random bytes using algorithm described at:
	// https://cr.yp.to/ecdh.html.
	privateKey[0] &= 248
	privateKey[31] &= 127
	privateKey[31] |= 64

	if publicKey, err = curve25519.X25519(privateKey, curve25519.Basepoint); err != nil {
		return [2]string{}, err
	}
	return [2]string{string(privateKey), string(publicKey)}, nil
}
