package proxy

import (
	"fmt"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/lego"
	pc "github.com/lureiny/v2raymg/proxy/config"
	"github.com/lureiny/v2raymg/proxy/manager"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

var proxyManager = manager.NewProxyManager()

// InitProxyManager ...
func InitProxyManager(configFile, version string, cm *lego.CertManager) error {
	if err := proxyManager.Init(configFile, version, cm); err != nil {
		return err
	}
	rawAdaptive := &manager.RawAdaptive{}
	if err := config.UnmarshalKey(common.ConfigProxyAdaptive, rawAdaptive); err != nil {
		return fmt.Errorf("please check adaptive config > %v", err)
	}
	if err := proxyManager.InitAdaptive(rawAdaptive); err != nil {
		return fmt.Errorf("InitAdaptive fail, err: %v", err)
	}
	proxyManager.AutoFlush(1)
	proxyManager.CycleAdaptive()
	return nil
}

// GetProxyManager ...
func GetProxyManager() *manager.ProxyManager {
	return proxyManager
}

// AddInbound ...
func AddInbound(inboud *manager.Inbound) error {
	return proxyManager.AddInbound(inboud)
}

// DeleteInbound ...
func DeleteInbound(tag string) error {
	return proxyManager.DeleteInbound(tag)
}

// GetInbound 根据tag获取inbound, 不存在返回nil
func GetInbound(tag string) *manager.Inbound {
	return proxyManager.GetInbound(tag)
}

// AddUser ...
func AddUser(user *manager.User) error {
	return proxyManager.AddUser(user)
}

// RemoveUser ...
func RemoveUser(user *manager.User) error {
	return proxyManager.RemoveUser(user)
}

// ResetUser ...
func ResetUser(user *manager.User) error {
	return proxyManager.ResetUser(user)
}

// QueryStats query user, inbound, outbound stat with pattern
func QueryStats(pattern string, reset bool) (*map[string]*proto.Stats, error) {
	return proxyManager.QueryStats(pattern, reset)
}

// TransferInbound 搬迁inbound, 适用于修改端口的场景
func TransferInbound(tag string, newPort uint32) error {
	return proxyManager.TransferInbound(tag, newPort)
}

// CopyInbound 复制inbound, 适用于快速创建相同inbound, 可选是否复制用户
func CopyInbound(srcTag, newTag, newProtocol string, newPort int) error {
	return proxyManager.CopyInbound(srcTag, newTag, newProtocol, newPort)
}

// GetUsersTag 获取proxy中用户tag情况
func GetUsersTag() map[string][]string {
	return proxyManager.GetUsersTag()
}

// GetTags 获取全部tag
func GetTags() []string {
	return proxyManager.GetTags()
}

// GetUpstreamInbound get inbound which fallback to port
func GetUpstreamInbound(port string) (pc.InboundDetourConfig, error) {
	return proxyManager.GetUpstreamInbound(port)
}

// StartProxyServer ...
func StartProxyServer() error {
	return proxyManager.StartProxyServer()
}

// StopProxyServer ...
func StopProxyServer() {
	proxyManager.StopProxyServer()
}

// RestartProxyServer ...
func RestartProxyServer() error {
	return proxyManager.RestartProxyServer()
}

// UpdateProxyServer update proxy server by git tag
func UpdateProxyServer(tag string) error {
	return proxyManager.UpdateProxyServer(tag)
}

// GetProxyServerVersion ...
func GetProxyServerVersion() string {
	return proxyManager.GetProxyServerVersion()
}

// AddAdaptivePort 添加port用于自动更换
func AddAdaptivePort(port interface{}) error {
	return proxyManager.AddAdaptivePort(port)
}

// DeleteAdaptivePort ...
func DeleteAdaptivePort(port int64) {
	proxyManager.DeleteAdaptivePort(port)
}

// AddAdaptiveTag 添加需要自适应变更端口的inbound tag, 返回添加新的接口后adaptive配置的字节组
func AddAdaptiveTag(tag string) error {
	return proxyManager.AddAdaptiveTag(tag)
}

// DeleteAdaptiveTag 删除tag对应inbound的自适应端口变更
func DeleteAdaptiveTag(tag string) {
	proxyManager.DeleteAdaptiveTag(tag)
}

// GetRawAdaptive ...
func GetRawAdaptive() *manager.RawAdaptive {
	return proxyManager.GetRawAdaptive()
}

func GetAdaptiveTags() []string {
	return proxyManager.GetAdaptiveTags()
}

// 对指定tag的inbound进行一次自动修改端口, 最上层接口, 自动变更与主动变更都从这里进入
// 目前采用放回式更新端口, 即刚刚使用的端口下次还有可能选择, 所以端口越多连续重复使用相同端口的概率越低
// 返回值为old port和new port, 供上层调用者做后处理
func AdaptiveOneInbound(tag string) (int64, int64, error) {
	return proxyManager.AdaptiveOneInbound(tag)
}

// CycleAdaptive 自动更新端口
func CycleAdaptive() {
	proxyManager.CycleAdaptive()
}
