package server

import (
	"context"
	"fmt"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureContainer records the params passed to FastAddInbound.
// kind is optional — when empty, Type() returns ContainerXray for backward
// compat with existing tests. Set kind to test other container types.
type captureContainer struct {
	lastTag    string
	lastParams map[string]any
	kind       contracts.ContainerType
	inbounds   []inbound.Inbound
}

func (c *captureContainer) Init(any) error                                                  { return nil }
func (c *captureContainer) Start() error                                                     { return nil }
func (c *captureContainer) Stop() error                                                      { return nil }
func (c *captureContainer) Restart() error                                                   { return nil }
func (c *captureContainer) Reload() error                                                    { return nil }
func (c *captureContainer) IsRunning() bool                                                  { return true }
func (c *captureContainer) Version() string                                                  { return "test" }
func (c *captureContainer) Type() container.ContainerType {
	if c.kind == "" {
		return contracts.ContainerXray
	}
	return c.kind
}
func (c *captureContainer) ConfigFile() string                                               { return "" }
func (c *captureContainer) Update(context.Context, container.UpdateRequest) (*container.UpdateResult, error) {
	return nil, nil
}
func (c *captureContainer) GetRunFunc() (func() error, func() error)                        { return nil, nil }
func (c *captureContainer) RemoveInboundConfig(string) error                                 { return nil }
func (c *captureContainer) GetInboundConfig(string) (inbound.Inbound, error)                 { return nil, nil }
func (c *captureContainer) ListInboundConfigs() []inbound.Inbound                            { return c.inbounds }
func (c *captureContainer) UserEventChannel() <-chan usermanager.UserEvent                   { return nil }
func (c *captureContainer) GetUserSubscriptions(contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	return nil, nil
}
func (c *captureContainer) FastAddInbound(tag string, params map[string]any) error {
	c.lastTag = tag
	c.lastParams = params
	return nil
}

// noopCertMgr satisfies the CertManager interface for testing.
type noopCertMgr struct{}

func (n *noopCertMgr) ObtainNewCert(string) error                { return nil }
func (n *noopCertMgr) AddCertificates(string, []byte, []byte) error { return nil }
func (n *noopCertMgr) GetCertInfo(string) interface{}            { return nil }
func (n *noopCertMgr) DeleteCert(string) error                   { return nil }
func (n *noopCertMgr) GetAllCert() []*proto.Cert                 { return nil }

func newFastAddTestServer(cc *captureContainer) *EndNodeServer {
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	// Inject captureContainer into the manager's internal map (same package access)
	mgr.SetForTest(contracts.ContainerXray, cc)
	return &EndNodeServer{
		containerMgr: mgr,
		certManager:  &noopCertMgr{},
	}
}

// TestFastAddInbound_AllCombinations verifies all 14 protocol combinations
// correctly pass parameters from proto through RPC server to the Executor.
func TestFastAddInbound_AllCombinations(t *testing.T) {
	cases := []struct {
		name     string
		req      *proto.FastAddInboundReq
		wantKeys map[string]any
	}{
		{
			name: "vless-tcp-tls",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
				Tag: "vless-tcp", Port: 10001, Security: "tls", Transport: "tcp",
				ExtraParams: map[string]string{"self_signed": "true"},
			},
			wantKeys: map[string]any{"protocol": "vless", "transport": "tcp", "security": "tls", "self_signed": true},
		},
		{
			name: "vless-tcp-reality",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
				Tag: "vless-reality", Port: 10002, Security: "reality", Transport: "tcp",
				Domain: "www.microsoft.com", RealityTarget: "www.microsoft.com:443",
				RealityServerNames: []string{"www.microsoft.com"}, RealityShortIds: []string{"0123456789abcdef"},
			},
			wantKeys: map[string]any{"protocol": "vless", "transport": "tcp", "security": "reality", "reality_target": "www.microsoft.com:443"},
		},
		{
			name: "vless-ws-tls",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
				Tag: "vless-ws", Port: 10003, Security: "tls", Transport: "ws",
				ExtraParams: map[string]string{"ws_path": "/wstest", "self_signed": "true"},
			},
			wantKeys: map[string]any{"protocol": "vless", "transport": "ws", "ws_path": "/wstest"},
		},
		{
			name: "vless-grpc-tls",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
				Tag: "vless-grpc", Port: 10004, Security: "tls", Transport: "grpc",
				ExtraParams: map[string]string{"grpc_service_name": "mygrpc", "alpn": "h2", "self_signed": "true"},
			},
			wantKeys: map[string]any{"protocol": "vless", "transport": "grpc", "grpc_service_name": "mygrpc", "alpn": []string{"h2"}},
		},
		{
			name: "vless-grpc-reality",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
				Tag: "vless-grpc-r", Port: 10005, Security: "reality", Transport: "grpc",
				Domain: "www.microsoft.com", RealityTarget: "www.microsoft.com:443",
				RealityServerNames: []string{"www.microsoft.com"},
				ExtraParams: map[string]string{"grpc_service_name": "mygrpc"},
			},
			wantKeys: map[string]any{"protocol": "vless", "transport": "grpc", "grpc_service_name": "mygrpc", "security": "reality"},
		},
		{
			name: "vless-httpupgrade-tls",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
				Tag: "vless-hu", Port: 10006, Security: "tls", Transport: "httpupgrade",
				ExtraParams: map[string]string{"httpupgrade_path": "/upgrade", "alpn": "http/1.1", "self_signed": "true"},
			},
			wantKeys: map[string]any{"protocol": "vless", "transport": "httpupgrade", "httpupgrade_path": "/upgrade", "alpn": []string{"http/1.1"}},
		},
		{
			name: "vless-xhttp-tls",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
				Tag: "vless-xhttp", Port: 10007, Security: "tls", Transport: "xhttp",
				ExtraParams: map[string]string{"xhttp_path": "/xhttp", "xhttp_mode": "auto", "self_signed": "true"},
			},
			wantKeys: map[string]any{"protocol": "vless", "transport": "xhttp", "xhttp_path": "/xhttp", "xhttp_mode": "auto"},
		},
		{
			name: "vless-xhttp-reality",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
				Tag: "vless-xhttp-r", Port: 10008, Security: "reality", Transport: "xhttp",
				Domain: "www.microsoft.com", RealityTarget: "www.microsoft.com:443",
				RealityServerNames: []string{"www.microsoft.com"},
				ExtraParams: map[string]string{"xhttp_path": "/xhttp"},
			},
			wantKeys: map[string]any{"protocol": "vless", "transport": "xhttp", "security": "reality", "xhttp_path": "/xhttp"},
		},
		{
			name: "vless-splithttp-tls",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
				Tag: "vless-split", Port: 10009, Security: "tls", Transport: "splithttp",
				ExtraParams: map[string]string{"xhttp_path": "/split", "self_signed": "true"},
			},
			wantKeys: map[string]any{"protocol": "vless", "transport": "splithttp", "xhttp_path": "/split"},
		},
		{
			name: "vless-splithttp-reality",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
				Tag: "vless-split-r", Port: 10010, Security: "reality", Transport: "splithttp",
				Domain: "www.microsoft.com", RealityTarget: "www.microsoft.com:443",
				RealityServerNames: []string{"www.microsoft.com"},
				ExtraParams: map[string]string{"xhttp_path": "/split"},
			},
			wantKeys: map[string]any{"protocol": "vless", "transport": "splithttp", "security": "reality"},
		},
		{
			name: "vmess-tcp-tls",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VMESSSettingBuilderType,
				Tag: "vmess-tcp", Port: 10011, Security: "tls", Transport: "tcp",
				ExtraParams: map[string]string{"self_signed": "true"},
			},
			wantKeys: map[string]any{"protocol": "vmess", "transport": "tcp", "security": "tls"},
		},
		{
			name: "vmess-ws-tls",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VMESSSettingBuilderType,
				Tag: "vmess-ws", Port: 10012, Security: "tls", Transport: "ws",
				ExtraParams: map[string]string{"ws_path": "/vmessws", "self_signed": "true"},
			},
			wantKeys: map[string]any{"protocol": "vmess", "transport": "ws", "ws_path": "/vmessws"},
		},
		{
			name: "trojan-tcp-tls",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_TrojanSettingBuilderType,
				Tag: "trojan-tcp", Port: 10013, Security: "tls", Transport: "tcp",
				ExtraParams: map[string]string{"self_signed": "true"},
			},
			wantKeys: map[string]any{"protocol": "trojan", "transport": "tcp", "security": "tls"},
		},
		{
			name: "ss-tcp-none",
			req: &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_SSSettingBuilderType,
				Tag: "ss-tcp", Port: 10014, Security: "none", Transport: "tcp",
				ExtraParams: map[string]string{"method": "2022-blake3-aes-256-gcm"},
			},
			wantKeys: map[string]any{"protocol": "shadowsocks", "transport": "tcp", "method": "2022-blake3-aes-256-gcm"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cc := &captureContainer{}
			srv := newFastAddTestServer(cc)

			rsp, err := srv.FastAddInbound(context.Background(), tc.req)
			require.NoError(t, err)
			require.Equal(t, int32(0), rsp.Code, "RPC error: %s", rsp.Msg)
			require.NotNil(t, cc.lastParams, "FastAddInbound was not called on container")

			for k, want := range tc.wantKeys {
				got, exists := cc.lastParams[k]
				assert.True(t, exists, "param %q missing from params", k)
				assert.Equal(t, want, got, "param %q value mismatch", k)
			}
		})
	}
}

