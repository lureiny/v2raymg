package user

import (
	"github.com/lureiny/v2raymg/cluster"
	"github.com/lureiny/v2raymg/global/proxy"
)

var globalUserManager *cluster.UserManager = cluster.NewUserManager()

// InitUserManager ...
func InitUserManager() error {
	globalUserManager.Init(proxy.GetProxyManager())
	return nil
}

// GetUserManager ...
func GetUserManager() *cluster.UserManager {
	return globalUserManager
}

// TODO:配置全局user manager, 实现解耦
