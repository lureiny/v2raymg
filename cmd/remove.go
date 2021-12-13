package cmd

import (
	"log"

	"github.com/lureiny/v2raymg/proxy/manager"
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
	proxyManager := manager.GetProxyManager()
	err := proxyManager.Init(configFile, "")
	if err != nil {
		log.Fatal("Failed to add user > %v", err)
	}

	user, err := manager.NewUser(email, inBoundTag, manager.UUID(uuid))
	if err != nil {
		log.Fatal(err.Error())
	}

	if err := proxyManager.RemoveUser(user); err != nil {
		log.Fatal(err.Error())
	}
}