// TestFastAddInbound_EmptyDomainNotBlocked verifies that empty domain
// doesn't trigger cert acquisition error.
func TestFastAddInbound_EmptyDomainNotBlocked(t *testing.T) {
	cc := &captureContainer{}
	srv := newFastAddTestServer(cc)

	rsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		Tag: "test-no-domain", Port: 9999, Security: "tls", Transport: "tcp",
		ExtraParams: map[string]string{"self_signed": "true"},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code, "empty domain should not fail: %s", rsp.Msg)
	assert.NotNil(t, cc.lastParams)
	assert.Equal(t, true, cc.lastParams["self_signed"])
}

// TestFastAddInbound_LegacyStreamFallback verifies legacy StreamBuilderType
// still works when Transport field is empty.
func TestFastAddInbound_LegacyStreamFallback(t *testing.T) {
	cc := &captureContainer{}
	srv := newFastAddTestServer(cc)

	rsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		StreamBuilderType:  proto.BuilderType_WSBuilderType, // legacy enum
		// Transport NOT set — should fallback to "ws"
		Tag: "test-legacy", Port: 9998, Security: "tls",
		ExtraParams: map[string]string{"self_signed": "true"},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code, "error: %s", rsp.Msg)
	assert.Equal(t, "ws", cc.lastParams["transport"])
}

