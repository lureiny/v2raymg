//go:build !v2ray

package sub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/lureiny/v2raymg/proxy/config"
	"github.com/lureiny/v2raymg/proxy/manager"
)

var proxyManager = manager.GetProxyManager()

const (
	vlessUriHeader  = "vless://"
	vmessUriHeader  = "vmess://"
	trojanUriHeader = "trojan://"

	surgeUserAgentKeyWord  = "surge"
	qv2rayUserAgentKeyWrok = "qv2ray"
)

// GetUserSubUri 获取某个指定用户的订阅uri
func GetUserSubUri(user, tag, host, nodeName string, port uint32, useSNI bool) (string, error) {
	inbound := proxyManager.GetInbound(tag)
	if inbound == nil {
		return "", fmt.Errorf("inbound with tag(%s) is not exist", tag)
	}

	inbound.RWMutex.RLock()
	defer inbound.RWMutex.RUnlock()

	switch strings.ToLower(inbound.Config.Protocol) {
	case manager.VlessProtocolName:
		return GetVlessSub(&inbound.Config, user, host, nodeName, port, useSNI)
	case manager.VmessProtocolName:
		return GetVmessSub(&inbound.Config, user, host, nodeName, port, useSNI)
	case manager.TrojanProtocolName:
		return GetTrojanSub(&inbound.Config, user, host, nodeName, port, useSNI)
	default:
		return "", fmt.Errorf("not support protocol: %s", inbound.Config.Protocol)
	}
}

func GetVlessSub(in *config.InboundDetourConfig, user, host, nodeName string, port uint32, useSNI bool) (string, error) {
	u, err := NewVlessShareConfig(in, user, host, port)
	if err != nil {
		return "", err
	}
	u.NodeName = nodeName
	u.UseSNI = useSNI
	return getVlessUri(u)
}

func GetVmessSub(in *config.InboundDetourConfig, user, host, nodeName string, port uint32, useSNI bool) (string, error) {
	u, err := NewVmessShareConfig(in, user, host, port)
	if err != nil {
		return "", err
	}
	u.PS = nodeName
	u.UseSNI = useSNI
	return getVmessUri(u)
}

func GetTrojanSub(in *config.InboundDetourConfig, user, host, nodeName string, port uint32, useSNI bool) (string, error) {
	u, err := NewTrojanShareConfig(in, user, host, port)
	if err != nil {
		return "", err
	}
	u.NodeName = nodeName
	u.UseSNI = useSNI
	return getTrojanUri(u)
}

func getVmessUri(u *VmessShareConfig) (string, error) {
	if !u.UseSNI {
		u.Sni = ""
	}
	b, err := json.Marshal(u)
	if err != nil {
		return "", nil
	}
	return fmt.Sprintf("vmess://%s", base64.StdEncoding.EncodeToString(b)), nil
}

func getVlessUri(v *VlessShareConfig) (string, error) {
	uri := fmt.Sprintf("vless://%s", v.Build())
	return uri, nil
}

func getTrojanUri(t *TrojanShareConfig) (string, error) {
	uri := fmt.Sprintf("trojan://%s", t.Build())
	return uri, nil
}

func TransferSubUri(standardUris []string, clientUserAgent string) string {
	isQv2ray := strings.Contains(strings.ToLower(clientUserAgent), qv2rayUserAgentKeyWrok)
	isSurge := strings.Contains(strings.ToLower(clientUserAgent), surgeUserAgentKeyWord)

	if isQv2ray {
		return strings.Join(standardUris, "\n")
	} else if isSurge {
		return transferSubToSurge(standardUris)
	} else {
		// base64 encode
		uri := strings.Join(standardUris, "\n")
		return base64.StdEncoding.EncodeToString([]byte(uri))
	}
}

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

func transferSubToSurge(standardUris []string) string {
	surgeSubUris := []string{}
	for _, uri := range standardUris {
		if strings.HasPrefix(uri, vlessUriHeader) {
			// surge不支持vless
			continue
		} else if strings.HasPrefix(uri, vmessUriHeader) {
			rawUri := decodeStandardUri(uri)
			vmessShareConfig := NewDefaultVmessShareConfig()
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
	return strings.Join(surgeSubUris, "\n")
}

func getSurgeVmessUri(vmessShareConfig *VmessShareConfig) string {
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
	if vmessShareConfig.Net == "ws" {
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
	}

	return strings.Join(surgeTrojanUriParts, ", ")
}
