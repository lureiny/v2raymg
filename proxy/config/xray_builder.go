//go:build !v2ray

package config

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/lego"
	"github.com/lureiny/v2raymg/server/rpc/proto"
	"github.com/xtls/xray-core/infra/conf"
)

const (
	chacha20     = "chacha20-poly1395"
	SecurityXTLS = "xtls"
	SecurityTLS  = "tls"
)

var inboundSettingBuilders = map[proto.BuilderType]*InboundSettingBuilderWithMutex{}
var streamSettingBuilders = map[proto.BuilderType]*StreamSettingBuilderWithMutex{}

func GetInboundSettingBuilder(builderType proto.BuilderType) *InboundSettingBuilderWithMutex {
	return inboundSettingBuilders[builderType]
}

func GetStreamSettingBuilder(builderType proto.BuilderType) *StreamSettingBuilderWithMutex {
	return streamSettingBuilders[builderType]
}

type InboundSettingBuilder interface {
	Build() *json.RawMessage
	GetProtocol() string
}

type InboundSettingBuilderWithMutex struct {
	InboundSettingBuilder
	Mutex *sync.Mutex
}

type VlessSettingBuilder struct{}

func (v *VlessSettingBuilder) Build() *json.RawMessage {
	vlessSettingConfig := &VLessInboundConfig{}
	vlessSettingConfig.Clients = []json.RawMessage{}
	vlessSettingConfig.Decryption = "none"
	data, _ := json.MarshalIndent(vlessSettingConfig, "", "    ")
	return (*json.RawMessage)(&data)
}

func (v *VlessSettingBuilder) GetProtocol() string {
	return "vless"
}

type VmessSettingBuilder struct{}

func (v *VmessSettingBuilder) Build() *json.RawMessage {
	vmessSettingConfig := &conf.VMessInboundConfig{}
	vmessSettingConfig.Users = []json.RawMessage{}
	data, _ := json.MarshalIndent(vmessSettingConfig, "", "    ")
	return (*json.RawMessage)(&data)
}

func (v *VmessSettingBuilder) GetProtocol() string {
	return "vmess"
}

type TrojanSettingBuilder struct{}

func (t *TrojanSettingBuilder) Build() *json.RawMessage {
	trojanSettingConfig := &conf.TrojanServerConfig{}
	trojanSettingConfig.Clients = []*conf.TrojanUserConfig{}
	data, _ := json.MarshalIndent(trojanSettingConfig, "", "    ")
	return (*json.RawMessage)(&data)
}

func (v *TrojanSettingBuilder) GetProtocol() string {
	return "trojan"
}

type StreamSettingBuilder interface {
	Init(string, *lego.CertManager, bool)
	Build() *conf.StreamConfig
}

type StreamSettingBuilderWithMutex struct {
	StreamSettingBuilder
	Mutex *sync.Mutex
}

func NewTLSConfig(domain string, certManager *lego.CertManager) *conf.TLSConfig {
	tlsConfig := &conf.TLSConfig{}
	tlsConfig.ServerName = domain
	tlsConfig.Certs = make([]*conf.TLSCertConfig, 0)
	certificate := certManager.GetCert(domain)
	if certificate != nil {
		tlsConfig.Certs = append(tlsConfig.Certs, &conf.TLSCertConfig{
			CertFile: certificate.CertificateFile,
			KeyFile:  certificate.KeyFile,
		})
	}
	return tlsConfig
}

func NewXTLSConfig(domain string, certManager *lego.CertManager) *conf.XTLSConfig {
	xtlsConfig := &conf.XTLSConfig{}
	xtlsConfig.ServerName = domain
	xtlsConfig.Certs = make([]*conf.XTLSCertConfig, 0)
	certificate := certManager.GetCert(domain)
	if certificate != nil {
		xtlsConfig.Certs = append(xtlsConfig.Certs, &conf.XTLSCertConfig{
			CertFile: certificate.CertificateFile,
			KeyFile:  certificate.KeyFile,
		})
	}
	return xtlsConfig
}

