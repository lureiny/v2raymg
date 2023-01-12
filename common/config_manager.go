package common

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/spf13/viper"
)

var globalConfigManager *ConfigManager = nil

const (
	RpcServerType        = "server.rpc.type"
	ProxyConfigFile      = "proxy.config_file"
	ProxyExec            = "proxy.exec"
	ProxyDefaultTags     = "proxy.default_tags"
	Users                = "users"
	ProxyHost            = "proxy.host"
	ProxyPort            = "proxy.port"
	ServerListen         = "server.listen"
	ServerHttpPort       = "server.http.port"
	ServerHttpToken      = "server.http.token"
	ServerName           = "server.name"
	SupportPrometheus    = "server.http.support_prometheus"
	ServerRpcPort        = "server.rpc.port"
	ServerRpcOnlyGateway = "server.rpc.only_gateway"
	ClusterName          = "cluster.name"
	ClusterToken         = "cluster.token"
	CenterNodeHost       = "cluster.center_node.host"
	CenterNodePort       = "cluster.center_node.port"
	ClusterNodes         = "cluster.nodes"
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

// CheckConifg check global config
func CheckConfig(cm *ConfigManager) error {
	if cm == nil {
		return fmt.Errorf("global config is not init")
	}
	if err := checkClusterConfig(cm); err != nil {
		return err
	}

	if err := checkProxyConfig(cm); err != nil {
		return err
	}

	if err := checkServerConfig(cm); err != nil {
		return err
	}
	return nil
}

func checkClusterConfig(cm *ConfigManager) error {
	return checkStaticNodes(cm)
}

func checkStaticNodes(cm *ConfigManager) error {
	nodeList := []staticNode{}
	if err := cm.UnmarshalKey(ClusterNodes, &nodeList); err != nil {
		return err
	}
	nodeMap := make(map[string]bool)
	for _, n := range nodeList {
		if _, ok := nodeMap[n.Name]; ok {
			return fmt.Errorf("node name[%s] repeat", n.Name)
		}
		nodeMap[n.Name] = true
	}
	return nil
}

func checkProxyConfig(cm *ConfigManager) error {
	if err := checkProxyConfigFile(cm); err != nil {
		return err
	}
	return nil
}

func checkProxyConfigFile(cm *ConfigManager) error {
	fileName := cm.GetString(ProxyConfigFile)
	_, err := os.Stat(fileName)
	return fmt.Errorf("check proxy config fail: %v", err)
}

func checkServerConfig(cm *ConfigManager) error {
	if cm.GetString(ServerHttpToken) == "" {
		return fmt.Errorf("http token can't be empty")
	}
	if cm.GetString(ServerName) == "" {
		return fmt.Errorf("server name can't be empty")
	}
	return nil
}

// 获取全局的ConfigManager, 获取后需要初始化
func GetGlobalConfigManager() *ConfigManager {
	if globalConfigManager == nil {
		globalConfigManager = &ConfigManager{}
	}
	return globalConfigManager
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
