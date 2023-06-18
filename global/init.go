package global

import (
	"fmt"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/global/lego"
	"github.com/lureiny/v2raymg/global/logger"
	"github.com/lureiny/v2raymg/global/proxy"
)

func initConfig(configFile string) error {
	if err := config.InitGlobalConfig(configFile); err != nil {
		return err
	}
	if err := config.CheckConfig(); err != nil {
		return err
	}
	config.AutoFlush(5)
	return nil
}

func initLogger() error {
	serverName := config.GetString(common.ConfigServerName)
	if serverName == "" {
		accessHost := config.GetString(common.ConfigProxyHost)
		port := config.GetInt(common.ConfigServerRpcPort)
		serverName = fmt.Sprintf("%s:%d", accessHost, port)
		config.Set(common.ConfigServerName, serverName)
	}
	serverType := config.GetString(common.ConfigRpcServerType)
	if serverType == "" {
		serverType = common.DefaultNodeType
	}
	logger.SetLogLevel(0)
	logger.SetNodeType(serverType)
	logger.SetServerName(serverName)
	return nil
}

func initCluster() error {
	return cluster.InitCluster()
}

//TODO: init global certManager
func initProxyManager() error {
	if err := proxy.InitProxyManager(config.GetString(common.ConfigProxyConfigFile),
		config.GetString(common.ConfigProxyVersion), lego.GetCertManager()); err != nil {
		return fmt.Errorf("Init proxy manager fail, err: %v", err)
	}
	return nil
}

func initCertManager() error {
	return lego.InitCertManager()
}

func InitGlobalInfra(configFile string) error {
	if err := initConfig(configFile); err != nil {
		return fmt.Errorf("init config fail, err: %v", err)
	}
	if err := initLogger(); err != nil {
		return err
	}
	if err := initCluster(); err != nil {
		return err
	}
	if err := initCertManager(); err != nil {
		return err
	}
	if err := initProxyManager(); err != nil {
		return err
	}
	return nil
}