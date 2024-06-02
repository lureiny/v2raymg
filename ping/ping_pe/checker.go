package pingpe

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/ping"
	"golang.org/x/net/html"
)

const (
	pingTime  = 100
	lastsTime = 8 // 分钟, 100 * 5 / 60

	pingResultChanSize = 1000
)

type PingPeChecker struct {
	cookie     string
	resultChan chan *ping.PingResult
	cancel     context.CancelFunc
	isRunning  atomic.Bool
}

func NewPingPeChekcer() *PingPeChecker {
	checker := &PingPeChecker{
		resultChan: make(chan *ping.PingResult, pingResultChanSize),
	}
	checker.isRunning.Store(false)
	return checker
}

func (pc *PingPeChecker) Init() error {
	pc.resultChan = make(chan *ping.PingResult, pingResultChanSize)
	if err := pc.updateCookie(); err != nil {
		logger.Error("init ping pe cookie fail > %v", err)
		return err
	}
	return nil
}

func (pc *PingPeChecker) StartPing(host string) error {
	if pc.isRunning.Load() {
		// 正在采集
		return nil
	}
	logger.Debug("start ping %s", host)
	if err := pc.updateCookie(); err != nil {
		logger.Error("init ping pe cookie fail > %v", err)
		return err
	}
	streamId, docString, err := getStreamIdAndDoc(host, pc.cookie)
	if err != nil {
		logger.Error("get stream id fail > %v", err)
		return err
	}
	rootNode, err := html.Parse(strings.NewReader(docString))
	if err != nil {
		logger.Error("parse ping pe html fail, err: %v", err)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*lastsTime)
	// 异步获取结果
	go func() {
		ticker := time.NewTicker(time.Second * 5)
		times := 0
		for range ticker.C {
			if times == pingTime {
				logger.Debug("exit current ping cycle with have ping [%d] times", times)
				return
			}
			select {
			case <-ctx.Done():
				logger.Debug("stop ping test wiht ping.pe")
				pc.isRunning.Store(false)
				return
			default:
				if pingResults, err := getResult(streamId, pc.cookie); err != nil {
					logger.Error("ping host[%s] with ping pe fail, err: %v", host, err)
				} else {
					for _, d := range pingResults.Data {
						if float64(d.Result)/1000 == -1 {
							// 跳过未收到请求的数据
							continue
						}
						geo, isp := getNodeGeoAndISP(rootNode, d.NodeId)
						result := ping.NewPingResult(geo, isp)
						result.Update(float64(d.Result) / 1000)
						pc.resultChan <- result
					}
				}
				times += 1
			}

		}
	}()
	pc.cancel = cancel
	pc.isRunning.Store(true)
	return nil
}

func (pc *PingPeChecker) StopPing() {
	if pc.cancel != nil {
		pc.cancel()
	}
}

func (pc *PingPeChecker) GetChan() <-chan *ping.PingResult {
	return pc.resultChan
}

func (pc *PingPeChecker) IsRunning() bool {
	return pc.isRunning.Load()
}

func (pc *PingPeChecker) updateCookie() error {
	cookie, err := getPingPeCookie()
	if err != nil {
		return fmt.Errorf("update ping pe cooke fail > %v", err)
	}
	pc.cookie = cookie

	return nil
}
