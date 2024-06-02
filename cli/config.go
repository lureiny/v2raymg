package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// config
type config struct {
	Host  string `yaml:"host"`
	Token string `yaml:"token"`
}

var globalConfig = &config{}

const configName = ".v2raymg-tools.yaml"

func loadConfig() error {
	data, err := os.ReadFile(configName)
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
	return os.WriteFile(configName, d, 0666)
}

func inputConfig() {
	fmt.Printf("please input host: ")
	fmt.Scanln(&(globalConfig.Host))
	fmt.Printf("please input token: ")
	fmt.Scanln(&(globalConfig.Token))
}

func getHost() string {
	return globalConfig.Host
}
func getToken() string {
	return globalConfig.Token
}
