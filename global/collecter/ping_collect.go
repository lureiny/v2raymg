package collecter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/common/util"
	"github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/ping"
)

type pingResults map[string]*ping.PingResult // key: node name({Geo}_{ISP}_{HOST/IP})

var (
	pingCheckers = map[string]ping.PingChecker{}

	pingResultMap = make(map[string]pingResults) // key: ping checker name

	pingMutex = sync.RWMutex{}

	pingNodeManager = ping.NewPingNodeManager()
)

const checkerCycle = 1 // 秒

func startOnePingCheker(pingChecker ping.PingChecker) {
	pingChecker.Init(
		ping.WithHost(config.GetString(common.ConfigProxyHost)),
		ping.WithGetNodeFunc(func() []*ping.PingNodeInfo {
			return pingNodeManager.ListNode()
		}),
	)
	ctx, _ := context.WithCancel(pingChecker.GetContext())
	//  collect ping checker result chan
	go func(ctx context.Context, ch <-chan *ping.PingResult, pingCheckerName string) {
		for {
			select {
			case <-ctx.Done():
				logger.Info("exit collect ping checker[%s] ping result", pingCheckerName)
				return
			case result := <-ch:
				nodeName := fmt.Sprintf("%s_%s_%s", result.Geo, result.ISP, result.PingIp)
				op := func() error {
					if pingResults, ok := pingResultMap[pingCheckerName]; !ok {
						pingResults = make(map[string]*ping.PingResult)
						pingResults[nodeName] = result
						pingResultMap[pingCheckerName] = pingResults
					} else {
						if pingResult, ok := pingResults[nodeName]; !ok {
							pingResults[nodeName] = result
						} else {
							pingResult.Update(result.GetLatestDelay())
						}
					}
					return nil
				}
				util.OpWithWlock(&pingMutex, op)

			}
		}
	}(ctx, pingChecker.GetChan(), pingChecker.Name())

	pingChecker.Start()
}

func StartPing() {
	logger.Debug("start ping collector")
	initNodes()
	if config.GetBool(common.ConfigServerPingPeCheck) {
		pingChecker := ping.NewPingPeChekcer()
		pingCheckers[pingChecker.Name()] = pingChecker
	}

	if config.GetBool(common.ConfigServerICMPPingCheck) {
		pingChecker := ping.NewIcmpPingChecker()
		pingCheckers[pingChecker.Name()] = pingChecker
	}

	for _, pingChecker := range pingCheckers {
		startOnePingCheker(pingChecker)
	}

	ticker := time.NewTicker(time.Second * checkerCycle)

	for range ticker.C {
		op := func() error {
			for _, pingChecker := range pingCheckers {
				if !pingChecker.IsRunning() && len(pingResultMap[pingChecker.Name()]) > 0 {
					// clear result
					pingResultMap[pingChecker.Name()] = make(pingResults)
				}
			}
			return nil
		}
		util.OpWithWlock(&pingMutex, op)
	}
}

func StopPing() {
	op := func() error {
		for _, pingChecker := range pingCheckers {
			pingChecker.Stop()
		}
		return nil
	}
	util.OpWithRlock(&pingMutex, op)
}

func GetPingResult() (results map[string]*ping.PingResult) {
	results = make(map[string]*ping.PingResult)
	op := func() error {
		for _, pingResults := range pingResultMap {
			for k, v := range pingResults {
				results[k] = v.Copy()
			}
		}
		return nil
	}
	util.OpWithRlock(&pingMutex, op)
	return
}

func initNodes() {
	allNodes := []*ping.PingNodeInfo{}
	allNodes = append(allNodes, telecomPingNodes...)
	allNodes = append(allNodes, mobilePingNodes...)
	allNodes = append(allNodes, unicomPingNodes...)
	for _, node := range allNodes {
		if err := pingNodeManager.AddNode(node); err != nil {
			logger.Error("add node[%v] fail > err: %v", node, err)
		}
	}
}
