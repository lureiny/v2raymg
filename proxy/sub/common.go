package sub

import (
	"encoding/base64"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	fakeSSUriSchema = "aes-256-cfb:%s@%s:%d#fake" // password@ip:port
	letterBytes     = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()"

	fakeSubPasswordLen = 10
)

// BaseConfig for vless and trojan
type BaseConfig struct {
	UUID       string // trojan: passwd vless: uuid
	RemoteHost string
	RemotePort uint32
	Flow       string // for xtls
}

// Build ...
func (c *BaseConfig) Build() string {
	return fmt.Sprintf("%s@%s:%d", url.QueryEscape(c.UUID), c.RemoteHost, c.RemotePort)
}

// fixUri 修复生成uri, 例如存在&&的问题
func fixUri(uri string) string {
	return strings.ReplaceAll(uri, "&&", "&")
}

func parseHost(configHost, sni string) string {
	if ip := net.ParseIP(configHost); sni == "" || ip != nil {
		return configHost
	}
	// 解析域名
	ips, err := net.LookupHost(configHost)
	if err != nil {
		return configHost
	}
	return ips[0]
}

// generateRandomIP 随机生成IPv4地址
func generateRandomIP() net.IP {
	rand.Seed(time.Now().UnixNano())
	ip := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		ip[i] = byte(rand.Intn(256))
	}
	return ip
}

// generateRandomPort 随机生成端口号
func generateRandomPort() int {
	rand.Seed(time.Now().UnixNano())
	// 生成一个1024到65535之间的端口号
	return rand.Intn(65535-1024) + 1024
}

// generateRandomString 随机生成指定长度的字符串
func generateRandomString(n int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

// GenerateFakeSSSub ...
func GenerateFakeSSSub() string {
	ip := generateRandomIP()
	port := generateRandomPort()
	password := generateRandomString(fakeSubPasswordLen)
	rawFakeSubUri := fmt.Sprintf(fakeSSUriSchema, password, ip.String(), port)
	ssUri := fmt.Sprintf("ss://%s", base64.RawURLEncoding.EncodeToString([]byte(rawFakeSubUri)))
	return base64.RawURLEncoding.EncodeToString([]byte(ssUri))
}