func NewRandomStringWithTime() string {
	buffer := make([]byte, 8)
	binary.BigEndian.PutUint64(buffer, uint64(time.Now().UnixNano()))
	data := md5.Sum(buffer)
	return fmt.Sprintf("%x", data)
}

func FullTlsXtlsConfig(streamConfig *conf.StreamConfig, domain string, certManager *lego.CertManager, isXtls bool) {
	if isXtls {
		streamConfig.Security = SecurityXTLS
		streamConfig.XTLSSettings = NewXTLSConfig(domain, certManager)
	} else {
		streamConfig.Security = SecurityTLS
		streamConfig.TLSSettings = NewTLSConfig(domain, certManager)
	}
}

type TCPBuilder struct {
	Domain      string
	CertManager *lego.CertManager
	IsXtls      bool
}

func (b *TCPBuilder) Build() *conf.StreamConfig {
	streamConfig := &conf.StreamConfig{}
	transportProtocol := (conf.TransportProtocol)("tcp")
	streamConfig.Network = &transportProtocol
	streamConfig.TCPSettings = nil
	FullTlsXtlsConfig(streamConfig, b.Domain, b.CertManager, b.IsXtls)
	return streamConfig
}

func (b *TCPBuilder) Init(domain string, c *lego.CertManager, isXtls bool) {
	b.Domain = domain
	b.CertManager = c
	b.IsXtls = isXtls
}

type WSBuilder struct {
	Domain      string
	CertManager *lego.CertManager
	IsXtls      bool
}

func NewWebSocketHeaders() map[string]string {
	return map[string]string{
		"Host":                    NewRandomStringWithTime(),
		NewRandomStringWithTime(): NewRandomStringWithTime(),
	}
}

func (b *WSBuilder) Build() *conf.StreamConfig {
	streamConfig := &conf.StreamConfig{}
	transportProtocol := (conf.TransportProtocol)("ws")
	streamConfig.Network = &transportProtocol
	streamConfig.WSSettings = &conf.WebSocketConfig{
		Path:    "/" + NewRandomStringWithTime(),
		Headers: NewWebSocketHeaders(),
	}
	FullTlsXtlsConfig(streamConfig, b.Domain, b.CertManager, b.IsXtls)
	return streamConfig
}

func (b *WSBuilder) Init(domain string, c *lego.CertManager, isXtls bool) {
	b.Domain = domain
	b.CertManager = c
	b.IsXtls = isXtls
}

func NewFakeHeader() json.RawMessage {
	return []byte(`{"type": "wechat-video"}`)
}

type QuicBuilder struct {
	Domain      string
	CertManager *lego.CertManager
	IsXtls      bool
}

func (b *QuicBuilder) Build() *conf.StreamConfig {
	streamConfig := &conf.StreamConfig{}
	transportProtocol := (conf.TransportProtocol)("quic")
	streamConfig.Network = &transportProtocol
	streamConfig.QUICSettings = &conf.QUICConfig{
		Security: chacha20,
		Key:      NewRandomStringWithTime(),
		Header:   NewFakeHeader(),
	}
	FullTlsXtlsConfig(streamConfig, b.Domain, b.CertManager, b.IsXtls)
	return streamConfig
}

func (b *QuicBuilder) Init(domain string, c *lego.CertManager, isXtls bool) {
	b.Domain = domain
	b.CertManager = c
	b.IsXtls = isXtls
}

type MkcpBuilder struct {
	Domain      string
	CertManager *lego.CertManager
	IsXtls      bool
}

func (b *MkcpBuilder) Build() *conf.StreamConfig {
	streamConfig := &conf.StreamConfig{}
	transportProtocol := (conf.TransportProtocol)("mkcp")
	streamConfig.Network = &transportProtocol
	congestion := true
	seed := NewRandomStringWithTime()
	streamConfig.KCPSettings = &conf.KCPConfig{
		Congestion:   &congestion,
		HeaderConfig: NewFakeHeader(),
		Seed:         &seed,
	}
	FullTlsXtlsConfig(streamConfig, b.Domain, b.CertManager, b.IsXtls)
	return streamConfig
}

