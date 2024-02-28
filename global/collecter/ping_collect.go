package collecter

import (
	"fmt"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/ping"
	pingpe "github.com/lureiny/v2raymg/ping/ping_pe"
)

var (
	pingChecker ping.PingChecker = pingpe.NewPingPeChekcer()

	pingResults      map[string]*ping.PingResult = make(map[string]*ping.PingResult) // key: node name({Geo}_{ISP})
	pingResultsMutex                             = sync.RWMutex{}
)

const checkerCycle = 1 // 秒

func StartPing() {
	logger.Debug("start ping collector")

	go func() {
		ch := pingChecker.GetChan()
		for result := range ch {
			nodeName := fmt.Sprintf("%s_%s", result.Geo, result.ISP)
			pingResultsMutex.Lock()
			if pingResult, ok := pingResults[nodeName]; !ok {
				pingResults[nodeName] = result
			} else {
				pingResult.Update(result.GetLatestDelay())
			}
			pingResultsMutex.Unlock()
		}
	}()

	ticker := time.NewTicker(time.Second * checkerCycle)
	host := config.GetString(common.ConfigProxyHost)

	for range ticker.C {
		if !pingChecker.IsRunning() {
			pingChecker.StartPing(host)
		}
	}
}

func GetPingResult() map[string]*ping.PingResult {
	pingResultsMutex.RLock()
	defer pingResultsMutex.RUnlock()
	results := make(map[string]*ping.PingResult)
	for k, v := range pingResults {
		results[k] = v.Copy()
	}
	return results
}
