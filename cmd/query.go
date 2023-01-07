package cmd

import (
	"fmt"
	"log"
	"strings"

	"github.com/lureiny/v2raymg/proxy/manager"
	"github.com/spf13/cobra"
)

const (
	k = 1024
	m = 1024 * 1024
	g = 1024 * 1024 * 1024
)

// queryCmd represents the query command
var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query user's stats",
	Run:   queryStats,
}

func init() {
	queryCmd.Flags().StringVarP(&unit, "unit", "u", "m", "Unit of stats. K/k or M/m or G/g")
	queryCmd.Flags().StringVarP(&pattern, "pattern", "p", "", "user name/bound tag which to query")
}

func queryStats(cmd *cobra.Command, args []string) {
	unitBase := m
	unitSign := "M"

	switch strings.ToLower(unit) {
	case "k":
		unitBase = k
		unitSign = "K"
	case "g":
		unitBase = g
		unitSign = "G"
	}
	statsResult, err := manager.QueryStats(pattern, host, port, false)

	if err != nil {
		log.Fatal(err)
	}

	if len(*statsResult) > 0 {
		fmt.Printf("%20s%21s%21s\n", "User", "Downlink", "Uplink")
		for key, value := range *statsResult {
			if value.Type != "user" {
				continue
			}
			fmt.Printf("%20[2]s%20[3]d%[1]s%20[4]d%[1]s\n", unitSign, key, value.Downlink/int64(unitBase), value.Uplink/int64(unitBase))
		}
	}
}
