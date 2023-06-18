package cmd

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/cluster"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/global"
	"github.com/lureiny/v2raymg/global/config"
	globalLego "github.com/lureiny/v2raymg/global/lego"
	"github.com/lureiny/v2raymg/global/logger"
	"github.com/lureiny/v2raymg/global/proxy"
	"github.com/lureiny/v2raymg/lego"
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

var (
	serverConfig = ""
)

const collectCycle = 30 * time.Second

func init() {
	serverCmd.Flags().StringVar(&serverConfig, "conf", "/usr/local/etc/v2raymg/config.json", "V2raymg server config file")
}

func initGlobalInfo() {
	log.Printf("Start v2raymg which manage %s", manager.FileName)
	if err := global.InitGlobalInfra(serverConfig); err != nil {
		log.Fatal(err)
	}
}

func initAndStartEndNodeServer(globalUserManager *cluster.UserManager, certManager *lego.CertManager) {
	endNodeServer := rpc.GetEndNodeServer()
	endNodeServer.Init(globalUserManager, certManager)
	go endNodeServer.Start()
}

func initAndStartHttpServer(globalUserManager *cluster.UserManager, certManager *lego.CertManager) {
	httpServer := http.GlobalHttpServer
	httpServer.Init(globalUserManager, certManager)
	if config.GetBool(common.ConfigSupportPrometheus) {
		http.RegisterPrometheus()
	}
	go httpServer.Start()
	go collectStats(httpServer)
}

func collectStats(httpServer *http.HttpServer) {
	ticker := time.NewTicker(collectCycle)
	nodes := httpServer.GetTargetNodes(httpServer.Name)
	rpcClient := client.NewEndNodeClient(nodes, nil)
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
	initGlobalInfo()
	// center node
	serverType := config.GetString(common.ConfigRpcServerType)
	if strings.ToLower(serverType) == common.CenterNodeType {
		centerNodeServer := &rpc.CenterNodeServer{}
		centerNodeServer.Init()
		centerNodeServer.Start()
		return
	}
	// end node
	// 1s检查刷新一次
	config.AutoFlush(1)

	globalUserManager := cluster.NewUserManager()
	initAndStartEndNodeServer(globalUserManager, globalLego.GetCertManager())
	initAndStartHttpServer(globalUserManager, globalLego.GetCertManager())

	// listen signal
	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGKILL)
	signal := <-c
	logger.Info("Msg=Exit With signal: %v", signal)
	// TODO: use context
	proxy.StopProxyServer()
}
