package converter

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/proxy/sub"

	"gopkg.in/yaml.v3"
)

var subConverterMap = []string{
	"https://sub.xeton.dev/sub?target=clash&new_name=true&url=%20&insert=true&emoji=true",
	"https://api.dler.io/sub?target=clash&new_name=true&url=%20&insert=true&emoji=true",
	"https://sub.maoxiongnet.com/sub?target=clash&new_name=true&url=%20&insert=true&emoji=true",
	"https://sub.id9.cc/sub?target=clash&new_name=true&url=%20&insert=false&emoji=true",
}

type ClashConverter struct{}

func NewClashConverter() Converter {
	return &ClashConverter{}
}

func (c *ClashConverter) Name() string {
	return clashClientKeyWord
}

func (c *ClashConverter) Convert(standardUris []string) (string, error) {
	// 1. parse uri to ClashProxy
	// 2. downlaod and unmarshal template
	// 3. marshal config
	clashProxies := getClashProxies(standardUris)
	data, err := yaml.Marshal(clashProxies)
	if err != nil {
		return "", err
	}
	proxyNode := &yaml.Node{}
	if err := yaml.Unmarshal(data, proxyNode); err != nil {
		return "", err
	}
	nodeMap, err := getTemplate()
	if err != nil {
		return "", err
	}
	if _, ok := nodeMap[proxiesKey]; !ok {
		return "", err
	}
	nodeMap[proxiesKey] = *proxyNode.Content[0]
	addToProxyGroup(&nodeMap, "", clashProxies)
	data, err = yaml.Marshal(nodeMap)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func addToProxyGroup(nodeMap *NodeMap, groupName string, clashProxies []*ClashProxy) {
	proxyGroups, ok := (*nodeMap)[proxyGroupsKey]
	if !ok {
		return
	}
	targetIndex := 0
	for i, n := range proxyGroups.Content {
		data, err := yaml.Marshal(n)
		if err != nil {
			continue
		}
		proxyGroupNode := &ProxyGroup{}
		if err := yaml.Unmarshal(data, proxyGroupNode); err != nil {
			continue
		}
		if proxyGroupNode.Name == groupName {
			targetIndex = i
		}
	}
	data, err := yaml.Marshal(proxyGroups.Content[targetIndex])
	if err != nil {
		return
	}
	targetNode := &ProxyGroup{}
	if err := yaml.Unmarshal(data, targetNode); err != nil {
		return
	}
	for _, c := range clashProxies {
		targetNode.Proxies = append(targetNode.Proxies, c.Name)
	}
	data, err = yaml.Marshal(targetNode)
	if err != nil {
		return
	}
	node := &yaml.Node{}
	if err := yaml.Unmarshal(data, node); err != nil {
		return
	}
	(*nodeMap)[proxyGroupsKey].Content[targetIndex] = node.Content[0]
}

func getTemplate() (NodeMap, error) {
	errMsg := ""
	for _, v := range subConverterMap {
		data, err := httpGet(v)
		if err != nil {
			errMsg = errMsg + "|" + err.Error()
			continue
		}
		nodeMap := NodeMap{}
		if err := yaml.Unmarshal(data, nodeMap); err != nil {
			errMsg = errMsg + "|" + err.Error()
			continue
		}
		return nodeMap, nil
	}
	return nil, fmt.Errorf("%v", errMsg)
}

func getClashProxies(standardUris []string) []*ClashProxy {
	clashProxies := []*ClashProxy{}
	for _, uri := range standardUris {
		if strings.HasPrefix(uri, vlessUriHeader) {
			// clash不支持vless
			continue
		} else if strings.HasPrefix(uri, vmessUriHeader) {
			rawUri := decodeVmessStandardUri(uri)
			vmessShareConfig := sub.NewDefaultVmessShareConfig()
			err := json.Unmarshal([]byte(rawUri), vmessShareConfig)
			if err != nil {
				logger.Error("parse vmess shared config err > %v", err)
				continue
			}
			if clashProxy := getClashVmessUri(vmessShareConfig); clashProxy != nil {
				clashProxies = append(clashProxies, clashProxy)
			}

		} else if strings.HasPrefix(uri, trojanUriHeader) {
			u, err := url.Parse(uri)
			if err != nil {
				logger.Error("parse trojan shared uri err > %v", err)
				continue
			}
			if clashProxy := getClashTrojanUri(u); clashProxy != nil {
				clashProxies = append(clashProxies, clashProxy)
			}
			// } else if strings.HasPrefix(uri, hysteriaUriHeader) {
			// 	u, err := url.Parse(uri)
			// 	if err != nil {
			// 		logger.Error("parse trojan shared uri err > %v\n", err)
			// 		continue
			// 	}
			// 	if clashProxy := getClashHysteriaUri(u); clashProxy != nil {
			// 		clashProxies = append(clashProxies, clashProxy)
			// 	}
		} else if strings.HasPrefix(uri, shadowsockesUriHeader) {
			u, err := url.Parse(uri)
			if err != nil {
				logger.Error("parse shadowsocks shared uri[%s] err > %v", uri, err)
				continue
			}
			if clashProxy := getClashSSUri(u); clashProxy != nil {
				clashProxies = append(clashProxies, clashProxy)
			}
		}
	}
	return clashProxies
}

func getClashTrojanUri(parsedUri *url.URL) *ClashProxy {
	// clash不支持xtls, 也不支持不加密情况
	if parsedUri.Query().Get("security") == "xtls" || parsedUri.Query().Get("security") == "none" {
		return nil
	}
	clashProxy := &ClashProxy{}
	clashProxy.Name = fmt.Sprintf("🌿 TROJAN_%s", parsedUri.Fragment)
	clashProxy.Type = "trojan"
	clashProxy.Server = parsedUri.Hostname()
	clashProxy.Port, _ = strconv.Atoi(parsedUri.Port())
	clashProxy.Password = parsedUri.User.Username()
	if parsedUri.Query().Get("sni") != "" {
		clashProxy.SNI = parsedUri.Query().Get("sni")
	}
	transferType := parsedUri.Query().Get("type")
	if transferType == wsNet {
		clashProxy.Network = wsNet
		if parsedUri.Query().Get("path") == "" {
			logger.Error("Err=trojan ws path is empty")
			return nil
		}
		clashProxy.WSOpts = &WSOpts{
			Path: parsedUri.Query().Get("path"),
		}
		if parsedUri.Query().Get("host") != "" {
			clashProxy.WSOpts.Headers = &WSHeaders{Host: parsedUri.Query().Get("host")}
		}
	} else if transferType == grpcNet {
		clashProxy.Network = grpcNet
		if parsedUri.Query().Get("serviceName") != "" {
			clashProxy.GrpcOpts = &GrpcOpts{
				GrpcServiceName: parsedUri.Query().Get("serviceName"),
			}
		}
	} else if transferType == tcpNet || transferType == originalNet {
		return clashProxy
	} else {
		return nil
	}
	return clashProxy
}

func getClashHysteriaUri(parsedUri *url.URL) *ClashProxy {
	clashProxy := &ClashProxy{}
	clashProxy.Name = fmt.Sprintf("🌿 TROJAN_%s", parsedUri.Fragment)
	clashProxy.Type = "hysteria"
	clashProxy.Server = parsedUri.Hostname()
	clashProxy.Port, _ = strconv.Atoi(parsedUri.Port())
	clashProxy.Password = parsedUri.User.Username()
	if parsedUri.Query().Get("sni") != "" {
		clashProxy.SNI = parsedUri.Query().Get("sni")
	}
	return clashProxy
}

func getClashSSUri(parsedUri *url.URL) *ClashProxy {
	method, port, password, server, err := decodeShadowsocksUrl(parsedUri)
	if err != nil {
		logger.Error("parse ss uri fail > err: %v", err)
		return nil
	}
	clashProxy := &ClashProxy{}
	clashProxy.Name = fmt.Sprintf("🌿 SS_%s", parsedUri.Fragment)
	clashProxy.Type = "ss"
	clashProxy.Server = server
	clashProxy.Port, _ = strconv.Atoi(port)
	clashProxy.Password = password
	clashProxy.Cipher = method
	return clashProxy
}

func getClashVmessUri(vmessShareConfig *sub.VmessShareConfig) *ClashProxy {
	clashProxy := &ClashProxy{}
	clashProxy.Name = fmt.Sprintf("🍀 VMESS_%s", vmessShareConfig.PS)
	clashProxy.Type = "vmess"
	clashProxy.Server = vmessShareConfig.Add
	clashProxy.Port, _ = strconv.Atoi(vmessShareConfig.Port)
	clashProxy.UUID = vmessShareConfig.ID
	clashProxy.Cipher = "auto"
	clashProxy.AlterId = &defaultAlterId
	if vmessShareConfig.Sni != "" {
		clashProxy.Servername = vmessShareConfig.Sni
	}
	if vmessShareConfig.TLS != "" {
		clashProxy.TLS = true
	}
	if vmessShareConfig.Net == wsNet {
		clashProxy.Network = wsNet
		wsOpts := &WSOpts{
			Path: vmessShareConfig.Path,
		}
		if vmessShareConfig.Host != "" {
			wsOpts.Headers = &WSHeaders{
				Host: vmessShareConfig.Host,
			}
		}
		clashProxy.WSOpts = wsOpts
	} else if vmessShareConfig.Net == h2Net {
		clashProxy.Network = h2Net
		clashProxy.H2Opts = &H2Opts{
			Host: []string{vmessShareConfig.Host},
			Path: vmessShareConfig.Path,
		}
	} else if vmessShareConfig.Net == grpcNet {
		clashProxy.Network = grpcNet
		clashProxy.GrpcOpts = &GrpcOpts{
			GrpcServiceName: vmessShareConfig.Path,
		}
	} else if vmessShareConfig.Net == tcpNet {
		return clashProxy
	} else {
		return nil
	}
	return clashProxy
}

func init() {
	registerConverter(NewClashConverter())
}

type ClashProxy struct {
	Name           string      `yaml:"name"`
	Type           string      `yaml:"type"`
	Server         string      `yaml:"server"`
	Port           int         `yaml:"port"`
	AlterId        *int        `yaml:"alterId,omitempty"`
	Password       string      `yaml:"password,omitempty"`
	UUID           string      `yaml:"uuid,omitempty"`
	Cipher         string      `yaml:"cipher,omitempty"`
	Plugin         string      `yaml:"plugin,omitempty"`
	UDP            bool        `yaml:"udp,omitempty"`
	TLS            bool        `yaml:"tls,omitempty"`
	SkipCertVerify bool        `yaml:"skip-cert-verify,omitempty"`
	Servername     string      `yaml:"servername,omitempty"`
	Network        string      `yaml:"network,omitempty"`
	SNI            string      `yaml:"sni,omitempty"` // trojan
	PluginOpts     *PluginOpts `yaml:"plugin-opts,omitempty"`
	WSOpts         *WSOpts     `yaml:"ws-opts,omitempty"`
	H2Opts         *H2Opts     `yaml:"h2-opts,omitempty"`
	HttpOpts       *HttpOpts   `yaml:"http-opts,omitempty"`
	GrpcOpts       *GrpcOpts   `yaml:"grpc-opts,omitempty"`
}

type PluginOpts struct {
	Mode           string `yaml:"mode"`
	Host           string `yaml:"host,omitempty"`
	TLS            bool   `yaml:"tls,omitempty"`
	SkipCertVerify bool   `yaml:"skip-cert-verify,omitempty"`
	Path           string `yaml:"path,omitempty"`
	MUX            bool   `yaml:"mux,omitempty"`
	// TODO: headers: https://github.com/Dreamacro/clash/wiki/configuration
}

type WSOpts struct {
	Path                string     `yaml:"path,omitempty"`
	Headers             *WSHeaders `yaml:"headers,omitempty"`
	MaxEarlyData        int        `yaml:"max-early-data,omitempty"`
	EarlyDataHeaderName string     `yaml:"early-data-header-name,omitempty"`
}

type WSHeaders struct {
	Host string `yaml:"Host"`
}

type H2Opts struct {
	Host []string `yaml:"host,omitempty"`
	Path string   `yaml:"path,omitempty"`
}

type HttpOpts struct {
	Path   []string `yaml:"path,omitempty"`
	Method string   `yaml:"method,omitempty"`
	// TODO: headers: https://github.com/Dreamacro/clash/wiki/configuration
}

type GrpcOpts struct {
	GrpcServiceName string `yaml:"grpc-service-name"`
}

type ProxyGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}
