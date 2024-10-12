package ping

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/client/http"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/common/util"
	"golang.org/x/net/html"
)

const (
	pingResultChanSize = 1000
	pingCycle          = 5 // 秒
)

var pingTime = 100

var once sync.Once = sync.Once{}

func SetPingPeTime(newPingTime int) {
	pingTime = newPingTime
}

type PingPeChecker struct {
	cookie     string
	userAgent  string
	host       string
	rootNode   *html.Node
	streamId   string
	infoInited bool
	*basePingChecker
}

func NewPingPeChekcer() *PingPeChecker {
	checker := &PingPeChecker{
		basePingChecker: newBasePingChecker(),
	}
	return checker
}

func (pc *PingPeChecker) Name() string {
	return "ping_pe"
}

func (pc *PingPeChecker) Init(opts ...OptionFunc) {
	for _, opt := range opts {
		opt(pc)
	}
	randIndex, _ := util.GetRandIntn(int64(len(common.UserAgents)))
	pc.userAgent = common.UserAgents[randIndex]
}

func (pc *PingPeChecker) initInfo() error {
	once.Do(func() {
	})
	if err := pc.updateCookie(); err != nil {
		logger.Error("init ping pe cookie fail > %v", err)
		return err
	}

	streamId, docString, err := getStreamIdAndDoc(pc.host, pc.cookie, pc.userAgent)
	if err != nil {
		logger.Error("get stream id fail > %v", err)
		return err
	}
	pc.streamId = streamId
	pc.rootNode, err = html.Parse(strings.NewReader(docString))
	if err != nil {
		logger.Error("parse ping pe html fail, err: %v", err)
		return err
	}
	pc.infoInited = true
	return nil
}

func (pc *PingPeChecker) Start() {
	if pc.isRunning.Load() {
		// 正在采集
		return
	}
	logger.Debug("start ping %s", pc.host)
	// 异步获取结果
	go func() {
		ticker := time.NewTicker(time.Second * pingCycle)
		times := 0
		for range ticker.C {
			if times == pingTime || !pc.infoInited {
				logger.Debug("exit current ping cycle with have ping [%d] times or not init", times)
				pc.infoInited = false
				pc.initInfo()
				continue
			}
			select {
			case <-pc.ctx.Done():
				logger.Debug("stop ping[%s] wiht ping.pe", pc.host)
				return
			default:
				if pingResults, err := getResult(pc.streamId, pc.cookie, pc.userAgent); err != nil {
					logger.Error("ping host[%s] with ping pe fail, err: %v", pc.host, err)
				} else {
					for _, d := range pingResults.Data {
						if float64(d.Result)/1000 == -1 {
							// 跳过未收到请求的数据
							continue
						}
						geo, isp := getNodeGeoAndISP(pc.rootNode, d.NodeId)
						// if d.Result == -2000 && geo != "" && isp != "" {
						// 	logger.Debug("get loss of geo[%v], isp[%v]", geo, isp)
						// }
						result := NewPingResult(geo, isp, "", pc.Name())
						result.Update(float64(d.Result) / 1000)
						pc.resultChan <- result
					}
				}
				times += 1
			}

		}
	}()
	pc.isRunning.Store(true)
}

func (pc *PingPeChecker) Stop() {
	if pc.cancel != nil {
		pc.cancel()
		pc.isRunning.Store(false)
	}
}

func (pc *PingPeChecker) GetChan() <-chan *PingResult {
	return pc.resultChan
}

func (pc *PingPeChecker) IsRunning() bool {
	return pc.isRunning.Load()
}

func (pc *PingPeChecker) updateCookie() error {
	cookie, err := getPingPeCookie(pc.userAgent)
	if err != nil {
		return fmt.Errorf("update ping pe cooke fail > %v", err)
	}
	pc.cookie = cookie

	return nil
}

var cookieRegex = regexp.MustCompile(`document.cookie="(.*)";`)

func getPingPeCookie(userAgent string) (string, error) {
	headers := map[string]string{
		"User-Agent": userAgent,
	}
	resp, err := http.DoGetRequest(common.PingPeBaseUrl, headers)
	if err != nil {
		return "", fmt.Errorf("get ping pe cookie fail, err: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read ping.pe response fail, err: %v", err)
	}
	matchs := cookieRegex.FindStringSubmatch(string(data))
	if len(matchs) <= 1 {
		return "", fmt.Errorf("can't get cookie")
	}
	return matchs[1], nil
}

var streamIdRegex = regexp.MustCompile(`stream_id = '([0-9]+)'`)

func getStreamIdAndDoc(host, cookie, userAgent string) (string, string, error) {
	headers := map[string]string{
		"User-Agent": userAgent,
		"Cookie":     cookie,
	}
	url := fmt.Sprintf(common.PingPeSubmitUrl, host)
	resp, err := http.DoGetRequest(url, headers)
	if err != nil {
		return "", "", fmt.Errorf("get ping pe stream id fail, err: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read ping.pe response fail, err: %v", err)
	}
	matchs := streamIdRegex.FindStringSubmatch(string(data))
	if len(matchs) <= 1 {
		return "", "", fmt.Errorf("can't get stream id")
	}
	return matchs[1], string(data), nil
}

type pingPeResult struct {
	Data []*pingPeResultData `json:"data"`
}

type pingPeResultData struct {
	NodeId      string `json:"node_id"`
	TimestampMs int64  `json:"timestamp_ms"`
	Result      int64  `json:"result"` // -2000 代表不可达, -1000代表还没有数据, 大于0表示正常探测
	ResultText  string `json:"result_text"`
}

func getResult(streamId, cookie, userAgent string) (*pingPeResult, error) {
	headers := map[string]string{
		"User-Agent": userAgent,
		"Cookie":     cookie,
	}
	url := fmt.Sprintf(common.PingPeGetResultUrl, streamId)
	resp, err := http.DoGetRequest(url, headers)
	if err != nil {
		return nil, fmt.Errorf("get ping pe result fail, err: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ping.pe result fail, err: %v", err)
	}
	result := &pingPeResult{}
	if err := json.Unmarshal(data, result); err != nil {
		return nil, fmt.Errorf("parse ping pe result fail, err: %v", err)
	}
	return result, nil
}

func getNodeGeoAndISP(rootNode *html.Node, id string) (string, string) {
	geo, err := getNodeGeo(rootNode, id)
	if err != nil {
		// logger.Error("get node geo of id[%s] fail, err: %v", id, err)
		return "", ""
	}
	isp, err := getNodeISP(rootNode, id)
	if err != nil {
		logger.Error("get isp of id[%s] fail, err : %v", id, err)
	}
	return geo, isp
}

func getNodeGeo(parser *html.Node, id string) (string, error) {
	node := util.FindNodeByID(parser, fmt.Sprintf("ping-%s-location", id))
	if node == nil {
		return "", fmt.Errorf("can't find node geo by id[%s]", id)
	}
	spans := util.FindTagsByClass(node, "td-location")
	if len(spans) == 0 {
		return "", fmt.Errorf("can't find node location by id[%s]", id)
	}
	return util.ExtractNodeValue(spans[0]), nil
}

func getNodeISP(parser *html.Node, id string) (string, error) {
	node := util.FindNodeByID(parser, fmt.Sprintf("ping-%s-provider", id))
	if node == nil {
		return "", fmt.Errorf("can't find node geo by id[%s]", id)
	}
	spans := util.FindTagsByClass(node, "td-provider")
	if len(spans) == 0 {
		return "", fmt.Errorf("can't find node provider by id[%s]", id)
	}
	return util.ExtractNodeValue(spans[0]), nil
}
