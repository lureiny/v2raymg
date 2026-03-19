package subscription

import (
	"encoding/base64"
	"fmt"
	"math/rand"
	"net"
	"time"
)

const (
	fakeSSUriSchema    = "aes-256-cfb:%s@%s:%d#fake"
	fakeLetterBytes    = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()"
	fakeSubPasswordLen = 10
)

// GenerateFakeSSSub generates a fake Shadowsocks subscription URI for template fetching
// (e.g. used to pull a Clash config skeleton from external sub-converter services).
func GenerateFakeSSSub() string {
	ip := generateRandomIP()
	port := generateRandomPort()
	password := generateRandomString(fakeSubPasswordLen)
	rawFakeSubUri := fmt.Sprintf(fakeSSUriSchema, password, ip.String(), port)
	ssUri := fmt.Sprintf("ss://%s", base64.RawURLEncoding.EncodeToString([]byte(rawFakeSubUri)))
	return base64.RawURLEncoding.EncodeToString([]byte(ssUri))
}

func generateRandomIP() net.IP {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	ip := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		ip[i] = byte(r.Intn(256))
	}
	return ip
}

func generateRandomPort() int {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return r.Intn(65535-1024) + 1024
}

func generateRandomString(n int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, n)
	for i := range b {
		b[i] = fakeLetterBytes[r.Intn(len(fakeLetterBytes))]
	}
	return string(b)
}
