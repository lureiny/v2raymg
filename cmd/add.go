package cmd

import (
	"github.com/lureiny/v2raymg/bound"
	"github.com/lureiny/v2raymg/config"

	"github.com/spf13/cobra"
)

// addCmd represents the add command
var (
	addCmd = &cobra.Command{
		Use:   "add",
		Short: "Add user to v2ray",
		Long:  ``,
		Run:   addUserLocal,
	}
)

func init() {
	// Required flags
	addCmd.Flags().StringVarP(&email, "email", "e", "", "Email of user.")
	addCmd.MarkFlagRequired("email")
	addCmd.Flags().StringVarP(&inBoundTag, "inboundTag", "t", "", "The inbound tag which adds user to.")
	addCmd.MarkFlagRequired("inboundTag")

	// Not necessary flags
	addCmd.Flags().StringVarP(&uuid, "uuid", "u", "", "UUID of vless or vmess.")
	addCmd.Flags().IntVarP(&alterID, "alterID", "a", 0, "The alter id of user.")
	addCmd.Flags().IntVarP(&level, "level", "l", 0, "The level of user.")
	addCmd.Flags().StringVarP(&configFile, "config", "c", "/usr/local/etc/v2ray/config.json", "The config file of v2ray.")
}

func addUserLocal(cmd *cobra.Command, args []string) {
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

	err = bound.AddUser(runtimeConfig, user)
	if err != nil {
		config.Error.Fatalf("Failed to add user > %v", err)
	}
}
