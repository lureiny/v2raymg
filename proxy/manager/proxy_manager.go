package manager

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/common/template"
	gc "github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/lego"
	"github.com/lureiny/v2raymg/proxy/config"
	"github.com/lureiny/v2raymg/server/rpc/proto"
	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v3"
)

// TODO: proxymanager及proxy部分需要重构

const apiTag = "api"
const supportInboundProtocol = "vmess|vless|trojan|shadowsocks"
const minPort = 100
const maxPort = 65535
const defaultApiPort = 10085

// 默认每天凌晨5点更换一次
const defalutCron = "0 5 * * *"

// 管理proxy相关的基础配置, 不包含用户的变更
type ProxyManager struct {
	ConfigFile     string // 文件目录
	Config         config.V2rayConfig
	needFlush      bool
	InboundManager InboundManager
	rwmutex        sync.RWMutex // bound操作锁
	RuntimeConfig  RuntimeConfig
	proxyServer    *ProxyServer // v2ray/xray server
	hysteriaServer *ProxyServer
	hyConfigFile   string
	hyConfig       *serverConfig
	adaptive       Adaptive
	adaptiveMutex  sync.Mutex // 操作自适应变更时的锁
	certManager    *lego.CertManager
}

func NewProxyManager() *ProxyManager {
	return &ProxyManager{
		rwmutex:        sync.RWMutex{},
		InboundManager: NewInboundManager(),
		adaptiveMutex:  sync.Mutex{},
		adaptive: Adaptive{
			Tags:  map[string]bool{},
			Ports: map[int64]bool{},
		},
	}
}

func (proxyManager *ProxyManager) Init(xrayOrV2rayConfigFile, hysteriaConfig, version string, cm *lego.CertManager) error {
	proxyManager.certManager = cm
	proxyManager.ConfigFile = xrayOrV2rayConfigFile
	proxyManager.hyConfigFile = hysteriaConfig
	if err := checkAndInitProxyConfig(xrayOrV2rayConfigFile, hysteriaConfig); err != nil {
		return fmt.Errorf("check and init proxy config fai > err: %v", err)
	}
	err := proxyManager.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config fail > err: %v", err)
	}

	proxyManager.proxyServer = NewProxyServer(xrayOrV2rayConfigFile, version, "xray")
	// hysteria 使用用最新版
	proxyManager.hysteriaServer = NewProxyServer(hysteriaConfig, "", "hysteria")
	return proxyManager.InitRuntimeConfig(true)
}

// 手动初始化
func (proxyManager *ProxyManager) InitAdaptive(rawAdaptive *RawAdaptive) error {
	if err := proxyManager.adaptive.Init(rawAdaptive); err != nil {
		return err
	}
	for tag := range proxyManager.adaptive.Tags {
		if inbound := proxyManager.InboundManager.Get(tag); inbound == nil {
			// 不存在则删除该tag
			proxyManager.adaptive.DeleteTag(tag)
		}
	}
	return nil
}

// InitRuntimeConfig init api config
func (proxyManager *ProxyManager) InitRuntimeConfig(isManageExec bool) error {
	inbound := proxyManager.GetInbound(apiTag)
	if inbound == nil {
		if !isManageExec {
			return fmt.Errorf("can't not found api inbound")
		}
		inbound = proxyManager.initApiConfig()
		proxyManager.Flush()
	}
	inbound.RWMutex.RLock()
	defer inbound.RWMutex.RUnlock()
	proxyManager.RuntimeConfig = RuntimeConfig{
		Host: "127.0.0.1",
		Port: int(inbound.Config.PortRange),
	}
	return nil
}

func (proxyManager *ProxyManager) initApiConfig() *Inbound {
	s := json.RawMessage(`{"address": "127.0.0.1"}`)
	inbound := &Inbound{
		Tag: apiTag,
		Config: config.InboundDetourConfig{
			Protocol:  "dokodemo-door",
			PortRange: defaultApiPort,
			ListenOn:  "127.0.0.1",
			Tag:       apiTag,
			Settings:  &s,
		},
	}
	// add inbound
	proxyManager.InboundManager.Add(inbound)

	configAllApiInfo(&proxyManager.Config)
	return inbound
}

