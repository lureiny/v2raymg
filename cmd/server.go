package cmd

import (
	"log"
	"strings"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/proxy/manager"
	"github.com/lureiny/v2raymg/server/http"
	"github.com/lureiny/v2raymg/server/rpc"
	"github.com/spf13/cobra"
)

// serverCmd restful api
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start restful api server.",
	Run:   startServer,
}

var serverConfig = ""

var configManager = common.GetGlobalConfigManager()

func init() {
	serverCmd.Flags().StringVar(&serverConfig, "conf", "/usr/local/etc/v2raymg/config.json", "V2raymg server config file")
}

func startServer(cmd *cobra.Command, args []string) {
	// 读取配置文件
	err := configManager.Init(serverConfig)
	if err != nil {
		log.Fatal(err.Error())
	}
	// center node
	serverType := configManager.GetString("server.rpc.type")
	if strings.ToLower(serverType) == "center" {
		centerNodeServer := &rpc.CenterNodeServer{}
		centerNodeServer.Init()
		centerNodeServer.Start()
		return
	}

	// end node
	// 1s检查刷新一次
	configManager.AutoFlush(1)

	proxyManager := manager.GetProxyManager()
	err = proxyManager.Init(configManager.GetString("proxy.config_file"), configManager.GetString("proxy.exec"))
	if err != nil {
		log.Fatal(err)
	}

	rawAdaptive := &manager.RawAdaptive{}
	err = configManager.UnmarshalKey("proxy.adaptive", rawAdaptive)
	if err != nil {
		log.Fatalf("please check adaptive config > %v", err)
	}
	if err := proxyManager.InitAdaptive(rawAdaptive); err != nil {
		log.Fatal(err)
	}
	proxyManager.AutoFlush(1)
	proxyManager.CycleAdaptive()

	globalUserManager := common.NewUserManager()
	globalClusterManager := common.NewEndNodeClusterManager()
	endNodeServer := rpc.GetEndNodeServer()
	endNodeServer.Init(globalUserManager, globalClusterManager)
	go endNodeServer.Start()
	httpServer := &http.HttpServer{}
	httpServer.Init(globalUserManager, globalClusterManager)
	httpServer.Start()
}
