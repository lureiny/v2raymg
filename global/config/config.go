package config

import (
	"fmt"
	"os"

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
	if err := checkProxyConfig(globalConfigManager); err != nil {
		return err
	}

	if err := checkServerConfig(globalConfigManager); err != nil {
		return err
	}
	return nil
}

// func checkStaticNodes(cm *ConfigManager) error {
// 	nodeList := []staticNode{}
// 	if err := cm.UnmarshalKey(ClusterNodes, &nodeList); err != nil {
// 		return err
// 	}
// 	nodeMap := make(map[string]bool)
// 	for _, n := range nodeList {
// 		if _, ok := nodeMap[n.Name]; ok {
// 			return fmt.Errorf("node name[%s] repeat", n.Name)
// 		}
// 		nodeMap[n.Name] = true
// 	}
// 	return nil
// }

func checkProxyConfig(cm *config.ConfigManager) error {
	if err := checkProxyConfigFile(cm); err != nil {
		return err
	}
	return nil
}

func checkProxyConfigFile(cm *config.ConfigManager) error {
	fileName := cm.GetString(common.ConfigProxyConfigFile)
	if _, err := os.Stat(fileName); err != nil {
		return fmt.Errorf("check proxy config fail: %v", err)
	}
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
