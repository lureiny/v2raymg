package cmd

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/global"
	"github.com/lureiny/v2raymg/global/collecter"
	"github.com/lureiny/v2raymg/global/config"
	globalLego "github.com/lureiny/v2raymg/global/lego"
	"github.com/lureiny/v2raymg/global/proxy"
	"github.com/lureiny/v2raymg/lego"
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

var (
	serverConfig = ""
)

func init() {
	serverCmd.Flags().StringVar(&serverConfig, "conf", "/usr/local/etc/v2raymg/config.yaml", "V2raymg server config file")
}

func initGlobalInfo() {
	// log.Printf("Start v2raymg which manage %s", manager.FileName)
	if err := global.InitGlobalInfra(serverConfig); err != nil {
		log.Fatal(err)
	}
}

func initAndStartEndNodeServer(certManager *lego.CertManager) {
	endNodeServer := rpc.GetEndNodeServer()
	endNodeServer.Init(certManager)
	go endNodeServer.Start()
}

func initAndStartHttpServer(certManager *lego.CertManager) {
	httpServer := http.GlobalHttpServer
	httpServer.Init(certManager)
	if config.GetBool(common.ConfigSupportPrometheus) {
		http.RegisterPrometheus()
	}
	go httpServer.Start()
}

func startCollector() {
	go collecter.CollectStats()
	go collecter.StartPing()
}

func startServer(cmd *cobra.Command, args []string) {
	initGlobalInfo()
	// center node
	if strings.EqualFold(config.GetString(common.ConfigRpcServerType), common.CenterNodeType) {
		centerNodeServer := &rpc.CenterNodeServer{}
		centerNodeServer.Init()
		centerNodeServer.Start()
		return
	}
	// end node
	// 1s检查刷新一次
	config.AutoFlush(1)

	initAndStartEndNodeServer(globalLego.GetCertManager())
	initAndStartHttpServer(globalLego.GetCertManager())

	startCollector()

	// listen signal
	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGKILL)
	signal := <-c
	logger.Info("Msg=Exit With signal: %v", signal)
	// TODO: use context
	proxy.StopProxyServer()
}
