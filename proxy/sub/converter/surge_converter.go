package converter

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/proxy/sub"
)

type SurgeConverter struct{}

func NewSurgeConverter() Converter {
	return &SurgeConverter{}
}

func (c *SurgeConverter) Name() string {
	return surgeClientKeyWord
}

func (c *SurgeConverter) Convert(standardUris []string) (string, error) {
	surgeSubUris := []string{}
	for _, uri := range standardUris {
		if strings.HasPrefix(uri, vlessUriHeader) {
			// surge不支持vless
			continue
		} else if strings.HasPrefix(uri, vmessUriHeader) {
			rawUri := decodeVmessStandardUri(uri)
			vmessShareConfig := sub.NewDefaultVmessShareConfig()
			err := json.Unmarshal([]byte(rawUri), vmessShareConfig)
			if err != nil {
				logger.Error("parse vmess shared config[%s] err > %v", uri, err)
				continue
			}
			if surgeUri := getSurgeVmessUri(vmessShareConfig); surgeUri != "" {
				surgeSubUris = append(surgeSubUris, surgeUri)
			}

		} else if strings.HasPrefix(uri, trojanUriHeader) {
			u, err := url.Parse(uri)
			if err != nil {
				logger.Error("parse trojan shared uri[%s] err > %v", uri, err)
				continue
			}
			if surgeUri := getSurgeTrojanUri(u); surgeUri != "" {
				surgeSubUris = append(surgeSubUris, surgeUri)
			}
		} else if strings.HasPrefix(uri, hysteriaUriHeader) {
			u, err := url.Parse(uri)
			if err != nil {
				logger.Error("parse hysteria2 shared uri[%s] err > %v", uri, err)
				continue
			}
			if surgeUri := getSurgeHysteriaUri(u); surgeUri != "" {
				surgeSubUris = append(surgeSubUris, surgeUri)
			}
		} else if strings.HasPrefix(uri, shadowsockesUriHeader) {
			u, err := url.Parse(uri)
			if err != nil {
				logger.Error("parse shadowsocks shared uri[%s] err > %v", uri, err)
				continue
			}
			if surgeUri := getSurgeSSUri(u); surgeUri != "" {
				surgeSubUris = append(surgeSubUris, surgeUri)
			}
		}
	}
	return strings.Join(surgeSubUris, "\n"), nil
}

func getSurgeVmessUri(vmessShareConfig *sub.VmessShareConfig) string {
	surgeVmessUriParts := []string{
		fmt.Sprintf("🍀 VMESS_%s=vmess", vmessShareConfig.PS),
		vmessShareConfig.Add,
		vmessShareConfig.Port,
		fmt.Sprintf("username=%s", vmessShareConfig.ID),
	}
	if vmessShareConfig.Sni != "" {
		surgeVmessUriParts = append(surgeVmessUriParts, fmt.Sprintf("sni=%s", vmessShareConfig.Sni))
	}
	if vmessShareConfig.Net == wsNet {
		surgeVmessUriParts = append(surgeVmessUriParts, "ws=true")
		surgeVmessUriParts = append(surgeVmessUriParts, fmt.Sprintf("ws-path=%s", vmessShareConfig.Path))
		if vmessShareConfig.Host != "" {
			surgeVmessUriParts = append(surgeVmessUriParts, fmt.Sprintf("ws-headers=Host:\"%s\"", vmessShareConfig.Host))
		}
	}
	if vmessShareConfig.TLS == "tls" {
		surgeVmessUriParts = append(surgeVmessUriParts, "tls=true")
	}
	if vmessShareConfig.Aid == 0 {
		surgeVmessUriParts = append(surgeVmessUriParts, "vmess-aead=true")
	}
	return strings.Join(surgeVmessUriParts, ", ")
}

func getSurgeTrojanUri(parsedUri *url.URL) string {
	// surge不支持xtls, 也不支持不加密情况
	if parsedUri.Query().Get("security") == "xtls" || parsedUri.Query().Get("security") == "none" {
		return ""
	}
	surgeTrojanUriParts := []string{
		fmt.Sprintf("🌿 TROJAN_%s=trojan", parsedUri.Fragment),
		parsedUri.Hostname(),
		parsedUri.Port(),
		fmt.Sprintf("password=%s", parsedUri.User.Username()),
		"tfo=true, tls=true", // 默认打开tls
	}
	transferType := parsedUri.Query().Get("type")
	if transferType == "ws" {
		surgeTrojanUriParts = append(surgeTrojanUriParts, "ws=true")
		if parsedUri.Query().Get("path") == "" {
			logger.Error("Err=trojan ws path is empty")
			return ""
		}
		surgeTrojanUriParts = append(surgeTrojanUriParts, fmt.Sprintf("ws-path=%s", parsedUri.Query().Get("path")))
		if parsedUri.Query().Get("host") != "" {
			surgeTrojanUriParts = append(surgeTrojanUriParts, fmt.Sprintf("ws-headers=Host:\"%s\"", parsedUri.Query().Get("host")))
		}
	}

	if parsedUri.Query().Get("sni") != "" {
		surgeTrojanUriParts = append(surgeTrojanUriParts, fmt.Sprintf("sni=%s", parsedUri.Query().Get("sni")))
	}

	return strings.Join(surgeTrojanUriParts, ", ")
}

func getSurgeHysteriaUri(parsedUri *url.URL) string {
	surgeHysteriaUriParts := []string{
		fmt.Sprintf("🌿 HYSTERIA2_%s=hysteria2", parsedUri.Fragment),
		parsedUri.Hostname(),
		parsedUri.Port(),
		fmt.Sprintf("password=%s", parsedUri.User.Username()),
		"download-bandwidth=1000",
		"ecn=true",
	}
	return strings.Join(surgeHysteriaUriParts, ", ")
}

func getSurgeSSUri(parsedUri *url.URL) string {
	method, port, password, server, err := decodeShadowsocksUrl(parsedUri)
	if err != nil {
		logger.Error("parse ss uri fail > err: %v", err)
		return ""
	}

	surgeSSUriParts := []string{
		fmt.Sprintf("🌿 SS_%s=ss", parsedUri.Fragment),
		server,
		port,
		fmt.Sprintf("encrypt-method=%s", method),
		fmt.Sprintf("password=%s", password),
	}
	return strings.Join(surgeSSUriParts, ", ")
}

func init() {
	registerConverter(NewSurgeConverter())
}
