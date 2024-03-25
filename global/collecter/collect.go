package collecter

import (
	"time"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/global/proxy"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

const collectCycle = 30 * time.Second

const statsChSize = 100

var StatsForPrometheus = &common.StatsWithMutex{
	StatsMap: make(map[string]*proto.Stats),
	Ch:       make(chan *proto.Stats, statsChSize),
}
var SumStats = &common.StatsWithMutex{
	StatsMap: make(map[string]*proto.Stats),
	Ch:       make(chan *proto.Stats, statsChSize),
}

func CollectStats() {
	ticker := time.NewTicker(collectCycle)
	source := config.GetString(common.ConfigServerName)
	// 如果一直采集, 存在一定的OOM风险: 长时间本地缓存, stat越来越多, 99.99的情况不会出现
	for range ticker.C {
		collectStats(source)
	}
}

func collectStats(source string) {
	pattern := ""
	reset := true
	stats, err := proxy.QueryStats(pattern, reset)
	if err != nil {
		logger.Error("Err=query stats fail > %v", err)
	}
	for _, stat := range stats {
		stat.Source = source
		StatsForPrometheus.Ch <- stat
		SumStats.Ch <- stat
	}
}

func init() {
	go StatsForPrometheus.Collect(common.CollectCBFunc)
	go SumStats.Collect(common.CollectCBFunc)
}
