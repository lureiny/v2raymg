package config

import (
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/config"
)

var globalConfigManager *config.ConfigManager = &config.ConfigManager{}

// 获取全局的ConfigManager, 获取后需要初始化
func GetGlobalConfigManager() *config.ConfigManager {
	if globalConfigManager == nil {
		globalConfigManager = &config.ConfigManager{}
	}
	return globalConfigManager
}

func InitGlobalConfig(configFile string) error {
	return globalConfigManager.Init(configFile)
}

// CheckConifg check global config
func CheckConfig() error {
	if globalConfigManager == nil {
		return fmt.Errorf("global config is not init")
	}
	if strings.EqualFold(GetString(common.ConfigRpcServerType), common.CenterNodeType) {
		return nil
	}
	if err := checkProxyConfig(globalConfigManager); err != nil {
		return err
	}

	if err := checkServerConfig(globalConfigManager); err != nil {
		return err
	}
	return nil
}

func checkProxyConfig(cm *config.ConfigManager) error {
	return nil
}

func checkServerConfig(cm *config.ConfigManager) error {
	if cm.GetString(common.ConfigServerHttpToken) == "" {
		return fmt.Errorf("http token can't be empty")
	}
	if cm.GetString(common.ConfigServerName) == "" {
		return fmt.Errorf("server name can't be empty")
	}
	return nil
}

func Set(key string, value interface{}) {
	globalConfigManager.Set(key, value)
}

func GetString(key string) string {
	return globalConfigManager.GetString(key)
}

func GetStringSlice(key string) []string {
	return globalConfigManager.GetStringSlice(key)
}

func UnmarshalKey(key string, rawVal interface{}) error {
	return globalConfigManager.UnmarshalKey(key, rawVal)
}

func GetStringMapString(key string) map[string]string {
	return globalConfigManager.GetStringMapString(key)
}

func GetInt(key string) int {
	return globalConfigManager.GetInt(key)
}

func GetBool(key string) bool {
	return globalConfigManager.GetBool(key)
}

func Flush() {
	globalConfigManager.Flush()
}

// cycle 刷新周期  单位 秒/s
func AutoFlush(cycle int64) {
	globalConfigManager.AutoFlush(cycle)
}
