package cmd

import (
	"github.com/lureiny/v2raymg/bound"
	"github.com/lureiny/v2raymg/config"
	"github.com/spf13/cobra"
)

// removeCmd represents the remove command
var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a user from inbound.",
	Run:   removeUserLocal,
}

func init() {
	removeCmd.Flags().StringVarP(&email, "email", "e", "", "Email of user.")
	removeCmd.MarkFlagRequired("email")
	removeCmd.Flags().StringVarP(&inBoundTag, "inboundTag", "t", "", "The inbound tag which remove user from.")
	removeCmd.MarkFlagRequired("inboundTag")
	removeCmd.Flags().StringVarP(&configFile, "config", "c", "/usr/local/etc/v2ray/config.json", "The config file of v2ray.")
}

func removeUserLocal(cmd *cobra.Command, args []string) {
	runtimeConfig := &config.RuntimeConfig{
		Host:       host,
		Port:       port,
		ConfigFile: configFile,
	}

	p, err := bound.GetProtocol(inBoundTag, configFile)
	if err != nil {
		config.Error.Fatal(err)
	}

	user, err := bound.NewUser(email, inBoundTag, bound.Protocol(p), bound.UUID(uuid))
	if err != nil {
		config.Error.Fatal(err)
	}

	if err := bound.RemoveUser(runtimeConfig, user); err != nil {
		config.Error.Fatal(err)
	}
}
