package converter

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/lureiny/v2raymg/common/log/logger"
	"gopkg.in/yaml.v3"
)

const (
	vlessUriHeader        = "vless://"
	vmessUriHeader        = "vmess://"
	trojanUriHeader       = "trojan://"
	hysteriaUriHeader     = "hysteria2://"
	shadowsockesUriHeader = "ss://"

	surgeClientKeyWord  = "surge"
	qv2rayClientKeyWrod = "qv2ray"
	clashClientKeyWord  = "clash"
	commonClientKeyWord = "common"

	wsNet       = "ws"
	h2Net       = "h2"
	grpcNet     = "grpc"
	tcpNet      = "tcp"
	originalNet = "original"

	proxiesKey     = "proxies"
	proxyGroupsKey = "proxy-groups"
)

var defaultAlterId int = 0

func decodeVmessStandardUri(standardUri string) string {
	// vmess内容经过base64编码
	if strings.HasPrefix(standardUri, vmessUriHeader) {
		plaintext, err := base64.RawURLEncoding.DecodeString(standardUri[len(vmessUriHeader):])
		if err != nil {
			logger.Error("use base64 url encoding decode vmess uri[%s] fail > err: %v", standardUri[len(vmessUriHeader):], err)
			return standardUri
		}
		return string(plaintext)
	} else {
		return standardUri
	}
}

func httpGet(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http get return %d code", resp.StatusCode)
	}
	return ioutil.ReadAll(resp.Body)
}

type NodeMap map[string]yaml.Node

func decodeShadowsocksUrl(parsedUri *url.URL) (method, port, password, server string, err error) {
	err = nil
	// ss订阅编码前格式为 ss://method:password@server:port
	rawData, err := base64.RawStdEncoding.DecodeString(parsedUri.Host)
	if err != nil {
		logger.Error("decode ss url[%s] > err: %v", parsedUri.String(), err)
		err = fmt.Errorf("decode ss url[%s] > err: %v", parsedUri.String(), err)
		return
	}
	uriParts := strings.Split(string(rawData), ":")

	if len(uriParts) != 3 {
		logger.Error("ss url[%s] is not standard", string(rawData))
		err = fmt.Errorf("ss url[%s] is not standard", string(rawData))
		return
	}
	method = uriParts[0]
	port = uriParts[2]
	passwordAndServer := strings.Split(uriParts[1], "@")
	if len(passwordAndServer) != 2 {
		logger.Error("ss password and server part is not standard: %s", uriParts[1])
		err = fmt.Errorf("ss password and server part is not standard: %s", uriParts[1])
		return
	}
	password = passwordAndServer[0]
	server = passwordAndServer[1]
	return
}
