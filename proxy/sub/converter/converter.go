package converter

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/lureiny/v2raymg/global/logger"
	"github.com/lureiny/v2raymg/proxy/sub"
)

type Opt func([]string) []string

type Converter interface {
	// get converter name
	Name() string
	// convert raw sub uri to the format of different client
	Convert([]string, ...Opt) (string, error)
}

type sniOpt func(string, bool) string

var sniOptMap map[string]sniOpt = map[string]sniOpt{}

func WithOutSni() Opt {
	return func(uris []string) []string {
		return sniBaseOpt(uris, true)
	}
}

func sniBaseOpt(uris []string, reset bool) []string {
	newUris := make([]string, len(uris))
	for _, uri := range uris {
		uriHeader := getUriHeader(uri)
		if sniOptFunc, ok := sniOptMap[uriHeader]; ok {
			newUris = append(newUris, sniOptFunc(uri, reset))
		}
	}
	return newUris
}

func uriSniOpt(uri string, reset bool) string {
	u, err := url.Parse(uri)
	if err != nil {
		logger.Debug("parse trojan shared uri err > %v\n", err)
	}
	if reset {
		u.Query().Set("sni", "")
	}
	return u.String()
}

func trojanSniOpt(uri string, reset bool) string {
	if strings.HasPrefix(uri, trojanUriHeader) {
		return uriSniOpt(uri, reset)
	}
	return uri
}

func vlessSniOpt(uri string, reset bool) string {
	if strings.HasPrefix(uri, vlessUriHeader) {
		return uriSniOpt(uri, reset)
	}
	return uri
}

func vmessSniOpt(uri string, reset bool) string {
	if strings.HasPrefix(uri, vmessUriHeader) {
		rawUri := decodeStandardUri(uri)
		vmessShareConfig := sub.NewDefaultVmessShareConfig()
		err := json.Unmarshal([]byte(rawUri), vmessShareConfig)
		if err != nil {
			logger.Error("parse vmess shared config[%v] err > %v\n", uri, err)
			return uri
		}
		if reset {
			vmessShareConfig.Sni = ""
		}

		newUri, err := sub.GetVmessUri(vmessShareConfig)
		if err != nil {
			return uri
		} else {
			return newUri
		}
	}
	return uri
}

func getUriHeader(uri string) string {
	if strings.HasPrefix(uri, vlessUriHeader) {
		return vlessUriHeader
	} else if strings.HasPrefix(uri, vmessUriHeader) {
		return vmessUriHeader
	} else if strings.HasPrefix(uri, trojanUriHeader) {
		return trojanUriHeader
	} else {
		logger.Debug("uri[%s] has not support header", uri)
		return ""
	}
}

func init() {
	sniOptMap[trojanUriHeader] = trojanSniOpt
	sniOptMap[vlessUriHeader] = vlessSniOpt
	sniOptMap[vmessUriHeader] = vmessSniOpt
}
