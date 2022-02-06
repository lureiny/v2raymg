package cmd

import (
	"fmt"

	"github.com/fsnotify/fsnotify"
	"github.com/lureiny/v2raymg/config"
	"github.com/lureiny/v2raymg/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// serverCmd restful api
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start restful api server.",
	Run:   startServer,
}

var serverConfig = ""

func init() {
	serverCmd.Flags().StringVar(&serverConfig, "conf", "/usr/local/etc/v2raymg/config.json", "V2raymg server config file")
}

func startServer(cmd *cobra.Command, args []string) {
	// 读取配置文件
	viper.SetConfigFile(serverConfig)
	err := viper.ReadInConfig()
	if err != nil {
		config.Error.Fatal(err)
	}

	viper.OnConfigChange(func(e fsnotify.Event) {
		config.Info.Printf("Config file changed: %s\n", e.Name)
		server.UpdatetUsers()
	})
	viper.WatchConfig()

	// 初始化参数
	subToken := viper.GetString("server.tokens.sub")
	manageToken := viper.GetString("server.tokens.manage")
	if manageToken == "" {
		manageToken = subToken
	}
	listen := viper.GetString("server.listen")
	configFile = viper.GetString("server.configFile")
	inBoundTag = viper.GetString("server.defaultTag")
	serverPort := viper.GetInt("server.port")
	proxyHost := viper.GetString("proxy.host")
	proxyPort := viper.GetInt("proxy.port")
	server.InitGinServer()
	server.InitSubService(subToken, host, proxyHost, configFile, inBoundTag, port, proxyPort)
	server.InitUserService(manageToken, host, proxyHost, configFile, inBoundTag, port, proxyPort)
	server.InitStatServer(manageToken, host, port)
	server.RestfulServer.Run(fmt.Sprintf("%s:%d", listen, serverPort))
}
