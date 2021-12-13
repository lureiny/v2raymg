//go:build v2ray

package stats

import (
	"context"
	"fmt"
	"regexp"

	"github.com/lureiny/v2raymg/server/rpc/proto"
	"github.com/v2fly/v2ray-core/v5/app/stats/command"
	"google.golang.org/grpc"
)

var regexCompile = regexp.MustCompile(`(user|inbound|outbound)>>>(\S+)>>>traffic>>>(downlink|uplink)`)

func queryStats(con command.StatsServiceClient, req *command.QueryStatsRequest) (*command.QueryStatsResponse, error) {
	resp, err := con.QueryStats(context.Background(), req)
	if err != nil {
		return nil, err
	} else {
		return resp, nil
	}
}

func parseQueryStats(resp *command.QueryStatsResponse) (*map[string]*proto.Stats, error) {
	result := make(map[string]*proto.Stats)
	for _, stat := range resp.GetStat() {
		reResult := regexCompile.FindStringSubmatch(stat.GetName())
		if _, ok := result[reResult[2]]; !ok {
			result[reResult[2]] = &proto.Stats{
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

func QueryStats(host, pattern string, port int, reset bool) (*map[string]*proto.Stats, error) {
	// 创建grpc client
	cmdConn, err := grpc.Dial(fmt.Sprintf("%s:%d", host, port), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	statClient := command.NewStatsServiceClient(cmdConn)
	// query 参数
	queryStatsReq := command.QueryStatsRequest{
		Pattern: pattern,
		Reset_:  reset,
	}
	resp, err := queryStats(statClient, &queryStatsReq)
	if err != nil {
		return nil, err
	}
	return parseQueryStats(resp)
}
