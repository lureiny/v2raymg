package cmd

import (
	clipkg "github.com/lureiny/v2raymg/cmd/cli"
	"github.com/spf13/cobra"
)

var cliCmd = &cobra.Command{
	Use:   "cli",
	Short: "cli tool to manage v2raymg cluster",
	Run:   cliFunc,
}

var cliConfig = ""

func init() {
	cliCmd.Flags().StringVar(&cliConfig, "conf", "", "cli config file")
}

func cliFunc(cmd *cobra.Command, args []string) {
	if err := clipkg.LoadConfig(cliConfig); err != nil {
		panic(err)
	}
	prompt := clipkg.InitPromptAndRegister()
	if err := prompt.Run(); err != nil {
		panic(err)
	}
}
