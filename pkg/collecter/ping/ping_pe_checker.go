package ping

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	"golang.org/x/net/html"
)

const (
	pingResultChanSize = 1000
	pingCycle          = 5 // 秒

	pingPeBaseUrl      = "https://ping.pe"
	pingPeGetResultUrl = pingPeBaseUrl + "/ajax_getPingResults_v2.php?stream_id=%s"
	pingPeSubmitUrl    = pingPeBaseUrl + "/%s"
)

// userAgents is a pool of user agent strings used to avoid bot detection.
var userAgents = []string{
	"Mozilla/5.0 (compatible; MSIE 7.0; Windows 98; Trident/3.0)",
	"Mozilla/5.0 (compatible; MSIE 9.0; Windows NT 10.0; Trident/3.0)",
	"Mozilla/5.0 (compatible; MSIE 8.0; Windows NT 6.0; Trident/3.0)",
	"Mozilla/5.0 (compatible; MSIE 7.0; Windows NT 6.0; Trident/4.1)",
	"Mozilla/5.0 (Windows NT 4.0) AppleWebKit/532.0 (KHTML, like Gecko) Chrome/43.0.837.0 Safari/532.0",
	"Mozilla/5.0 (Windows 98) AppleWebKit/532.2 (KHTML, like Gecko) Chrome/41.0.833.0 Safari/532.2",
}

var pingTime = 100
var once sync.Once

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
	return &PingPeChecker{
		basePingChecker: newBasePingChecker(),
	}
}

func (pc *PingPeChecker) Name() string {
	return "ping_pe"
}

func (pc *PingPeChecker) Init(opts ...OptionFunc) {
	for _, opt := range opts {
		opt(pc)
	}
	idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(userAgents))))
	pc.userAgent = userAgents[idx.Int64()]
}

func (pc *PingPeChecker) initInfo() error {
	once.Do(func() {})
	if err := pc.updateCookie(); err != nil {
		log.Error("init ping pe cookie failed", "err", err)
		return err
	}
	streamId, docString, err := getStreamIdAndDoc(pc.host, pc.cookie, pc.userAgent)
	if err != nil {
		log.Error("get stream id failed", "err", err)
		return err
	}
	pc.streamId = streamId
	pc.rootNode, err = html.Parse(strings.NewReader(docString))
	if err != nil {
		log.Error("parse ping pe html failed", "err", err)
		return err
	}
	pc.infoInited = true
	return nil
}

func (pc *PingPeChecker) Start() {
	if pc.isRunning.Load() {
		return
	}
	log.Debug("start ping", "host", pc.host)
	go func() {
		ticker := time.NewTicker(time.Second * pingCycle)
		times := 0
		for range ticker.C {
			if times == pingTime || !pc.infoInited {
				log.Debug("reinitializing ping pe", "times", times, "inited", pc.infoInited)
				pc.infoInited = false
				pc.initInfo()
				continue
			}
			select {
			case <-pc.ctx.Done():
				log.Debug("stop ping.pe", "host", pc.host)
				return
			default:
				if pingResults, err := getResult(pc.streamId, pc.cookie, pc.userAgent); err != nil {
					log.Error("ping failed", "host", pc.host, "err", err)
				} else {
					for _, d := range pingResults.Data {
						if float64(d.Result)/1000 == -1 {
							continue
						}
						geo, isp := getNodeGeoAndISP(pc.rootNode, d.NodeId)
						result := NewPingResult(geo, isp, "", pc.Name())
						result.Update(float64(d.Result) / 1000)
						pc.resultChan <- result
					}
				}
				times++
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
		return fmt.Errorf("update ping pe cookie failed: %w", err)
	}
	pc.cookie = cookie
	return nil
}

// --- HTTP helpers (inlined from client/http) ---

func doGetRequest(url string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return http.DefaultClient.Do(req)
}

// --- ping.pe API ---

var cookieRegex = regexp.MustCompile(`document.cookie="(.*)";`)

func getPingPeCookie(userAgent string) (string, error) {
	resp, err := doGetRequest(pingPeBaseUrl, map[string]string{"User-Agent": userAgent})
	if err != nil {
		return "", fmt.Errorf("get ping pe cookie failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read ping.pe response failed: %w", err)
	}
	matchs := cookieRegex.FindStringSubmatch(string(data))
	if len(matchs) <= 1 {
		return "", fmt.Errorf("can't get cookie")
	}
	return matchs[1], nil
}

var streamIdRegex = regexp.MustCompile(`stream_id = '([0-9]+)'`)

func getStreamIdAndDoc(host, cookie, userAgent string) (string, string, error) {
	url := fmt.Sprintf(pingPeSubmitUrl, host)
	resp, err := doGetRequest(url, map[string]string{"User-Agent": userAgent, "Cookie": cookie})
	if err != nil {
		return "", "", fmt.Errorf("get ping pe stream id failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read ping.pe response failed: %w", err)
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
	Result      int64  `json:"result"`
	ResultText  string `json:"result_text"`
}

func getResult(streamId, cookie, userAgent string) (*pingPeResult, error) {
	url := fmt.Sprintf(pingPeGetResultUrl, streamId)
	resp, err := doGetRequest(url, map[string]string{"User-Agent": userAgent, "Cookie": cookie})
	if err != nil {
		return nil, fmt.Errorf("get ping pe result failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ping.pe result failed: %w", err)
	}
	result := &pingPeResult{}
	if err := json.Unmarshal(data, result); err != nil {
		return nil, fmt.Errorf("parse ping pe result failed: %w", err)
	}
	return result, nil
}

// --- HTML helpers (inlined from common/util/html_parser.go) ---

func getNodeGeoAndISP(rootNode *html.Node, id string) (string, string) {
	geo, err := getNodeGeo(rootNode, id)
	if err != nil {
		return "", ""
	}
	isp, err := getNodeISP(rootNode, id)
	if err != nil {
		log.Error("get node ISP failed", "id", id, "err", err)
	}
	return geo, isp
}

func getNodeGeo(parser *html.Node, id string) (string, error) {
	node := findNodeByID(parser, fmt.Sprintf("ping-%s-location", id))
	if node == nil {
		return "", fmt.Errorf("can't find node geo by id[%s]", id)
	}
	spans := findTagsByClass(node, "td-location")
	if len(spans) == 0 {
		return "", fmt.Errorf("can't find node location by id[%s]", id)
	}
	return extractNodeValue(spans[0]), nil
}

func getNodeISP(parser *html.Node, id string) (string, error) {
	node := findNodeByID(parser, fmt.Sprintf("ping-%s-provider", id))
	if node == nil {
		return "", fmt.Errorf("can't find node provider by id[%s]", id)
	}
	spans := findTagsByClass(node, "td-provider")
	if len(spans) == 0 {
		return "", fmt.Errorf("can't find node provider by id[%s]", id)
	}
	return extractNodeValue(spans[0]), nil
}

func findNodeByID(node *html.Node, id string) *html.Node {
	if node.Type == html.ElementNode {
		for _, attr := range node.Attr {
			if attr.Key == "id" && attr.Val == id {
				return node
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findNodeByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

func findTagsByClass(node *html.Node, className string) []*html.Node {
	var tags []*html.Node
	if node.Type == html.ElementNode {
		for _, attr := range node.Attr {
			if attr.Key == "class" && attr.Val == className {
				tags = append(tags, node)
				break
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		tags = append(tags, findTagsByClass(child, className)...)
	}
	return tags
}

func extractNodeValue(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}
	var value string
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		value += extractNodeValue(child)
	}
	return value
}
