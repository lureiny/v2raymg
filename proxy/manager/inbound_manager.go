package manager

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/lureiny/v2raymg/proxy/config"
)

const emptyTcpHeaderConfig = "{\n\"type\": \"none\"\n}"

type Inbound struct {
	Config  config.InboundDetourConfig
	Tag     string // 全局唯一
	RWMutex sync.RWMutex
}

// 返回InboundDetourConfig 进行json序列化后的二进制
func (inbound *Inbound) Encode() ([]byte, error) {
	return json.Marshal(inbound.Config)
}

// 根据rawByte初始化Inbound
func (inbound *Inbound) Init(rawString string) error {
	rawByte, err := base64.StdEncoding.DecodeString(rawString)
	if err != nil {
		return err
	}
	err = json.Unmarshal(rawByte, &inbound.Config)
	if err != nil {
		return err
	}
	inbound.Tag = inbound.Config.Tag
	if inbound.Tag == "" {
		return fmt.Errorf("inbound tag can't be empty")
	}
	inbound.CompleteInboundConfigInformation()
	return nil
}

func (inbound *Inbound) CompleteInboundConfigInformation() {
	if inbound.Config.StreamSetting == nil || inbound.Config.StreamSetting.TCPSettings == nil {
		return
	}
	b, err := inbound.Config.StreamSetting.TCPSettings.HeaderConfig.MarshalJSON()
	if err == nil {
		if string(b) == "null" {
			inbound.Config.StreamSetting.TCPSettings.HeaderConfig = []byte(emptyTcpHeaderConfig)
		}
	}
}

func (inbound *Inbound) GetUsers() []string {
	inbound.RWMutex.RLock()
	defer inbound.RWMutex.RUnlock()
	return GetInboundUsers(&inbound.Config)
}

// 根据协议创建新的inbound, 新的inbound仅Settings不一样, 配置有效性由proxy保证
func CopyNewInbound(srcInbound *Inbound, newProtocol, newTag string, newPort int) *Inbound {
	newInbound := &Inbound{
		Config:  srcInbound.Config,
		Tag:     newTag,
		RWMutex: sync.RWMutex{},
	}
	newInbound.Config.Tag = newTag
	newInbound.Config.PortRange = uint32(newPort)
	newInbound.Config.Protocol = newProtocol
	newInbound.Config.Settings = NewProtocolSetting(newProtocol)
	return newInbound
}

// 自身不保证并发安全, 需要通过ProxyManager保证并发安全
type InboundManager struct {
	inbounds       map[string]*Inbound
	listeningPorts map[uint32]bool // 统计已经监听的端口
}

func NewInboundManager() InboundManager {
	return InboundManager{
		inbounds:       map[string]*Inbound{},
		listeningPorts: map[uint32]bool{},
	}
}

// 添加inbound
func (inboundManager *InboundManager) Add(inbound *Inbound) error {
	if inbound.Tag == "" {
		return fmt.Errorf("inbound tag can't be empty")
	}

	if _, ok := inboundManager.listeningPorts[inbound.Config.PortRange]; ok {
		return fmt.Errorf("port(%d) is used", inbound.Config.PortRange)
	}

	if _, ok := inboundManager.inbounds[inbound.Tag]; ok {
		return fmt.Errorf("repeat add inbound with tag(%s)", inbound.Tag)
	}

	// 并发安全由外部保证
	inboundManager.inbounds[inbound.Tag] = inbound
	inboundManager.listeningPorts[inbound.Config.PortRange] = true
	return nil
}

// 根据tag删除指定的inbound
func (inboundManager *InboundManager) Delete(tag string) error {
	if inbound, ok := inboundManager.inbounds[tag]; !ok {
		return fmt.Errorf("inbound with tag(%s) is not exist", tag)
	} else {
		delete(inboundManager.inbounds, tag)
		delete(inboundManager.listeningPorts, inbound.Config.PortRange)
	}
	return nil
}

// 根据tag获取inbound, 不存在返回nil
func (inboundManager *InboundManager) Get(tag string) *Inbound {
	return inboundManager.inbounds[tag]
}

// 根据tag获取inbound, 不存在返回nil
func (inboundManager *InboundManager) AddVlessTCPTLS(tag string) *Inbound {
	return inboundManager.inbounds[tag]
}