//go:build !v2ray

package sub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/global/proxy"
	"github.com/lureiny/v2raymg/proxy/config"
	"github.com/lureiny/v2raymg/proxy/manager"
)

// GetUserSubUri 获取某个指定用户的订阅uri
func GetUserSubUri(user, tag, host, nodeName string, port uint32, useSNI bool) (string, error) {
	inbound := proxy.GetInbound(tag)
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
	return fmt.Sprintf("vmess://%s", base64.RawURLEncoding.EncodeToString(b)), nil
}

func getVlessUri(v *VlessShareConfig) (string, error) {
	uri := fmt.Sprintf("vless://%s", v.Build())
	return uri, nil
}

func getTrojanUri(t *TrojanShareConfig) (string, error) {
	uri := fmt.Sprintf("trojan://%s", t.Build())
	return uri, nil
}
