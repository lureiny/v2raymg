package cmd

import (
	"github.com/lureiny/v2raymg/bound"
	"github.com/lureiny/v2raymg/config"
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
	runtimeConfig := config.RuntimeConfig{
		Host:       host,
		Port:       port,
		ConfigFile: configFile,
	}

	p, err := bound.GetProtocol(tag, configFile)
	if err != nil {
		return err
	}

	user, err := bound.NewUser(email, tag, bound.Protocol(p))
	if err != nil {
		return err
	}

	if err := bound.RemoveUser(&runtimeConfig, user); err != nil {
		return err
	}

	if err := bound.AddUser(&runtimeConfig, user); err != nil {
		return err
	}
	return nil
}

func reset(cmd *cobra.Command, args []string) {
	Reset(email, inBoundTag)
}
