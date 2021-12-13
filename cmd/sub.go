package cmd

import (
	"log"

	"github.com/lureiny/v2raymg/proxy/sub"
	"github.com/spf13/cobra"
)

// subCmd represents the sub command
var subCmd = &cobra.Command{
	Use:   "sub",
	Short: "Get some user's sub uri",
	Run:   getSubURI,
}

func init() {
	subCmd.Flags().StringVarP(&email, "email", "e", "", "Email of user.")
	subCmd.Flags().StringVarP(&tag, "tag", "t", "", "Tag of inbound, can't be empty")
	subCmd.Flags().StringVarP(&nodeName, "node_name", "n", tag, "Node name as alias")
	subCmd.MarkFlagRequired("email")
	subCmd.MarkFlagRequired("tag")
	subCmd.MarkFlagRequired("node_name")
	subCmd.Flags().StringVarP(&configFile, "config", "c", "/usr/local/etc/v2ray/config.json", "The config file of v2ray.")

}

func getSubURI(cmd *cobra.Command, args []string) {
	uri, err := sub.GetUserSubUri(email, tag, host, nodeName, uint32(port))
	if err != nil {
		log.Fatal(err)
	}
	log.Println(uri)
}
