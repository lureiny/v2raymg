package cli

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// config
type config struct {
	Host  string `yaml:"host"`
	Token string `yaml:"token"`
}

var globalConfig = &config{}

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
	fmt.Printf("please input token: ")
	fmt.Scanln(&(globalConfig.Token))
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