// AddInbound ...
func (proxyManager *ProxyManager) AddInbound(inbound *Inbound) error {
	if inbound.Tag == apiTag {
		return fmt.Errorf("api inbound can not add")
	}
	if inbound.Config.PortRange < minPort || inbound.Config.PortRange > maxPort {
		return fmt.Errorf("invalid port, port should be in range %d-%d", minPort, maxPort)
	}
	proxyManager.rwmutex.Lock()
	defer proxyManager.rwmutex.Unlock()

	inboundConfigByte, err := inbound.Encode()
	if err != nil {
		return err
	}
	// 添加到配置文件
	err = proxyManager.InboundManager.Add(inbound)
	if err != nil {
		return err
	}
	// 添加到runtime
	err = AddInboundToRuntime(&proxyManager.RuntimeConfig, inboundConfigByte)
	if err != nil {
		proxyManager.InboundManager.Delete(inbound.Tag)
		return err
	}
	proxyManager.needFlush = true
	return nil
}

// DeleteInbound ...
func (proxyManager *ProxyManager) DeleteInbound(tag string) error {
	if tag == apiTag {
		return fmt.Errorf("api inbound can not delete")
	}
	proxyManager.rwmutex.Lock()
	defer proxyManager.rwmutex.Unlock()

	err := RemoveInboundFromRuntime(&proxyManager.RuntimeConfig, tag)
	if err != nil {
		return err
	}

	err = proxyManager.InboundManager.Delete(tag)
	if err != nil {
		return err
	}
	proxyManager.needFlush = true
	return nil
}

// GetInbound 根据tag获取inbound, 不存在返回nil
func (proxyManager *ProxyManager) GetInbound(tag string) *Inbound {
	proxyManager.rwmutex.RLock()
	defer proxyManager.rwmutex.RUnlock()

	return proxyManager.InboundManager.Get(tag)
}

// LoadConfig 从指定的配置文件中加载config
func (proxyManager *ProxyManager) LoadConfig() error {
	proxyManager.rwmutex.Lock()
	defer proxyManager.rwmutex.Unlock()

	content, err := ioutil.ReadFile(proxyManager.ConfigFile)
	if err != nil {
		return err
	}

	err = json.Unmarshal(content, &proxyManager.Config)
	if err != nil {
		return fmt.Errorf("unmarshal xray/v2ray config fail > err: %v", err)
	}

	for _, inbound := range proxyManager.Config.InboundConfigs {
		newInbound := &Inbound{
			Config: inbound,
			Tag:    inbound.Tag,
		}
		if inbound.Tag != apiTag {
			newInbound.CompleteInboundConfigInformation()
		}
		err := proxyManager.InboundManager.Add(newInbound)
		if err != nil {
			return err
		}
	}
	// 加载hysteria config
	if proxyManager.hyConfigFile != "" {
		proxyManager.hyConfig = &serverConfig{}
		data, err := os.ReadFile(proxyManager.hyConfigFile)
		if err != nil {
			return err
		}

		var dataMap map[string]interface{}
		if err := yaml.Unmarshal(data, &dataMap); err != nil {
			return err
		}
		if err := mapstructure.Decode(dataMap, proxyManager.hyConfig); err != nil {
			return fmt.Errorf("mapstructure decode fail > %v", err)
		}
	}

	return nil
}

