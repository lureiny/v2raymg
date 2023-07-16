package cmd

import (
	"github.com/lureiny/v2raymg/proxy/manager"
	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "reset user's uuid",
	Run:   reset,
}

func init() {
	resetCmd.Flags().StringVarP(&email, "email", "e", "", "Email of user.")
	resetCmd.MarkFlagRequired("email")
	resetCmd.Flags().StringVarP(&inBoundTag, "inboundTag", "t", "", "The inbound tag which adds user to.")
	resetCmd.MarkFlagRequired("inboundTag")

	resetCmd.Flags().StringVarP(&configFile, "config", "c", "/usr/local/etc/v2ray/config.json", "The config file of v2ray.")
}

// Reset reset user uuid
func Reset(email string, tag string) error {
	proxyManager := manager.NewProxyManager()

	proxyManager.Init(configFile, "", nil)

	user, err := manager.NewUser(email, tag)
	if err != nil {
		return err
	}

	return proxyManager.ResetUser(user)
}

func reset(cmd *cobra.Command, args []string) {
	Reset(email, inBoundTag)
}
