package pingpe

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"

	httpclient "github.com/lureiny/v2raymg/client/http"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/log/logger"
	htmlparser "github.com/lureiny/v2raymg/ping/html_parser"
	"golang.org/x/net/html"
)

var userAgentIndex = 0

var cookieRegex = regexp.MustCompile(`document.cookie="(.*)";`)

func getPingPeCookie() (string, error) {
	headers := map[string]string{
		"User-Agent": common.UserAgents[userAgentIndex%len(common.UserAgents)],
	}
	userAgentIndex += 1
	resp, err := httpclient.DoGetRequest(common.PingPeBaseUrl, headers)
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

func getStreamIdAndDoc(host, cookie string) (string, string, error) {
	headers := map[string]string{
		"User-Agent": common.UserAgents[userAgentIndex%len(common.UserAgents)],
		"Cookie":     cookie,
	}
	userAgentIndex += 1
	url := fmt.Sprintf(common.PingPeSubmitUrl, host)
	resp, err := httpclient.DoGetRequest(url, headers)
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
	Data  []*pingPeResultData `json:"data"`
	State *pingPeState        `json:"state"`
}

type pingPeResultData struct {
	NodeId      string `json:"node_id"`
	TimestampMs int64  `json:"timestamp_ms"`
	Result      int64  `json:"result"` // -2000 代表不可达, -1000代表还没有数据, 大于0表示正常探测
	ResultText  string `json:"result_text"`
}

type pingPeState struct {
	OutstandingNodeCount int64 `json:"outstandingNodeCount"`
}

func getResult(streamId, cookie string) (*pingPeResult, error) {
	headers := map[string]string{
		"User-Agent": common.UserAgents[userAgentIndex%len(common.UserAgents)],
		"Cookie":     cookie,
	}
	userAgentIndex += 1
	url := fmt.Sprintf(common.PingPeGetResultUrl, streamId)
	resp, err := httpclient.DoGetRequest(url, headers)
	if err != nil {
		return nil, fmt.Errorf("get ping pe result fail, err: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ping.pe result fail, err: %v", err)
	}
	reulst := &pingPeResult{}
	if err := json.Unmarshal(data, reulst); err != nil {
		return nil, fmt.Errorf("parse ping pe result fail, err: %v", err)
	}
	return reulst, nil
}

func getNodeGeoAndISP(rootNode *html.Node, id string) (string, string) {
	geo, err := getNodeGeo(rootNode, id)
	if err != nil {
		logger.Error("get geo of id[%d] fail, err : %v", id, err)
	}
	isp, err := getNodeISP(rootNode, id)
	if err != nil {
		logger.Error("get isp of id[%d] fail, err : %v", id, err)
	}
	return geo, isp
}

func getNodeGeo(parser *html.Node, id string) (string, error) {
	node := htmlparser.FindNodeByID(parser, fmt.Sprintf("ping-%s-location", id))
	if node == nil {
		return "", fmt.Errorf("can't find node geo by id[%s]", id)
	}
	spans := htmlparser.FindTagsByClass(node, "td-location")
	if len(spans) == 0 {
		return "", fmt.Errorf("can't find node location by id[%s]", id)
	}
	return htmlparser.ExtractNodeValue(spans[0]), nil
}

func getNodeISP(parser *html.Node, id string) (string, error) {
	node := htmlparser.FindNodeByID(parser, fmt.Sprintf("ping-%s-provider", id))
	if node == nil {
		return "", fmt.Errorf("can't find node geo by id[%s]", id)
	}
	spans := htmlparser.FindTagsByClass(node, "td-provider")
	if len(spans) == 0 {
		return "", fmt.Errorf("can't find node provider by id[%s]", id)
	}
	return htmlparser.ExtractNodeValue(spans[0]), nil
}
