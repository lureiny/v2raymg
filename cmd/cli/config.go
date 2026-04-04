package cli

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/cmd/cli/client"
	"gopkg.in/yaml.v3"
)

// config
type config struct {
	Host     string `yaml:"host"`
	Token    string `yaml:"token"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

var globalConfig = &config{}

// jwtCache holds a cached JWT token obtained via /login.
var jwtCache struct {
	mu     sync.Mutex
	token  string
	expire int64 // Unix timestamp
}

const defaultConfigName = ".v2raymg-tools.yaml"

func LoadConfig(config string) error {
	if config == "" {
		config = defaultConfigName
	}
	data, err := os.ReadFile(config)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err != nil && os.IsNotExist(err) {
		// 等待输入配置
		inputConfig()
	}
	if err == nil {
		if err := yaml.Unmarshal(data, globalConfig); err != nil {
			inputConfig()
		}
	}
	d, err := yaml.Marshal(globalConfig)
	if err != nil {
		return fmt.Errorf("marshal config fail, err: %v", err)
	}
	return os.WriteFile(config, d, 0666)
}

func inputConfig() {
	fmt.Printf("please input host: ")
	fmt.Scanln(&(globalConfig.Host))
	fmt.Printf("please input token (leave empty to use username/password login): ")
	fmt.Scanln(&(globalConfig.Token))
	if globalConfig.Token == "" {
		fmt.Printf("please input username: ")
		fmt.Scanln(&(globalConfig.Username))
		fmt.Printf("please input password: ")
		fmt.Scanln(&(globalConfig.Password))
	}
}

func getHost() string {
	host := strings.TrimSpace(globalConfig.Host)
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	// Let net/url parse and reconstruct to ensure well-formed URL
	u, err := url.Parse(host)
	if err != nil {
		return "http://" + strings.TrimSpace(globalConfig.Host)
	}
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	return u.String()
}
func getToken() string {
	return globalConfig.Token
}

// ClearJWTCache invalidates any cached JWT, forcing a fresh login on the next getAuthToken() call.
// Call this after a successful logout so subsequent commands don't reuse the revoked token.
func ClearJWTCache() {
	jwtCache.mu.Lock()
	jwtCache.token = ""
	jwtCache.expire = 0
	jwtCache.mu.Unlock()
}

// getAuthToken returns the auth token to use for API calls.
// If username and password are configured, it tries JWT login first and returns
// "Bearer <jwt>". Falls back to the static X-Token if login is unavailable or
// credentials are not configured.
func getAuthToken() string {
	if globalConfig.Username == "" || globalConfig.Password == "" {
		return globalConfig.Token
	}

	jwtCache.mu.Lock()
	defer jwtCache.mu.Unlock()

	// Reuse cached token if it expires more than 60 seconds from now.
	if jwtCache.token != "" && time.Now().Unix()+60 < jwtCache.expire {
		return "Bearer " + jwtCache.token
	}

	token, expire, err := client.Login(getHost(), globalConfig.Username, globalConfig.Password)
	if err != nil {
		// Login failed — fall back to static token.
		return globalConfig.Token
	}

	jwtCache.token = token
	jwtCache.expire = expire
	return "Bearer " + token
}