// TestFastAddInbound_XHTTPHostPassedAsSlice verifies xhttp_host arrives as []string
// (not string), matching what profilegen/adapter expect.
func TestFastAddInbound_XHTTPHostPassedAsSlice(t *testing.T) {
	cc := &captureContainer{}
	srv := newFastAddTestServer(cc)

	rsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		Tag: "test-xhttp-host", Port: 9997, Security: "tls", Transport: "xhttp",
		ExtraParams: map[string]string{
			"xhttp_host": "cdn.example.com",
			"self_signed": "true",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code, "error: %s", rsp.Msg)

	// Must arrive as []string, not string
	got, ok := cc.lastParams["xhttp_host"]
	require.True(t, ok, "xhttp_host missing")
	gotSlice, ok := got.([]string)
	require.True(t, ok, "xhttp_host should be []string, got %T", got)
	assert.Equal(t, []string{"cdn.example.com"}, gotSlice)
}

// TestFastAddInbound_RealityWithDomainNoCertTrigger verifies that security=reality
// with a non-empty domain does NOT trigger cert acquisition.
func TestFastAddInbound_RealityWithDomainNoCertTrigger(t *testing.T) {
	// Use a cert manager that fails on any ObtainNewCert call
	failCertMgr := &failingCertMgr{}
	cc := &captureContainer{}
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, cc)
	srv := &EndNodeServer{
		containerMgr: mgr,
		certManager:  failCertMgr,
	}

	rsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		Tag: "test-reality-domain", Port: 9996, Security: "reality", Transport: "tcp",
		Domain: "www.microsoft.com",
		RealityTarget: "www.microsoft.com:443",
		RealityServerNames: []string{"www.microsoft.com"},
	})
	require.NoError(t, err)
	// Should succeed — cert manager should NOT be called for reality
	assert.Equal(t, int32(0), rsp.Code, "reality+domain should not trigger cert: %s", rsp.Msg)
	assert.NotNil(t, cc.lastParams)
}

// failingCertMgr fails on any ObtainNewCert call to detect unwanted cert triggers.
type failingCertMgr struct{ noopCertMgr }

func (f *failingCertMgr) ObtainNewCert(domain string) error {
	return fmt.Errorf("UNEXPECTED cert acquisition for domain %q", domain)
}
