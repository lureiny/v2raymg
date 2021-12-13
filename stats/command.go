package stats

import (
	"context"
	"regexp"

	"github.com/v2fly/v2ray-core/v4/app/stats/command"
)

// MyStat 集成了用户uplink和downlink的
type MyStat struct {
	Name     string
	Type     string
	Downlink int64
	Uplink   int64
}

var regexCompile = regexp.MustCompile(`(user|inbound|outbound)>>>(\S+)>>>traffic>>>(downlink|uplink)`)

func QueryStats(con command.StatsServiceClient, req *command.QueryStatsRequest) (*command.QueryStatsResponse, error) {
	resp, err := con.QueryStats(context.Background(), req)
	if err != nil {
		return nil, err
	} else {
		return resp, nil
	}
}

func QueryAllStats(con command.StatsServiceClient, req *command.QueryStatsRequest) (*map[string]*MyStat, error) {
	stats, err := QueryStats(con, req)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*MyStat)
	for _, stat := range stats.GetStat() {
		reResult := regexCompile.FindStringSubmatch(stat.GetName())
		if _, ok := result[reResult[2]]; !ok {
			result[reResult[2]] = &MyStat{
				Name: reResult[2],
				Type: reResult[1],
			}
		}
		// 填充数据流量
		switch reResult[3] {
		case "downlink":
			result[reResult[2]].Downlink = stat.GetValue()
		case "uplink":
			result[reResult[2]].Uplink = stat.GetValue()
		}
	}
	return &result, nil
}