func (b *MkcpBuilder) Init(domain string, c *lego.CertManager, isXtls bool) {
	b.Domain = domain
	b.CertManager = c
	b.IsXtls = isXtls
}

type GrpcBuilder struct {
	Domain      string
	CertManager *lego.CertManager
	IsXtls      bool
}

func (b *GrpcBuilder) Build() *conf.StreamConfig {
	streamConfig := &conf.StreamConfig{}
	transportProtocol := (conf.TransportProtocol)("grpc")
	streamConfig.Network = &transportProtocol
	streamConfig.GRPCConfig = &conf.GRPCConfig{
		ServiceName: NewRandomStringWithTime(),
	}
	FullTlsXtlsConfig(streamConfig, b.Domain, b.CertManager, b.IsXtls)
	return streamConfig
}

func (b *GrpcBuilder) Init(domain string, c *lego.CertManager, isXtls bool) {
	b.Domain = domain
	b.CertManager = c
	b.IsXtls = isXtls
}

type HttpBuilder struct {
	Domain      string
	CertManager *lego.CertManager
	IsXtls      bool
}

func (b *HttpBuilder) Build() *conf.StreamConfig {
	streamConfig := &conf.StreamConfig{}
	transportProtocol := (conf.TransportProtocol)("http")
	streamConfig.Network = &transportProtocol
	hosts := []string{
		fmt.Sprintf("%s.%s", NewRandomStringWithTime(), NewRandomStringWithTime()),
	}
	streamConfig.HTTPSettings = &conf.HTTPConfig{
		Path: "/" + NewRandomStringWithTime(),
		Host: (*conf.StringList)(&hosts),
	}
	FullTlsXtlsConfig(streamConfig, b.Domain, b.CertManager, b.IsXtls)
	return streamConfig
}

func (b *HttpBuilder) Init(domain string, c *lego.CertManager, isXtls bool) {
	b.Domain = domain
	b.CertManager = c
	b.IsXtls = isXtls
}

func init() {
	inboundSettingBuilders[proto.BuilderType_VLESSSettingBuilderType] = &InboundSettingBuilderWithMutex{
		InboundSettingBuilder: &VlessSettingBuilder{},
		Mutex:                 &sync.Mutex{},
	}
	inboundSettingBuilders[proto.BuilderType_VMESSSettingBuilderType] = &InboundSettingBuilderWithMutex{
		InboundSettingBuilder: &VmessSettingBuilder{},
		Mutex:                 &sync.Mutex{},
	}
	inboundSettingBuilders[proto.BuilderType_TrojanSettingBuilderType] = &InboundSettingBuilderWithMutex{
		InboundSettingBuilder: &TrojanSettingBuilder{},
		Mutex:                 &sync.Mutex{},
	}

	streamSettingBuilders[proto.BuilderType_TCPBuilderType] = &StreamSettingBuilderWithMutex{
		StreamSettingBuilder: &TCPBuilder{},
		Mutex:                &sync.Mutex{},
	}
	streamSettingBuilders[proto.BuilderType_WSBuilderType] = &StreamSettingBuilderWithMutex{
		StreamSettingBuilder: &WSBuilder{},
		Mutex:                &sync.Mutex{},
	}
	streamSettingBuilders[proto.BuilderType_QuicBuilderType] = &StreamSettingBuilderWithMutex{
		StreamSettingBuilder: &QuicBuilder{},
		Mutex:                &sync.Mutex{},
	}
	streamSettingBuilders[proto.BuilderType_MkcpBuilderType] = &StreamSettingBuilderWithMutex{
		StreamSettingBuilder: &MkcpBuilder{},
		Mutex:                &sync.Mutex{},
	}
	streamSettingBuilders[proto.BuilderType_GrpcBuilderType] = &StreamSettingBuilderWithMutex{
		StreamSettingBuilder: &GrpcBuilder{},
		Mutex:                &sync.Mutex{},
	}
	streamSettingBuilders[proto.BuilderType_HttpBuilderType] = &StreamSettingBuilderWithMutex{
		StreamSettingBuilder: &HttpBuilder{},
		Mutex:                &sync.Mutex{},
	}
}