// Flush() write config to file
func (proxyManager *ProxyManager) Flush() error {
	proxyManager.rwmutex.Lock()
	defer proxyManager.rwmutex.Unlock()

	proxyManager.Config.InboundConfigs = make([]config.InboundDetourConfig, 0)
	for _, inbound := range proxyManager.InboundManager.inbounds {
		inbound.RWMutex.RLock()
		proxyManager.Config.InboundConfigs = append(proxyManager.Config.InboundConfigs, inbound.Config)
		inbound.RWMutex.RUnlock()
	}

	data, err := json.MarshalIndent(proxyManager.Config, "", "    ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(proxyManager.ConfigFile, data, 0777)
	if err != nil {
		return err
	}
	proxyManager.needFlush = false
	return nil
}

// AutoFlush cycle 刷新周期  单位 秒/s
func (proxyManager *ProxyManager) AutoFlush(cycle int64) {
	go func() {
		timeTicker := time.NewTicker(time.Second * time.Duration(cycle))
		for {
			<-timeTicker.C
			if proxyManager.needFlush {
				proxyManager.Flush()
			}
		}
	}()
}

// AddUser ...
func (proxyManager *ProxyManager) AddUser(user *User) error {
	err := CompleteUserInformation(user, proxyManager.GetInbound(user.Tag))
	if err != nil {
		return err
	}
	err = addUserToRuntime(&proxyManager.RuntimeConfig, user)
	if err != nil {
		return err
	}

	return proxyManager.addUserToFile(user)
}

func (proxyManager *ProxyManager) addUserToFile(user *User) error {
	inbound := proxyManager.GetInbound(user.Tag)
	if inbound == nil {
		return fmt.Errorf("inbound with tag(%s) is not exist", user.Tag)
	}
	inbound.RWMutex.Lock()
	defer inbound.RWMutex.Unlock()

	var err error = nil
	switch strings.ToLower(inbound.Config.Protocol) {
	// 添加用户前应先检测是否已经存在
	case VmessProtocolName:
		err = addVmessUser(&inbound.Config, user)
	case VlessProtocolName:
		err = addVlessUser(&inbound.Config, user)
	case TrojanProtocolName:
		err = addTrojanUser(&inbound.Config, user)
	}
	if err == nil {
		proxyManager.needFlush = true
		logger.Debug("Add user to config file, user: %v", user)
	}
	return err
}

// RemoveUser ...
func (proxyManager *ProxyManager) RemoveUser(user *User) error {
	err := CompleteUserInformation(user, proxyManager.GetInbound(user.Tag))
	if err != nil {
		return err
	}
	err = removeUserFromRuntime(&proxyManager.RuntimeConfig, user)
	if err != nil {
		return err
	}

	return proxyManager.removeUserFromFile(user)
}

func (proxyManager *ProxyManager) removeUserFromFile(user *User) error {
	inbound := proxyManager.GetInbound(user.Tag)
	if inbound == nil {
		return fmt.Errorf("inbound with tag(%s) is not exist", user.Tag)
	}
	inbound.RWMutex.Lock()
	defer inbound.RWMutex.Unlock()

	var err error = nil
	switch strings.ToLower(inbound.Config.Protocol) {
	// 添加用户前应先检测是否已经存在, 暂时由运行时操作保证
	case VmessProtocolName:
		err = removeVmessUser(&inbound.Config, user)
	case VlessProtocolName:
		err = removeVlessUser(&inbound.Config, user)
	case TrojanProtocolName:
		err = removeTrojanUser(&inbound.Config, user)
	}
	if err == nil {
		proxyManager.needFlush = true
		logger.Debug("Remove User from runtime: [Email] %s from [Bound] %s", user.Email, user.Tag)
	}
	return err
}

// ResetUser reset user inbound uuid/password
func (proxyManager *ProxyManager) ResetUser(user *User) error {
	err := proxyManager.RemoveUser(user)
	if err != nil {
		return err
	}

	return proxyManager.AddUser(user)
}

// QueryStats query user/inbound stat
func (proxyManager *ProxyManager) QueryStats(pattern string, reset bool) (map[string]*proto.Stats, error) {
	errs := []error{}
	result := map[string]*proto.Stats{}
	if proxyManager.hyConfig != nil {
		hyStats, err := QueryHysteriaStats(
			proxyManager.hyConfig.TrafficStats.Listen,
			proxyManager.hyConfig.TrafficStats.Secret,
			reset)
		errs = append(errs, err)
		mergeStats(hyStats, result)
	}

	xrayV2rayStats, err := QueryStats(pattern, proxyManager.RuntimeConfig.Host, proxyManager.RuntimeConfig.Port, reset)
	errs = append(errs, err)
	mergeStats(xrayV2rayStats, result)
	errMsg := ""
	for _, e := range errs {
		if e != nil {
			errMsg += e.Error()
		}
	}
	if errMsg == "" {
		return result, nil
	}
	return result, fmt.Errorf(errMsg)
}

// TransferInbound 搬迁inbound, 适用于修改端口的场景
func (proxyManager *ProxyManager) TransferInbound(tag string, newPort uint32) error {
	if tag == apiTag {
		return fmt.Errorf("api inbound can not transfer")
	}
	if newPort < minPort || newPort > maxPort {
		return fmt.Errorf("invalid port, port should be in range %d-%d", minPort, maxPort)
	}
	inbound := proxyManager.GetInbound(tag)
	if inbound == nil {
		return fmt.Errorf("Not found inbound with tag(%s)", tag)
	}
	err := proxyManager.DeleteInbound(tag)
	if err != nil {
		return err
	}
	inbound.RWMutex.Lock()
	defer inbound.RWMutex.Unlock()

	oldProt := inbound.Config.PortRange
	if oldProt == newPort {
		return fmt.Errorf("new port(%d) is same with old port", newPort)
	}

	inbound.Config.PortRange = newPort
	err = proxyManager.AddInbound(inbound)
	if err != nil {
		// 可能出现listen失败但是依旧存在该tag的inbound的情况
		proxyManager.DeleteInbound(tag)
		inbound.Config.PortRange = oldProt
		// 回滚
		if lErr := proxyManager.AddInbound(inbound); lErr != nil {
			err = fmt.Errorf("%v > rollback err %v", err, lErr)
		}
		return err
	}
	return nil
}

// CopyInbound 复制inbound, 适用于快速创建相同inbound, 可选是否复制用户
func (proxyManager *ProxyManager) CopyInbound(srcTag, newTag, newProtocol string, newPort int) error {
	if srcTag == apiTag {
		return fmt.Errorf("api inbound can not copy")
	}
	if newPort < minPort || newPort > maxPort {
		return fmt.Errorf("invalid port, port should be in range %d-%d", minPort, maxPort)
	}
	if srcTag == newTag {
		return fmt.Errorf("src tag is same with new tag")
	}
	inbound := proxyManager.GetInbound(srcTag)
	if inbound == nil {
		return fmt.Errorf("Not found inbound with tag(%s)", srcTag)
	}
	newInbound := proxyManager.GetInbound(newTag)
	if newInbound != nil {
		return fmt.Errorf("Inbound with tag(%s) already exist", newTag)
	}
	inbound.RWMutex.RLock()
	defer inbound.RWMutex.RUnlock()

	newInbound = CopyNewInbound(inbound, newProtocol, newTag, newPort)

	return proxyManager.AddInbound(newInbound)
}

// GetUsersTag 获取proxy中用户tag情况
func (proxyManager *ProxyManager) GetUsersTag() map[string][]string {
	proxyManager.rwmutex.RLock()
	defer proxyManager.rwmutex.RUnlock()

	userTagMap := map[string][]string{}

	for _, inbound := range proxyManager.Config.InboundConfigs {
		users := GetInboundUsers(&inbound)
		for _, user := range users {
			if tags, ok := userTagMap[user]; ok {
				userTagMap[user] = append(tags, inbound.Tag)
			} else {
				userTagMap[user] = []string{inbound.Tag}
			}
		}
	}
	return userTagMap
}

// GetTags 获取全部tag
func (proxyManager *ProxyManager) GetTags() []string {
	proxyManager.rwmutex.RLock()
	defer proxyManager.rwmutex.RUnlock()

	tags := []string{}

	for _, inbound := range proxyManager.Config.InboundConfigs {
		if inbound.Tag != apiTag && strings.Contains(supportInboundProtocol, inbound.Protocol) {
			tags = append(tags, inbound.Tag)
		}
	}
	return tags
}

// GetUpstreamInbound get inbound which fallback to port
func (proxyManager *ProxyManager) GetUpstreamInbound(port string) (config.InboundDetourConfig, error) {
	proxyManager.rwmutex.RLock()
	defer proxyManager.rwmutex.RUnlock()

	for _, inboundConfig := range proxyManager.Config.InboundConfigs {
		if isUpstreamInbound(port, &inboundConfig) {
			return inboundConfig, nil
		}
	}
	return config.InboundDetourConfig{}, fmt.Errorf("no upstream inbound of inbound with port(%s)", port)
}

func isUpstreamInbound(port string, inboundConfig *config.InboundDetourConfig) bool {
	protocolName := strings.ToLower(inboundConfig.Protocol)
	if protocolName == VlessProtocolName {
		vlessInboundConfig, err := NewVlessInboundConfig(inboundConfig)
		if err != nil {
			logger.Error("get upstream inbound err > %v", err)
		}
		for _, fallback := range vlessInboundConfig.Fallbacks {
			var i uint16
			var s string
			if err := json.Unmarshal(fallback.Dest, &i); err == nil {
				s = strconv.Itoa(int(i))
			} else {
				_ = json.Unmarshal(fallback.Dest, &s)
			}
			if strings.Contains(s, ":") {
				s = strings.Split(s, ":")[1]
			}
			if s == port {
				return true
			}
		}
	} else if protocolName == TrojanProtocolName {
		trojanInboundConfig, err := NewTrojanInboundConfig(inboundConfig)
		if err != nil {
			logger.Error("get upstream inbound err > %v", err)
		}
		for _, fallback := range trojanInboundConfig.Fallbacks {
			var i uint16
			var s string
			if err := json.Unmarshal(fallback.Dest, &i); err == nil {
				s = strconv.Itoa(int(i))
			} else {
				_ = json.Unmarshal(fallback.Dest, &s)
			}
			if strings.Contains(s, ":") {
				s = strings.Split(s, ":")[1]
			}
			if s == port {
				return true
			}
		}
	}
	return false
}

// StartProxyServer ...
func (proxyManager *ProxyManager) StartProxyServer() error {
	if err := proxyManager.proxyServer.Start(); err != nil {
		logger.Error("start v2ray/xray server fail, err: %v", err)
		return err
	}
	if err := proxyManager.hysteriaServer.Start(); err != nil {
		logger.Error("start hysteria server fail, err: %v", err)
		return err
	}
	return proxyManager.hysteriaServer.Start()
}

// StopProxyServer ...
func (proxyManager *ProxyManager) StopProxyServer() {
	proxyManager.proxyServer.Stop()
	proxyManager.hysteriaServer.Stop()
}

// RestartProxyServer ...
func (proxyManager *ProxyManager) RestartProxyServer() error {
	proxyManager.proxyServer.Stop()
	return proxyManager.proxyServer.Start()
}

// UpdateProxyServer update proxy server by git tag
func (proxyManager *ProxyManager) UpdateProxyServer(tag string) error {
	return proxyManager.proxyServer.Update(tag)
}

// GetProxyServerVersion ...
func (proxyManager *ProxyManager) GetProxyServerVersion() string {
	return proxyManager.proxyServer.currentVersion
}

// AddAdaptivePort 添加port用于自动更换
func (proxyManager *ProxyManager) AddAdaptivePort(port interface{}) error {
	return proxyManager.adaptive.AddPort(port)
}

// DeleteAdaptivePort ...
func (proxyManager *ProxyManager) DeleteAdaptivePort(port int64) {
	proxyManager.adaptive.DeletePort(port)
}

// AddAdaptiveTag 添加需要自适应变更端口的inbound tag, 返回添加新的接口后adaptive配置的字节组
func (proxyManager *ProxyManager) AddAdaptiveTag(tag string) error {
	if inbound := proxyManager.GetInbound(tag); inbound == nil {
		return fmt.Errorf("inbound with tag(%s) not found", tag)
	}
	proxyManager.adaptive.AddTag(tag)
	return nil
}

// DeleteAdaptiveTag 删除tag对应inbound的自适应端口变更
func (proxyManager *ProxyManager) DeleteAdaptiveTag(tag string) {
	proxyManager.adaptive.DeleteTag(tag)
}

// GetRawAdaptive ...
func (proxyManager *ProxyManager) GetRawAdaptive() *RawAdaptive {
	return proxyManager.adaptive.Build()
}

// 如果不存在或者备选端口数小于需要随机的inbound数量的两倍则返回-1
func (proxyManager *ProxyManager) getRandPort() int64 {
	if len(proxyManager.adaptive.Ports) == 1 {
		return -1
	}
	if len(proxyManager.adaptive.Ports) < len(proxyManager.adaptive.Tags)*2 {
		return -1
	}
	rand.Seed(time.Now().UnixNano())
	targetIndex := rand.Intn(len(proxyManager.adaptive.Ports))
	index := 0
	var targetPort int64 = -1
	for port := range proxyManager.adaptive.Ports {
		// 跳过人工添加的最大值
		if port == math.MaxInt64 {
			continue
		}
		if targetIndex == index {
			targetPort = port
			break
		}
		index++
	}
	return targetPort
}

func (proxyManager *ProxyManager) GetAdaptiveTags() []string {
	return proxyManager.adaptive.GetTags()
}

// 对指定tag的inbound进行一次自动修改端口, 最上层接口, 自动变更与主动变更都从这里进入
// 目前采用放回式更新端口, 即刚刚使用的端口下次还有可能选择, 所以端口越多连续重复使用相同端口的概率越低
// 返回值为old port和new port, 供上层调用者做后处理
func (proxyManager *ProxyManager) AdaptiveOneInbound(tag string) (int64, int64, error) {
	proxyManager.adaptiveMutex.Lock()
	defer proxyManager.adaptiveMutex.Unlock()

	newPort := proxyManager.getRandPort()
	inbound := proxyManager.InboundManager.Get(tag)
	if inbound == nil {
		return 0, 0, fmt.Errorf("not found inbound with tag(%s)", tag)
	}

	oldPort := int64(inbound.Config.PortRange)
	if err := proxyManager.TransferInbound(tag, uint32(newPort)); err != nil {
		return oldPort, newPort, err
	}
	// 将已经分配的端口从候选池中去除, 同时回收刚刚使用过的端口
	proxyManager.adaptive.DeletePort(newPort)
	proxyManager.adaptive.AddPort(inbound.Config.PortRange)
	return oldPort, newPort, nil
}

// CycleAdaptive 自动更新端口
func (proxyManager *ProxyManager) CycleAdaptive() {
	cycleFunc := func() {
		for tag := range proxyManager.adaptive.Tags {
			oldPort, newPort, err := proxyManager.AdaptiveOneInbound(tag)
			logger.Info("Tag=%s|OldPort=%d|NewPort=%d|Err=%v", tag, oldPort, newPort, err)
		}
	}
	if _, err := proxyManager.adaptive.Cron.AddFunc(proxyManager.adaptive.CronRule, cycleFunc); err != nil {
		logger.Error("add adaptive cycle func err > %v, use default cronrule: %s", err, defalutCron)
		proxyManager.adaptive.CronRule = defalutCron
		proxyManager.adaptive.Cron.AddFunc(proxyManager.adaptive.CronRule, cycleFunc)
	}
	proxyManager.adaptive.Cron.Start()
}

// TODO: 暂时这么处理
func (proxyManager *ProxyManager) GetUserSub(user, password, nodeName string) string {
	// TODO: 临时兼容方案, 此处只考虑hysteria的订阅链接
	if proxyManager.hysteriaServer != nil {
		p := base64.RawStdEncoding.EncodeToString([]byte(password))
		uri := "hysteria2://" + p + "@" + proxyManager.hyConfig.ACME.Domains[0] + ":443" + "#" + nodeName
		return uri
	}
	return ""
}

func mergeStats(srcMap, dstMap map[string]*proto.Stats) {
	for k, v := range srcMap {
		dstMap[k] = v
	}
}

type serverConfig struct {
	Listen                string                      `mapstructure:"listen"`
	Obfs                  serverConfigObfs            `mapstructure:"obfs"`
	TLS                   *serverConfigTLS            `mapstructure:"tls"`
	ACME                  *serverConfigACME           `mapstructure:"acme"`
	QUIC                  serverConfigQUIC            `mapstructure:"quic"`
	Bandwidth             serverConfigBandwidth       `mapstructure:"bandwidth"`
	IgnoreClientBandwidth bool                        `mapstructure:"ignoreClientBandwidth"`
	DisableUDP            bool                        `mapstructure:"disableUDP"`
	UDPIdleTimeout        time.Duration               `mapstructure:"udpIdleTimeout"`
	Auth                  serverConfigAuth            `mapstructure:"auth"`
	Resolver              serverConfigResolver        `mapstructure:"resolver"`
	ACL                   serverConfigACL             `mapstructure:"acl"`
	Outbounds             []serverConfigOutboundEntry `mapstructure:"outbounds"`
	TrafficStats          serverConfigTrafficStats    `mapstructure:"trafficStats"`
	Masquerade            serverConfigMasquerade      `mapstructure:"masquerade"`
}

type serverConfigObfsSalamander struct {
	Password string `mapstructure:"password"`
}

type serverConfigObfs struct {
	Type       string                     `mapstructure:"type"`
	Salamander serverConfigObfsSalamander `mapstructure:"salamander"`
}

type serverConfigTLS struct {
	Cert string `mapstructure:"cert"`
	Key  string `mapstructure:"key"`
}

type serverConfigACME struct {
	Domains        []string `mapstructure:"domains"`
	Email          string   `mapstructure:"email"`
	CA             string   `mapstructure:"ca"`
	DisableHTTP    bool     `mapstructure:"disableHTTP"`
	DisableTLSALPN bool     `mapstructure:"disableTLSALPN"`
	AltHTTPPort    int      `mapstructure:"altHTTPPort"`
	AltTLSALPNPort int      `mapstructure:"altTLSALPNPort"`
	Dir            string   `mapstructure:"dir"`
}

type serverConfigQUIC struct {
	InitStreamReceiveWindow     uint64        `mapstructure:"initStreamReceiveWindow"`
	MaxStreamReceiveWindow      uint64        `mapstructure:"maxStreamReceiveWindow"`
	InitConnectionReceiveWindow uint64        `mapstructure:"initConnReceiveWindow"`
	MaxConnectionReceiveWindow  uint64        `mapstructure:"maxConnReceiveWindow"`
	MaxIdleTimeout              time.Duration `mapstructure:"maxIdleTimeout"`
	MaxIncomingStreams          int64         `mapstructure:"maxIncomingStreams"`
	DisablePathMTUDiscovery     bool          `mapstructure:"disablePathMTUDiscovery"`
}

type serverConfigBandwidth struct {
	Up   string `mapstructure:"up"`
	Down string `mapstructure:"down"`
}

type serverConfigAuthHTTP struct {
	URL      string `mapstructure:"url"`
	Insecure bool   `mapstructure:"insecure"`
}

type serverConfigAuth struct {
	Type     string               `mapstructure:"type"`
	Password string               `mapstructure:"password"`
	UserPass map[string]string    `mapstructure:"userpass"`
	HTTP     serverConfigAuthHTTP `mapstructure:"http"`
	Command  string               `mapstructure:"command"`
}

type serverConfigResolverTCP struct {
	Addr    string        `mapstructure:"addr"`
	Timeout time.Duration `mapstructure:"timeout"`
}

type serverConfigResolverUDP struct {
	Addr    string        `mapstructure:"addr"`
	Timeout time.Duration `mapstructure:"timeout"`
}

type serverConfigResolverTLS struct {
	Addr     string        `mapstructure:"addr"`
	Timeout  time.Duration `mapstructure:"timeout"`
	SNI      string        `mapstructure:"sni"`
	Insecure bool          `mapstructure:"insecure"`
}

type serverConfigResolverHTTPS struct {
	Addr     string        `mapstructure:"addr"`
	Timeout  time.Duration `mapstructure:"timeout"`
	SNI      string        `mapstructure:"sni"`
	Insecure bool          `mapstructure:"insecure"`
}

type serverConfigResolver struct {
	Type  string                    `mapstructure:"type"`
	TCP   serverConfigResolverTCP   `mapstructure:"tcp"`
	UDP   serverConfigResolverUDP   `mapstructure:"udp"`
	TLS   serverConfigResolverTLS   `mapstructure:"tls"`
	HTTPS serverConfigResolverHTTPS `mapstructure:"https"`
}

type serverConfigACL struct {
	File    string   `mapstructure:"file"`
	Inline  []string `mapstructure:"inline"`
	GeoIP   string   `mapstructure:"geoip"`
	GeoSite string   `mapstructure:"geosite"`
}

type serverConfigOutboundDirect struct {
	Mode       string `mapstructure:"mode"`
	BindIPv4   string `mapstructure:"bindIPv4"`
	BindIPv6   string `mapstructure:"bindIPv6"`
	BindDevice string `mapstructure:"bindDevice"`
}

type serverConfigOutboundSOCKS5 struct {
	Addr     string `mapstructure:"addr"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type serverConfigOutboundHTTP struct {
	URL      string `mapstructure:"url"`
	Insecure bool   `mapstructure:"insecure"`
}

type serverConfigOutboundEntry struct {
	Name   string                     `mapstructure:"name"`
	Type   string                     `mapstructure:"type"`
	Direct serverConfigOutboundDirect `mapstructure:"direct"`
	SOCKS5 serverConfigOutboundSOCKS5 `mapstructure:"socks5"`
	HTTP   serverConfigOutboundHTTP   `mapstructure:"http"`
}

type serverConfigTrafficStats struct {
	Listen string `mapstructure:"listen"`
	Secret string `mapstructure:"secret"`
}

type serverConfigMasqueradeFile struct {
	Dir string `mapstructure:"dir"`
}

type serverConfigMasqueradeProxy struct {
	URL         string `mapstructure:"url"`
	RewriteHost bool   `mapstructure:"rewriteHost"`
}

type serverConfigMasqueradeString struct {
	Content    string            `mapstructure:"content"`
	Headers    map[string]string `mapstructure:"headers"`
	StatusCode int               `mapstructure:"statusCode"`
}

type serverConfigMasquerade struct {
	Type        string                       `mapstructure:"type"`
	File        serverConfigMasqueradeFile   `mapstructure:"file"`
	Proxy       serverConfigMasqueradeProxy  `mapstructure:"proxy"`
	String      serverConfigMasqueradeString `mapstructure:"string"`
	ListenHTTP  string                       `mapstructure:"listenHTTP"`
	ListenHTTPS string                       `mapstructure:"listenHTTPS"`
	ForceHTTPS  bool                         `mapstructure:"forceHTTPS"`
}

func checkAndInitProxyConfig(xrayOrV2rayConfig, hysteriaConfig string) error {
	var err error = nil
	if rErr := initXrayOrV2rayConfig(xrayOrV2rayConfig); rErr != nil {
		common.MergeError(err, rErr)
	}
	if rErr := initHysteriaConfig(hysteriaConfig); rErr != nil {
		common.MergeError(err, rErr)
	}
	return err
}

func initXrayOrV2rayConfig(configFile string) error {
	// 如果文件不存在, 则需要初始化
	if isFileExist(configFile) {
		return nil
	}

	templateVars := map[string]interface{}{
		common.TemplateXrayV2rayApiPort: common.DefaultXrayV2rayApiPort,
	}
	result, err := template.RenderXrayOrV2rayConfigTemplate(templateVars)
	if err != nil {
		return fmt.Errorf("render xray or v2ray config fail > err: %v", err)
	}
	return writeToFile(configFile, result)
}

func initHysteriaConfig(configFile string) error {
	// 如果文件不存在, 则需要初始化
	if isFileExist(configFile) {
		return nil
	}
	hysteriaAuthUrl := fmt.Sprintf("http://localhost:%d/authHysteria2", gc.GetInt(common.ConfigServerHttpPort))
	email := gc.GetString(common.ConfigCertEmail)
	domain := gc.GetString(common.ConfigProxyHost)
	// 自动初始化时限制必须使用acme, 不支持自定义证书
	if net.ParseIP(domain) != nil {
		return fmt.Errorf("auto init hysteria must set proxy host with domain which is resolved to the current machine")
	}
	templateVars := map[string]interface{}{
		common.TemplateHysteriaListen:                common.DefaultHysteriaListen,
		common.TemplateHysteriaBandwidthUp:           common.DefaultHysteriaBandwidthUp,
		common.TemplateHysteriaBandwidthDown:         common.DefaultHysteriaBandwidthDown,
		common.TemplateHysteriaIgnoreClientBandwidth: common.DefaultIgnoreClientBandwidth,
		common.TemplateHysteriaAuthUrl:               hysteriaAuthUrl,
		common.TemplateHysteriaTrafficStatsListen:    common.DefaultHysteriaTrafficStatsListen,
		common.TemplateHysteriaTrafficStatsSecret:    uuid.New(),
		common.TemplateUseAcme:                       true,
		common.TemplateDomains:                       []string{domain},
		common.TemplateEmail:                         email,
	}
	result, err := template.RenderHysteriaConfigTemplate(templateVars)
	if err != nil {
		return fmt.Errorf("render hysteria config fail > err: %v", err)
	}
	return writeToFile(configFile, result)
}

func writeToFile(configFile, info string) error {
	if err := mkdir(configFile); err != nil {
		return err
	}
	f, err := os.Create(configFile)
	if err != nil {
		return fmt.Errorf("create fil[%s] fail > err: %v", configFile, err)
	}
	defer f.Close()
	if _, err := f.WriteString(info); err != nil {
		return fmt.Errorf("write to file[%s] fail > err: %v", configFile, err)
	}
	return nil
}

func mkdir(filePath string) error {
	// 获取文件所在目录
	dir := filepath.Dir(filePath)

	// 创建目录
	err := os.MkdirAll(dir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("create file[%s] dir fail > err: %v", filePath, err)
	}
	return nil
}

func isFileExist(file string) bool {
	// 判断文件是否存在
	_, err := os.Stat(file)
	if err != nil {
		return !os.IsNotExist(err)
	}
	return true
}
