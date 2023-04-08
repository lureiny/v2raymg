package converter

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	vlessUriHeader  = "vless://"
	vmessUriHeader  = "vmess://"
	trojanUriHeader = "trojan://"

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

func decodeStandardUri(standardUri string) string {
	// vmess因为内容经过base64编码
	if strings.HasPrefix(standardUri, vmessUriHeader) {
		plaintext, err := base64.StdEncoding.DecodeString(standardUri[len(vmessUriHeader):])
		if err != nil {
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
