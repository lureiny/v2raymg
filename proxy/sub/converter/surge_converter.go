package converter

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/lureiny/v2raymg/proxy/sub"
)

type SurgeConverter struct{}

func NewSurgeConverter() Converter {
	return &SurgeConverter{}
}

func (c *SurgeConverter) Name() string {
	return surgeClientKeyWord
}

func (c *SurgeConverter) Convert(standardUris []string, opts ...Opt) (string, error) {
	for _, opt := range opts {
		standardUris = opt(standardUris)
	}
	surgeSubUris := []string{}
	for _, uri := range standardUris {
		if strings.HasPrefix(uri, vlessUriHeader) {
			// surge不支持vless
			continue
		} else if strings.HasPrefix(uri, vmessUriHeader) {
			rawUri := decodeStandardUri(uri)
			vmessShareConfig := sub.NewDefaultVmessShareConfig()
			err := json.Unmarshal([]byte(rawUri), vmessShareConfig)
			if err != nil {
				log.Printf("parse vmess shared config err > %v\n", err)
				continue
			}
			if surgeUri := getSurgeVmessUri(vmessShareConfig); surgeUri != "" {
				surgeSubUris = append(surgeSubUris, surgeUri)
			}

		} else if strings.HasPrefix(uri, trojanUriHeader) {
			u, err := url.Parse(uri)
			if err != nil {
				log.Printf("parse trojan shared uri err > %v\n", err)
				continue
			}
			if surgeUri := getSurgeTrojanUri(u); surgeUri != "" {
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
		"tls=true, tfo=true, vmess-aead=true", // 默认打开tls
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
			log.Printf("Err=trojan ws path is empty\n")
			return ""
		}
		surgeTrojanUriParts = append(surgeTrojanUriParts, fmt.Sprintf("ws-path=%s", parsedUri.Query().Get("path")))
		if parsedUri.Query().Get("host") != "" {
			surgeTrojanUriParts = append(surgeTrojanUriParts, fmt.Sprintf("ws-headers=Host:\"%s\"", parsedUri.Query().Get("host")))
		}
	}

	if parsedUri.Query().Get("sni") != "" {
		surgeTrojanUriParts = append(surgeTrojanUriParts, fmt.Sprintf("sni=%s", parsedUri.Query().Get("sni")))
	} else {
		surgeTrojanUriParts = append(surgeTrojanUriParts, "skip-cert-verify=true")
	}

	return strings.Join(surgeTrojanUriParts, ", ")
}

func init() {
	registerConverter(NewSurgeConverter())
}
