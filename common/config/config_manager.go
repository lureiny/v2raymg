package config

import (
	"sync"
	"time"

	"github.com/spf13/viper"
)

// 管理进程本身配置
type ConfigManager struct {
	v         *viper.Viper
	lock      sync.RWMutex
	needFlush bool
	isInit    bool
}

// 返回初始化后的ConfigManager实例
func NewConfigManager(configFile string) *ConfigManager {
	cm := &ConfigManager{}
	cm.isInit = false
	cm.Init(configFile)
	return cm
}

// Init...
func (cm *ConfigManager) Init(configFile string) error {
	cm.needFlush = false
	cm.v = viper.GetViper()
	cm.v.SetConfigFile(configFile)
	return cm.v.ReadInConfig()

}

func (cm *ConfigManager) Set(key string, value interface{}) {
	cm.lock.Lock()
	defer cm.lock.Unlock()
	cm.v.Set(key, value)
	cm.needFlush = true
}

func (cm *ConfigManager) GetString(key string) string {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	return cm.v.GetString(key)
}

func (cm *ConfigManager) GetStringSlice(key string) []string {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	return cm.v.GetStringSlice(key)
}

func (cm *ConfigManager) UnmarshalKey(key string, rawVal interface{}) error {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	return cm.v.UnmarshalKey(key, rawVal)
}

func (cm *ConfigManager) GetStringMapString(key string) map[string]string {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	return cm.v.GetStringMapString(key)
}

func (cm *ConfigManager) GetInt(key string) int {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	v := cm.v.GetInt(key)
	return v
}

func (cm *ConfigManager) GetBool(key string) bool {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	return cm.v.GetBool(key)
}

func (cm *ConfigManager) Flush() {
	cm.lock.RLock()
	defer cm.lock.RUnlock()
	cm.v.WriteConfig()
}

// cycle 刷新周期  单位 秒/s
func (cm *ConfigManager) AutoFlush(cycle int64) {
	go func() {
		timeTicker := time.NewTicker(time.Second * time.Duration(cycle))
		for {
			<-timeTicker.C
			if cm.needFlush {
				cm.Flush()
			}
		}
	}()
}
