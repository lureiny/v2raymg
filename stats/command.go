package stats

import (
	"context"
	"fmt"
	"regexp"

	"github.com/v2fly/v2ray-core/v4/app/stats/command"
	"google.golang.org/grpc"
)

// MyStat 集成了用户uplink和downlink的
type MyStat struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Downlink int64  `json:"downlink"`
	Uplink   int64  `json:"uplink"`
}

var regexCompile = regexp.MustCompile(`(user|inbound|outbound)>>>(\S+)>>>traffic>>>(downlink|uplink)`)

func queryStats(con command.StatsServiceClient, req *command.QueryStatsRequest) (*command.QueryStatsResponse, error) {
	resp, err := con.QueryStats(context.Background(), req)
	if err != nil {
		return nil, err
	} else {
		return resp, nil
	}
}

func parseQueryStats(resp *command.QueryStatsResponse) (*map[string]*MyStat, error) {
	result := make(map[string]*MyStat)
	for _, stat := range resp.GetStat() {
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

// QueryAllStats 查询全部用户流量信息
func QueryAllStats(host string, port int) (*map[string]*MyStat, error) {
	// 创建grpc client
	cmdConn, err := grpc.Dial(fmt.Sprintf("%s:%d", host, port), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	statClient := command.NewStatsServiceClient(cmdConn)

	// query 参数
	queryStatsReq := command.QueryStatsRequest{
		Pattern: "",
		Reset_:  false,
	}
	resp, err := queryStats(statClient, &queryStatsReq)
	if err != nil {
		return nil, err
	}
	return parseQueryStats(resp)

}

func QueryUserStat(host string, port int, user string) (*map[string]*MyStat, error) {
	// 创建grpc client
	cmdConn, err := grpc.Dial(fmt.Sprintf("%s:%d", host, port), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	statClient := command.NewStatsServiceClient(cmdConn)
	// query 参数
	queryStatsReq := command.QueryStatsRequest{
		Pattern: user,
		Reset_:  false,
	}
	resp, err := queryStats(statClient, &queryStatsReq)
	if err != nil {
		return nil, err
	}
	return parseQueryStats(resp)
}
