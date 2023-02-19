package manager

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/lego"
	"github.com/lureiny/v2raymg/proxy/config"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

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
	proxyServer    *ProxyServer
	adaptive       Adaptive
	adaptiveMutex  sync.Mutex // 操作自适应变更时的锁
	certManager    *lego.CertManager
}

var proxyManager = &ProxyManager{
	rwmutex:        sync.RWMutex{},
	InboundManager: NewInboundManager(),
	adaptiveMutex:  sync.Mutex{},
	adaptive: Adaptive{
		Tags:  map[string]bool{},
		Ports: map[int64]bool{},
	},
}

func GetProxyManager() *ProxyManager {
	return proxyManager
}

func (proxyManager *ProxyManager) Init(configFile, version string, cm *lego.CertManager) error {
	proxyManager.certManager = cm
	proxyManager.ConfigFile = configFile
	err := proxyManager.LoadConfig()
	if err != nil {
		return err
	}

	proxyManager.proxyServer = NewProxyServer(configFile, version)
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

// 根据tag获取inbound, 不存在返回nil
func (proxyManager *ProxyManager) GetInbound(tag string) *Inbound {
	proxyManager.rwmutex.RLock()
	defer proxyManager.rwmutex.RUnlock()

	return proxyManager.InboundManager.Get(tag)
}

// 从指定的配置文件中加载config
func (proxyManager *ProxyManager) LoadConfig() error {
	proxyManager.rwmutex.Lock()
	defer proxyManager.rwmutex.Unlock()

	content, err := ioutil.ReadFile(proxyManager.ConfigFile)
	if err != nil {
		return err
	}

	err = json.Unmarshal(content, &proxyManager.Config)
	if err != nil {
		return err
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

// cycle 刷新周期  单位 秒/s
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
		log.Printf("Add user to config file, user: %v", user)
	}
	return err
}

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
		log.Printf("Remove User from runtime: [Email] %s from [Bound] %s", user.Email, user.Tag)
	}
	return err
}

func (proxyManager *ProxyManager) ResetUser(user *User) error {
	err := proxyManager.RemoveUser(user)
	if err != nil {
		return err
	}

	return proxyManager.AddUser(user)
}

func (proxyManager *ProxyManager) QueryStats(pattern string, reset bool) (*map[string]*proto.Stats, error) {
	return QueryStats(pattern, proxyManager.RuntimeConfig.Host, proxyManager.RuntimeConfig.Port, reset)
}

// 搬迁inbound, 适用于修改端口的场景
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

// 复制inbound, 适用于快速创建相同inbound, 可选是否复制用户
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

// 获取proxy中用户tag情况
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

// 获取全部tag
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
			log.Printf("get upstream inbound err > %v\n", err)
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
			log.Printf("get upstream inbound err > %v\n", err)
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

func (proxyManager *ProxyManager) StartProxyServer() error {
	return proxyManager.proxyServer.Start()
}

func (proxyManager *ProxyManager) StopProxyServer() {
	proxyManager.proxyServer.Stop()
}

func (proxyManager *ProxyManager) ReStartProxyServer() error {
	proxyManager.proxyServer.Stop()
	return proxyManager.proxyServer.Start()
}

func (proxyManager *ProxyManager) UpdateProxyServer(tag string) error {
	return proxyManager.proxyServer.Update(tag)
}

func (proxyManager *ProxyManager) GetProxyServerVersion() string {
	return proxyManager.proxyServer.currentVersion
}

// 添加port用于自动更换
func (proxyManager *ProxyManager) AddAdaptivePort(port interface{}) error {
	return proxyManager.adaptive.AddPort(port)
}

func (proxyManager *ProxyManager) DeleteAdaptivePort(port int64) {
	proxyManager.adaptive.DeletePort(port)
}

// 添加需要自适应的tag, 返回添加新的接口后adaptive配置的字节组
func (proxyManager *ProxyManager) AddAdaptiveTag(tag string) error {
	if inbound := proxyManager.GetInbound(tag); inbound == nil {
		return fmt.Errorf("inbound with tag(%s) not found", tag)
	}
	proxyManager.adaptive.AddTag(tag)
	return nil
}

func (proxyManager *ProxyManager) DeleteAdaptiveTag(tag string) {
	proxyManager.adaptive.DeleteTag(tag)
}

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

// 自动更新端口的线程
func (proxyManager *ProxyManager) CycleAdaptive() {
	cycleFunc := func() {
		for tag := range proxyManager.adaptive.Tags {
			oldPort, newPort, err := proxyManager.AdaptiveOneInbound(tag)
			log.Printf("INFO|Func=CycleAdaptive|Tag=%s|OldPort=%d|NewPort=%d|Err=%v\n", tag, oldPort, newPort, err)
		}
	}
	if _, err := proxyManager.adaptive.Cron.AddFunc(proxyManager.adaptive.CronRule, cycleFunc); err != nil {
		log.Printf("Err=add adaptive cycle func err > %v, use default cronrule\n", err)
	} else {
		proxyManager.adaptive.CronRule = defalutCron
		proxyManager.adaptive.Cron.AddFunc(proxyManager.adaptive.CronRule, cycleFunc)
	}
	proxyManager.adaptive.Cron.Start()
}
