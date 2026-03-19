package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "v2raymg",
	Short: "v2ray管理程序",
	Long:  `基于命令行的v2ray管理程序，支持启动服务端、管理用户、查询流量信息`,
}

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(cliCmd)
}
