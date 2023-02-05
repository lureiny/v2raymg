package cmd

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/proxy/manager"
	"github.com/lureiny/v2raymg/server/http"
	"github.com/lureiny/v2raymg/server/rpc"
	"github.com/lureiny/v2raymg/server/rpc/proto"
	"github.com/spf13/cobra"
)

// serverCmd restful api
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start restful api server.",
	Run:   startServer,
}

var logger = common.LoggerImp

var serverConfig = ""

var configManager = common.GetGlobalConfigManager()

const collectCycle = 30 * time.Second

func init() {
	serverCmd.Flags().StringVar(&serverConfig, "conf", "/usr/local/etc/v2raymg/config.json", "V2raymg server config file")
}

func readConfig() {
	log.Printf("Start v2raymg which manage %s", manager.FileName)
	// 读取配置文件
	if err := configManager.Init(serverConfig); err != nil {
		log.Fatalf("init global config fail: %v", err)
	}
	if err := common.CheckConfig(configManager); err != nil {
		log.Fatalf("global config has something wrong: %v", err)
	}
	log.Printf("read config from: %s \n", serverConfig)

}

func initProxyManager(proxyManager *manager.ProxyManager) {
	err := proxyManager.Init(configManager.GetString(common.ProxyConfigFile), configManager.GetString(common.ProxyVersion))
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
}

func initAndStartEndNodeServer(globalUserManager *common.UserManager, globalClusterManager *common.EndNodeClusterManager) {
	endNodeServer := rpc.GetEndNodeServer()
	endNodeServer.Init(globalUserManager, globalClusterManager)
	go endNodeServer.Start()
}

func initAndStartHttpServer(globalUserManager *common.UserManager, globalClusterManager *common.EndNodeClusterManager) {
	httpServer := http.GlobalHttpServer
	httpServer.Init(globalUserManager, globalClusterManager)
	if configManager.GetBool(common.SupportPrometheus) {
		http.RegisterPrometheus()
	}
	go httpServer.Start()
	go collectStats(httpServer)
}

func collectStats(httpServer *http.HttpServer) {
	ticker := time.NewTicker(collectCycle)
	nodes := httpServer.GetTargetNodes(httpServer.Name)
	rpcClient := client.NewEndNodeClient(nodes, common.GlobalLocalNode)
	req := &proto.GetBandwidthStatsReq{
		Pattern: "",
		Reset_:  true,
	}
	for range ticker.C {
		succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
			client.GetBandWidthStatsReqType,
			req,
		)
		if len(succList) == 0 {
			logger.Error("Err=Get local node stat error > %s", failedList[httpServer.Name])
			continue
		}
		stats := succList[httpServer.Name].([]*proto.Stats)
		for _, stat := range stats {
			common.StatsForPrometheus.Ch <- stat
			common.SumStats.Ch <- stat
		}
	}
}

func startServer(cmd *cobra.Command, args []string) {
	readConfig()
	// center node
	serverType := configManager.GetString(common.RpcServerType)
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
	initProxyManager(proxyManager)

	globalUserManager := common.NewUserManager()
	globalClusterManager := common.NewEndNodeClusterManager()
	initAndStartEndNodeServer(globalUserManager, globalClusterManager)
	initAndStartHttpServer(globalUserManager, globalClusterManager)

	// listen signal
	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGKILL)
	signal := <-c
	common.LoggerImp.Info("Msg=Exit With signal: %v", signal)
	proxyManager.StopProxyServer()
}
