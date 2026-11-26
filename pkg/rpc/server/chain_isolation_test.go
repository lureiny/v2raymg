package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type certSpyMgr struct {
	obtainCalls []string
	certDir     string
}

func (c *certSpyMgr) ObtainNewCert(domain string) error {
	c.obtainCalls = append(c.obtainCalls, domain)
	return nil
}
func (c *certSpyMgr) AddCertificates(string, []byte, []byte) error { return nil }
func (c *certSpyMgr) GetCertInfo(string) interface{}               { return nil }
func (c *certSpyMgr) DeleteCert(string) error                      { return nil }
func (c *certSpyMgr) GetAllCert() []*proto.Cert                    { return nil }
func (c *certSpyMgr) GetCertFiles(domain string) (string, string, bool) {
	if c.certDir == "" || domain == "" {
		return "", "", false
	}
	certPath := filepath.Join(c.certDir, domain+".crt")
	keyPath := filepath.Join(c.certDir, domain+".key")
	_ = os.WriteFile(certPath, []byte("test-cert"), 0600)
	_ = os.WriteFile(keyPath, []byte("test-key"), 0600)
	return certPath, keyPath, true
}

type chainForwardMgrStub struct {
	forward.ForwardManager
	rules         []*forward.ForwardRule
	allocatedPort map[uint32]bool
}

func (f *chainForwardMgrStub) GetAllRules() []*forward.ForwardRule {
	return f.rules
}

func (f *chainForwardMgrStub) GetRulesByUser(username string) []*forward.ForwardRule {
	var out []*forward.ForwardRule
	for _, r := range f.rules {
		if r.Username == username {
			out = append(out, r)
		}
	}
	return out
}

func (f *chainForwardMgrStub) IsPortAllocated(port uint32) bool {
	if f.allocatedPort != nil && f.allocatedPort[port] {
		return true
	}
	for _, r := range f.rules {
		if r.ListenPort == port {
			return true
		}
	}
	return false
}

func (f *chainForwardMgrStub) Close() error { return nil }

type chainContainerStub struct {
	captureContainer
	supportsChain  bool
	validateURIErr error
	metas          map[string]contracts.InboundMeta
	setTagsCalls   []setTagsCall
	setTagsErr     error
	nativeAdded    [][]byte
	subSpecs       []contracts.SubscriptionSpec
}

type setTagsCall struct {
	tag  string
	tags []string
}

func (c *chainContainerStub) SupportsChainOutbound() bool { return c.supportsChain }

func (c *chainContainerStub) ValidateChainURI(rawURI string) error {
	return c.validateURIErr
}

func (c *chainContainerStub) GetInboundMeta(tag string) (contracts.InboundMeta, bool) {
	if c.metas == nil {
		return contracts.InboundMeta{}, false
	}
	m, ok := c.metas[tag]
	return m, ok
}

func (c *chainContainerStub) SetInboundAccessTags(tag string, accessTags []string) error {
	c.setTagsCalls = append(c.setTagsCalls, setTagsCall{
		tag:  tag,
		tags: append([]string(nil), accessTags...),
	})
	if c.setTagsErr != nil {
		return c.setTagsErr
	}
	if c.metas == nil {
		c.metas = make(map[string]contracts.InboundMeta)
	}
	meta := c.metas[tag]
	meta.AccessTags = append([]string(nil), accessTags...)
	c.metas[tag] = meta
	return nil
}

func (c *chainContainerStub) AddInboundNative(nativeInboundJSON []byte) error {
	c.nativeAdded = append(c.nativeAdded, append([]byte(nil), nativeInboundJSON...))
	return nil
}
func (c *chainContainerStub) RemoveInboundNative(string) error         { return nil }
func (c *chainContainerStub) AddUser(string, contracts.UserSpec) error { return nil }
func (c *chainContainerStub) RemoveUser(string, string) error          { return nil }
func (c *chainContainerStub) QueryStats(string, bool) (map[string]*contracts.Stats, error) {
	return nil, nil
}
func (c *chainContainerStub) GetUserSubscriptions(contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	return c.subSpecs, nil
}

func TestMethodRspMap_ContainsSetInboundAccessTags(t *testing.T) {
	rsp, err := newEmptyRsp("/proto.EndNodeAccess/SetInboundAccessTags")
	require.NoError(t, err)
	require.NotNil(t, rsp, "SetInboundAccessTags must be registered in methodRspMap")
	_, isInboundOp := rsp.(*proto.InboundOpRsp)
	assert.True(t, isInboundOp, "expected *proto.InboundOpRsp, got %T", rsp)
}

func TestFastAddInbound_PreCertValidationAndMetadata(t *testing.T) {
	makeServer := func(allowLoopback bool) (*EndNodeServer, *chainContainerStub, *chainContainerStub, *certSpyMgr) {
		xray := &chainContainerStub{
			captureContainer: captureContainer{
				kind:     contracts.ContainerXray,
				inbounds: []inbound.Inbound{&stubInbound{tag: "existing-xray"}},
			},
			supportsChain: true,
		}
		mihomo := &chainContainerStub{
			captureContainer: captureContainer{
				kind:     contracts.ContainerMihomo,
				inbounds: []inbound.Inbound{&stubInbound{tag: "existing-mihomo"}},
			},
			supportsChain: true,
		}
		snell := &chainContainerStub{
			captureContainer: captureContainer{kind: contracts.ContainerSnell},
			supportsChain:    false,
		}
		mgr := container.NewContainerMgr(nil, container.BuildOptions{})
		mgr.SetForTest(contracts.ContainerXray, xray)
		mgr.SetForTest(contracts.ContainerMihomo, mihomo)
		mgr.SetForTest(contracts.ContainerSnell, snell)

		fm := &chainForwardMgrStub{
			rules:         []*forward.ForwardRule{{ListenPort: 41001, Username: "alice", InboundTag: "existing-xray", ContainerType: contracts.ContainerXray}},
			allocatedPort: map[uint32]bool{42002: true},
		}
		um := usermanager.NewUserManager(fm, "node-1")
		certs := &certSpyMgr{certDir: t.TempDir()}
		srv := &EndNodeServer{
			cfg: appconfig.EndNodeConfig{
				NodeConfig: appconfig.NodeConfig{
					RpcPort:   50051,
					ProxyHost: "node1.example.com",
				},
				HttpPort:                 8080,
				AllowLoopbackChainTarget: allowLoopback,
			},
			containerMgr: mgr,
			certManager:  certs,
			userMgr:      um,
		}
		return srv, xray, mihomo, certs
	}

	t.Run("rejects invalid requests beforeObtainNewCert", func(t *testing.T) {
		cases := []struct {
			name    string
			req     *proto.FastAddInboundReq
			wantSub string
		}{
			{
				name: "chain without chain_uri",
				req: &proto.FastAddInboundReq{
					InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
					Domain:             "a.example.com",
					Security:           "tls",
					InboundType:        "chain",
				},
				wantSub: "chain_uri",
			},
			{
				name: "direct with chain_uri",
				req: &proto.FastAddInboundReq{
					InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
					Domain:             "b.example.com",
					Security:           "tls",
					InboundType:        "direct",
					ChainUri:           "socks5://1.2.3.4:1080",
				},
				wantSub: "must not specify chain_uri",
			},
			{
				name: "invalid inbound_type",
				req: &proto.FastAddInboundReq{
					InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
					Domain:             "c.example.com",
					Security:           "tls",
					InboundType:        "relay",
				},
				wantSub: `must be "direct" or "chain"`,
			},
			{
				name: "invalid tag chars",
				req: &proto.FastAddInboundReq{
					InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
					Domain:             "d.example.com",
					Security:           "tls",
					Tag:                "bad tag!",
				},
				wantSub: "invalid tag",
			},
			{
				name: "cross-container tag collision",
				req: &proto.FastAddInboundReq{
					InboundBuilderType: proto.BuilderType_VMESSSettingBuilderType,
					ContainerType:      "mihomo",
					Domain:             "e.example.com",
					Security:           "tls",
					Tag:                "existing-xray",
				},
				wantSub: "already exists in container xray",
			},
			{
				name: "chain on non-chain-capable container",
				req: &proto.FastAddInboundReq{
					InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
					ContainerType:      "snell",
					Domain:             "f.example.com",
					Security:           "tls",
					InboundType:        "chain",
					ChainUri:           "socks5://1.2.3.4:1080",
				},
				wantSub: "does not support chain outbound",
			},
			{
				name: "chain with non-loopback extra listen",
				req: &proto.FastAddInboundReq{
					InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
					Domain:             "g.example.com",
					Security:           "tls",
					InboundType:        "chain",
					ChainUri:           "socks5://1.2.3.4:1080",
					ExtraParams:        map[string]string{"listen": "0.0.0.0"},
				},
				wantSub: "127.0.0.1",
			},
			{
				name: "chain loopback blocked when AllowLoopbackChainTarget=false",
				req: &proto.FastAddInboundReq{
					InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
					Domain:             "h.example.com",
					Security:           "tls",
					InboundType:        "chain",
					ChainUri:           "socks5://user:secret@127.0.0.1:7890",
				},
				wantSub: "loopback",
			},
			{
				name: "chain ProxyHost on reserved port blocked",
				req: &proto.FastAddInboundReq{
					InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
					Domain:             "i.example.com",
					Security:           "tls",
					InboundType:        "chain",
					ChainUri:           "socks5://node1.example.com:8080",
				},
				wantSub: "reserved or allocated port",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv, xray, mihomo, certs := makeServer(false)
				rsp, err := srv.FastAddInbound(context.Background(), tc.req)
				require.NoError(t, err)
				assert.Equal(t, int32(1020), rsp.Code)
				assert.Contains(t, rsp.Msg, tc.wantSub)
				assert.NotContains(t, rsp.Msg, "secret", "error message must redact chain password")
				assert.Empty(t, certs.obtainCalls, "ObtainNewCert must NOT be called when validation fails")
				assert.Empty(t, xray.lastTag)
				assert.Empty(t, mihomo.lastTag)
			})
		}
	})

	t.Run("AllowLoopback=true enforces reserved and allocated ports before cert", func(t *testing.T) {
		for _, port := range []int{8080, 50051, 62789, 9999, 9090, 41001, 42002} {
			srv, _, _, certs := makeServer(true)
			rsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
				InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
				Domain:             "port.example.com",
				Security:           "tls",
				InboundType:        "chain",
				ChainUri:           fmt.Sprintf("socks5://127.0.0.1:%d", port),
			})
			require.NoError(t, err)
			assert.Equal(t, int32(1020), rsp.Code, "port %d must be rejected", port)
			assert.Empty(t, certs.obtainCalls, "port %d rejection must happen before ObtainNewCert", port)
		}

		srv, xray, _, certs := makeServer(true)
		rsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
			InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
			Domain:             "ok.example.com",
			Security:           "tls",
			ChainUri:           "socks5://user:pass@127.0.0.1:7890#home-relay",
			AccessTags:         []string{" vip ", "hk", "vip", ""},
		})
		require.NoError(t, err)
		assert.Equal(t, int32(0), rsp.Code, "unexpected error: %s", rsp.Msg)
		assert.Equal(t, []string{"ok.example.com"}, certs.obtainCalls)
		assert.True(t, strings.HasPrefix(xray.lastTag, "vless-chain-"), "auto tag=%q", xray.lastTag)
		assert.Equal(t, 12+8, len(xray.lastTag), "expected vless-chain-<8hex>, got %q", xray.lastTag)
		assert.Equal(t, "chain", xray.lastParams["inbound_type"])
		assert.Equal(t, "socks5://user:pass@127.0.0.1:7890#home-relay", xray.lastParams["chain_uri"])
		assert.Equal(t, []string{"hk", "vip"}, xray.lastParams["access_tags"])
		assert.Equal(t, "home-relay", xray.lastParams["remark"])
		assert.Equal(t, "127.0.0.1", xray.lastParams["listen"])
	})

	t.Run("container ValidateChainURI rejection happens before ObtainNewCert", func(t *testing.T) {
		srv, _, mihomo, certs := makeServer(false)
		mihomo.validateURIErr = fmt.Errorf("mihomo unsupported scheme")
		rsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
			InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
			ContainerType:      "mihomo",
			Domain:             "mihomo.example.com",
			Security:           "tls",
			InboundType:        "chain",
			ChainUri:           "socks5://8.8.8.8:1080",
		})
		require.NoError(t, err)
		assert.Equal(t, int32(1020), rsp.Code)
		assert.Contains(t, rsp.Msg, "mihomo unsupported scheme")
		assert.Empty(t, certs.obtainCalls)
	})

	t.Run("local interface IP blocked by collectLocalHosts", func(t *testing.T) {
		ifaces, err := net.InterfaceAddrs()
		require.NoError(t, err)
		var nonLoopbackIP string
		for _, a := range ifaces {
			if ipNet, ok := a.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil && !ipNet.IP.IsUnspecified() {
				nonLoopbackIP = ipNet.IP.String()
				break
			}
		}
		if nonLoopbackIP == "" {
			t.Skip("no non-loopback IPv4 interface address found")
		}
		srv, _, _, certs := makeServer(true)
		rsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
			InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
			Domain:             "iface.example.com",
			Security:           "tls",
			InboundType:        "chain",
			ChainUri:           fmt.Sprintf("socks5://%s:8080", nonLoopbackIP),
		})
		require.NoError(t, err)
		assert.Equal(t, int32(1020), rsp.Code)
		assert.Contains(t, rsp.Msg, "reserved or allocated port")
		assert.Empty(t, certs.obtainCalls)
	})
}

func TestAddInbound_CrossContainerUniqueness(t *testing.T) {
	xray := &chainContainerStub{
		captureContainer: captureContainer{
			kind:     contracts.ContainerXray,
			inbounds: []inbound.Inbound{&stubInbound{tag: "xray-in"}},
		},
	}
	mihomo := &chainContainerStub{
		captureContainer: captureContainer{
			kind:     contracts.ContainerMihomo,
			inbounds: []inbound.Inbound{&stubInbound{tag: "mihomo-dup"}},
		},
	}
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, xray)
	mgr.SetForTest(contracts.ContainerMihomo, mihomo)
	srv := &EndNodeServer{containerMgr: mgr}

	dupJSON := base64.StdEncoding.EncodeToString([]byte(`{"tag":"mihomo-dup","port":10001,"protocol":"vless"}`))
	rsp, err := srv.AddInbound(context.Background(), &proto.InboundOpReq{InboundInfo: dupJSON})
	require.NoError(t, err)
	assert.Equal(t, int32(601), rsp.Code)
	assert.Contains(t, rsp.Msg, "already exists in container mihomo")
	assert.Empty(t, xray.nativeAdded)

	okJSON := base64.StdEncoding.EncodeToString([]byte(`{"tag":"xray-fresh","port":10002,"protocol":"vless"}`))
	rsp, err = srv.AddInbound(context.Background(), &proto.InboundOpReq{InboundInfo: okJSON})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code)
	assert.Len(t, xray.nativeAdded, 1)
}

func TestListInbound_EnrichesMetaAndRedactsChainURI(t *testing.T) {
	xray := &chainContainerStub{
		captureContainer: captureContainer{
			kind:     contracts.ContainerXray,
			inbounds: []inbound.Inbound{&stubInbound{tag: "chain-1"}, &stubInbound{tag: "direct-1"}},
		},
		metas: map[string]contracts.InboundMeta{
			"chain-1": {
				InboundType:      contracts.InboundTypeChain,
				AccessTags:       []string{"hk", "vip"},
				ChainOutboundURI: "socks5://alice:super-secret@203.0.113.10:1080#hk-home",
				Remark:           "HK Home",
			},
			"direct-1": {
				InboundType: contracts.InboundTypeDirect,
				AccessTags:  []string{"std"},
			},
		},
	}
	snell := &captureContainer{
		kind:     contracts.ContainerSnell,
		inbounds: []inbound.Inbound{&stubInbound{tag: "snell-default"}},
	}

	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, xray)
	mgr.SetForTest(contracts.ContainerSnell, snell)
	srv := &EndNodeServer{containerMgr: mgr}

	rsp, err := srv.ListInbound(context.Background(), &proto.ListInboundReq{})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code)
	require.Len(t, rsp.Inbounds, 3)

	// Sorted: snell:snell-default, xray:chain-1, xray:direct-1
	assert.Equal(t, "direct", rsp.Inbounds[0].GetInboundType())

	chainInfo := rsp.Inbounds[1]
	assert.Equal(t, "chain-1", chainInfo.GetName())
	assert.Equal(t, "chain", chainInfo.GetInboundType())
	assert.Equal(t, []string{"hk", "vip"}, chainInfo.GetAccessTags())
	assert.Equal(t, "HK Home", chainInfo.GetRemark())
	assert.Equal(t, "socks5", chainInfo.GetChainProtocol())
	assert.Equal(t, "203.0.113.10:1080", chainInfo.GetChainTarget())
	assert.NotContains(t, chainInfo.String(), "super-secret")
}

func TestSetInboundAccessTags(t *testing.T) {
	xray := &chainContainerStub{
		captureContainer: captureContainer{kind: contracts.ContainerXray},
	}
	plain := &captureContainer{kind: contracts.ContainerSnell}
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, xray)
	mgr.SetForTest(contracts.ContainerSnell, plain)
	srv := &EndNodeServer{containerMgr: mgr}

	// Missing container / name / invalid tag
	rsp, err := srv.SetInboundAccessTags(context.Background(), &proto.SetInboundAccessTagsReq{Name: "t1"})
	require.NoError(t, err)
	assert.NotEqual(t, int32(0), rsp.Code)

	rsp, err = srv.SetInboundAccessTags(context.Background(), &proto.SetInboundAccessTagsReq{Container: "xray"})
	require.NoError(t, err)
	assert.NotEqual(t, int32(0), rsp.Code)

	rsp, err = srv.SetInboundAccessTags(context.Background(), &proto.SetInboundAccessTagsReq{Container: "xray", Name: "t1", AccessTags: []string{"bad,tag"}})
	require.NoError(t, err)
	assert.NotEqual(t, int32(0), rsp.Code)

	// Container without InboundTagUpdater
	rsp, err = srv.SetInboundAccessTags(context.Background(), &proto.SetInboundAccessTagsReq{Container: "snell", Name: "snell-default", AccessTags: []string{"vip"}})
	require.NoError(t, err)
	assert.NotEqual(t, int32(0), rsp.Code)

	// Normalizes and forwards to InboundTagUpdater
	rsp, err = srv.SetInboundAccessTags(context.Background(), &proto.SetInboundAccessTagsReq{
		Container:  "xray",
		Name:       "t1",
		AccessTags: []string{" vip ", "hk", "vip", ""},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code)
	require.Len(t, xray.setTagsCalls, 1)
	assert.Equal(t, "t1", xray.setTagsCalls[0].tag)
	assert.Equal(t, []string{"hk", "vip"}, xray.setTagsCalls[0].tags)

	// Empty clears tags
	rsp, err = srv.SetInboundAccessTags(context.Background(), &proto.SetInboundAccessTagsReq{
		Container: "xray",
		Name:      "t1",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code)
	assert.Empty(t, xray.metas["t1"].AccessTags)
}

func TestGetContainers_IncludesContainerInfos(t *testing.T) {
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, &chainContainerStub{captureContainer: captureContainer{kind: contracts.ContainerXray}, supportsChain: true})
	mgr.SetForTest(contracts.ContainerSnell, &chainContainerStub{captureContainer: captureContainer{kind: contracts.ContainerSnell}, supportsChain: false})
	srv := &EndNodeServer{containerMgr: mgr}

	rsp, err := srv.GetContainers(context.Background(), &proto.GetContainersReq{})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code)
	assert.ElementsMatch(t, []string{"xray", "snell"}, rsp.GetContainers())
	byType := make(map[string]bool)
	for _, info := range rsp.GetContainerInfos() {
		byType[info.GetType()] = info.GetSupportsChain()
	}
	assert.Equal(t, map[string]bool{"xray": true, "snell": false}, byType)
}

func TestUserRPC_ChainIsolationAndProfileAndGetSub(t *testing.T) {
	ngs := newTestNGStore("default")
	fm := &chainForwardMgrStub{
		rules: []*forward.ForwardRule{
			{Username: "alice", InboundTag: "vless-1", ContainerType: contracts.ContainerXray, ListenPort: 10001},
			{Username: "hidden", InboundTag: "vless-1", ContainerType: contracts.ContainerXray, ListenPort: 10002},
		},
	}
	um := usermanager.NewUserManager(fm, "node-1")
	um.EnableClusterSync("default", ngs)

	subCh := um.Subscribe()
	defer um.Unsubscribe(subCh)

	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, &chainContainerStub{
		captureContainer: captureContainer{kind: contracts.ContainerXray},
		subSpecs:         nil, // empty filtered specs
	})
	subMgr := subscription.NewManager(mgr, um)

	srv := &EndNodeServer{
		userMgr:      um,
		containerMgr: mgr,
		subMgr:       subMgr,
	}

	// 1. AddUsers: atomic single event with Role, Group, InboundTags
	addRsp, err := srv.AddUsers(context.Background(), &proto.UserOpReq{
		Users: []*proto.User{
			{
				Name:        "alice",
				Passwd:      "pw1",
				Role:        "admin",
				Group:       "default",
				InboundTags: []string{" vip ", "hk", "vip"},
			},
			{
				Name:        "hidden",
				Passwd:      "pw2",
				Role:        "normal",
				Group:       "other-group",
				InboundTags: []string{"hk"},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), addRsp.Code, "msg=%s", addRsp.Msg)

	// Drain event channel and verify exactly 2 Add events (1 per user, no follow-up Role/Group update events)
	var events []usermanager.UserEvent
	for len(events) < 2 {
		select {
		case ev := <-subCh:
			events = append(events, ev)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for add events, got %d", len(events))
		}
	}
	select {
	case extra := <-subCh:
		t.Fatalf("expected single event per AddUser, got unexpected extra event %+v", extra)
	case <-time.After(30 * time.Millisecond):
	}
	assert.Equal(t, usermanager.UserEventAdd, events[0].Type)
	assert.Equal(t, "admin", events[0].User.Role)
	assert.Equal(t, "default", events[0].User.TargetGroup)
	assert.Equal(t, []string{"hk", "vip"}, events[0].User.InboundTags)

	// 2. UpdateUsers: UpdateInboundTags=false keeps tags intact; UpdateInboundTags=true replaces/clears
	updRsp, err := srv.UpdateUsers(context.Background(), &proto.UserOpReq{
		Users: []*proto.User{
			{Name: "alice", Role: "normal", UpdateInboundTags: false},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, updRsp.Msg)
	uAlice, err := um.GetUser("alice")
	require.NoError(t, err)
	assert.Equal(t, []string{"hk", "vip"}, uAlice.InboundTags)

	updRsp, err = srv.UpdateUsers(context.Background(), &proto.UserOpReq{
		Users: []*proto.User{
			{Name: "alice", UpdateInboundTags: true, InboundTags: []string{"jp"}},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, updRsp.Msg)
	uAlice, _ = um.GetUser("alice")
	assert.Equal(t, []string{"jp"}, uAlice.InboundTags)

	updRsp, err = srv.UpdateUsers(context.Background(), &proto.UserOpReq{
		Users: []*proto.User{
			{Name: "alice", UpdateInboundTags: true, InboundTags: nil},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, updRsp.Msg)
	uAlice, _ = um.GetUser("alice")
	assert.Empty(t, uAlice.InboundTags)

	// 3. GetProfile: hidden user returns basic fields with nil Inbounds; visible user gets Inbounds
	profHidden, err := srv.GetProfile(context.Background(), &proto.GetProfileReq{Username: "hidden"})
	require.NoError(t, err)
	assert.Equal(t, int32(0), profHidden.GetCode())
	assert.Equal(t, "hidden", profHidden.GetUsername())
	assert.Empty(t, profHidden.GetInbounds())

	profVisible, err := srv.GetProfile(context.Background(), &proto.GetProfileReq{Username: "alice"})
	require.NoError(t, err)
	assert.Equal(t, int32(0), profVisible.GetCode())
	require.Len(t, profVisible.GetInbounds(), 1)
	assert.Equal(t, "vless-1", profVisible.GetInbounds()[0].GetTag())

	// 4. GetSub: empty uris after filtering with nil error -> Code=0, Uris=nil
	subRsp, err := srv.GetSub(context.Background(), &proto.GetSubReq{
		User: &proto.User{Name: "alice"},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), subRsp.GetCode())
	assert.Empty(t, subRsp.GetMsg())
	assert.Nil(t, subRsp.GetUris())

	// 5. Cluster sync conversion roundtrip preserves InboundTags & sets UpdateInboundTags=true
	uHidden, _ := um.GetUser("hidden")
	syncProto := userToProtoClusterUserSync(uHidden)
	require.NotNil(t, syncProto.GetUser())
	assert.True(t, syncProto.GetUser().GetUpdateInboundTags())
	assert.Equal(t, []string{"hk"}, syncProto.GetUser().GetInboundTags())
	roundTrip := protoClusterUserSyncToUser(syncProto)
	assert.Equal(t, []string{"hk"}, roundTrip.InboundTags)
}

type stubInboundStore struct {
	getFn func(tag string) (*store.InboundRecord, error)
}

func (s *stubInboundStore) Load() ([]*store.InboundRecord, error) { return nil, nil }
func (s *stubInboundStore) Get(tag string) (*store.InboundRecord, error) {
	return s.getFn(tag)
}
func (s *stubInboundStore) Save(*store.InboundRecord) error { return nil }
func (s *stubInboundStore) Delete(string) error             { return nil }

func TestInboundTagUniqueness_PersistedStoreDisabledContainer(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "test.db")
	sm, err := store.NewStoreManager(dsn, migrations.All)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sm.Close() })

	// Persist an inbound belonging to hysteria while hysteria is NOT registered
	// in ContainerMgr (simulating a disabled container with a stale DB row).
	err = sm.InboundStore().Save(&store.InboundRecord{
		Tag:           "hy2-persisted",
		ContainerType: string(contracts.ContainerHysteria),
		CertSource:    "none",
		NativeJSON:    []byte(`{"tag":"hy2-persisted"}`),
	})
	require.NoError(t, err)

	xray := &chainContainerStub{
		captureContainer: captureContainer{kind: contracts.ContainerXray},
		supportsChain:    true,
	}
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, xray)
	certs := &certSpyMgr{certDir: t.TempDir()}

	srv := &EndNodeServer{
		containerMgr: mgr,
		certManager:  certs,
	}
	srv.SetInboundStore(sm.InboundStore())

	// Direct check finds persisted disabled-container tag and misses absent tag.
	ct, exists := srv.inboundTagExistsAnywhere("hy2-persisted")
	assert.True(t, exists)
	assert.Equal(t, contracts.ContainerHysteria, ct)

	ct, exists = srv.inboundTagExistsAnywhere("not-in-store")
	assert.False(t, exists)
	assert.Empty(t, ct)

	// FastAddInbound rejects duplicate tag targeting another container before cert acquisition.
	fastRsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		ContainerType:      "xray",
		Domain:             "dup.example.com",
		Security:           "tls",
		Tag:                "hy2-persisted",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1020), fastRsp.Code)
	assert.Contains(t, fastRsp.Msg, "already exists in container hysteria")
	assert.Empty(t, certs.obtainCalls, "ObtainNewCert must NOT be called when persisted tag collides")
	assert.Empty(t, xray.lastTag)

	// AddInbound also rejects duplicate persisted tag before touching xray runtime.
	dupJSON := base64.StdEncoding.EncodeToString([]byte(`{"tag":"hy2-persisted","port":10001,"protocol":"vless"}`))
	addRsp, err := srv.AddInbound(context.Background(), &proto.InboundOpReq{InboundInfo: dupJSON})
	require.NoError(t, err)
	assert.Equal(t, int32(601), addRsp.Code)
	assert.Contains(t, addRsp.Msg, "already exists in container hysteria")
	assert.Empty(t, xray.nativeAdded)

	// FastAddInbound succeeds for a fresh tag not present in ContainerMgr or InboundStore.
	okRsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		ContainerType:      "xray",
		Port:               10002,
		Security:           "none",
		Tag:                "xray-fresh",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), okRsp.Code, "unexpected error: %s", okRsp.Msg)
	assert.Equal(t, "xray-fresh", xray.lastTag)

	// containerMgr == nil still checks inboundStore safely without panicking.
	nilMgrSrv := &EndNodeServer{}
	nilMgrSrv.SetInboundStore(sm.InboundStore())
	ct, exists = nilMgrSrv.inboundTagExistsAnywhere("hy2-persisted")
	assert.True(t, exists)
	assert.Equal(t, contracts.ContainerHysteria, ct)

	ct, exists = nilMgrSrv.inboundTagExistsAnywhere("not-in-store")
	assert.False(t, exists)
	assert.Empty(t, ct)

	// Non-nil record paired with an error from store.Get is not treated as an existing tag.
	errSrv := &EndNodeServer{
		inboundStore: &stubInboundStore{
			getFn: func(string) (*store.InboundRecord, error) {
				return &store.InboundRecord{}, errors.New("db read failed")
			},
		},
	}
	ct, exists = errSrv.inboundTagExistsAnywhere("hy2-persisted")
	assert.False(t, exists)
	assert.Empty(t, ct)
}

func TestFastAddInbound_RealForwardManagerBlocksClaimedAndReservedPorts(t *testing.T) {
	fm, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort:  38000,
		MaxPort:  38099,
		Reserved: []uint32{54321, 19090},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = fm.Close() })

	// Claim a container port (not a forward rule) and also add a live forward rule.
	require.NoError(t, fm.AllocateSpecificPort(38050))
	t.Cleanup(func() { fm.ReleasePort(38050) })

	rule, err := fm.AddRule(forward.ForwardRule{
		Username:      "alice",
		ContainerType: contracts.ContainerXray,
		InboundTag:    "existing",
		TargetAddr:    "127.0.0.1:10001",
		Network:       "tcp",
	})
	require.NoError(t, err)

	xray := &chainContainerStub{
		captureContainer: captureContainer{
			kind:     contracts.ContainerXray,
			inbounds: []inbound.Inbound{inbound.NewDefaultInbound("existing", contracts.ProtocolVLess, 38051)},
		},
		supportsChain: true,
	}
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, xray)

	um := usermanager.NewUserManager(fm, "node-1")
	certs := &certSpyMgr{certDir: t.TempDir()}
	srv := &EndNodeServer{
		cfg: appconfig.EndNodeConfig{
			NodeConfig: appconfig.NodeConfig{
				RpcPort:   50051,
				ProxyHost: "node1.example.com",
			},
			HttpPort:                 8080,
			AllowLoopbackChainTarget: true,
		},
		containerMgr: mgr,
		certManager:  certs,
		userMgr:      um,
	}

	// Claimed container port (38050), live forward port, custom reserved management ports (54321, 19090),
	// and active container inbound port (38051) must all be rejected before cert acquisition.
	for _, port := range []uint32{38050, rule.ListenPort, 54321, 19090, 38051} {
		rsp, rpcErr := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
			InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
			Domain:             "blocked.example.com",
			Security:           "tls",
			InboundType:        "chain",
			ChainUri:           fmt.Sprintf("socks5://127.0.0.1:%d", port),
		})
		require.NoError(t, rpcErr)
		assert.Equal(t, int32(1020), rsp.Code, "port %d must be rejected by DefaultForwardManager/container policy", port)
		assert.Contains(t, rsp.Msg, "reserved or allocated port")
		assert.Empty(t, certs.obtainCalls, "port %d must be rejected before ObtainNewCert", port)
	}

	// Unallocated external loopback port succeeds.
	okRsp, rpcErr := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		Domain:             "ok.example.com",
		Security:           "tls",
		InboundType:        "chain",
		ChainUri:           "socks5://127.0.0.1:7890",
	})
	require.NoError(t, rpcErr)
	assert.Equal(t, int32(0), okRsp.Code, "unexpected error: %s", okRsp.Msg)
}

func TestFastAddInbound_ModeAReEnableAndListenAddrLock(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "modea.db")
	sm, err := store.NewStoreManager(dsn, migrations.All)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sm.Close() })

	// Seed disabled hysteria-default and snell-default, plus an enabled xray record.
	require.NoError(t, sm.InboundStore().Save(&store.InboundRecord{
		Tag:           "hysteria-default",
		ContainerType: string(contracts.ContainerHysteria),
		CertSource:    "none",
		NativeJSON:    []byte(`{"enabled":false,"port":9443}`),
		InboundType:   "direct",
	}))
	require.NoError(t, sm.InboundStore().Save(&store.InboundRecord{
		Tag:           "snell-default",
		ContainerType: string(contracts.ContainerSnell),
		CertSource:    "none",
		NativeJSON:    []byte(`{"enabled":false,"port":16160}`),
		InboundType:   "direct",
	}))
	require.NoError(t, sm.InboundStore().Save(&store.InboundRecord{
		Tag:           "xray-enabled",
		ContainerType: string(contracts.ContainerXray),
		CertSource:    "none",
		NativeJSON:    []byte(`{"tag":"xray-enabled","port":10001}`),
		InboundType:   "direct",
	}))

	hysteriaStub := &chainContainerStub{
		captureContainer: captureContainer{kind: contracts.ContainerHysteria},
		supportsChain:    false,
	}
	snellStub := &chainContainerStub{
		captureContainer: captureContainer{kind: contracts.ContainerSnell},
		supportsChain:    false,
	}
	xrayStub := &chainContainerStub{
		captureContainer: captureContainer{kind: contracts.ContainerXray},
		supportsChain:    true,
	}
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerHysteria, hysteriaStub)
	mgr.SetForTest(contracts.ContainerSnell, snellStub)
	mgr.SetForTest(contracts.ContainerXray, xrayStub)

	certs := &certSpyMgr{certDir: t.TempDir()}
	srv := &EndNodeServer{
		cfg:          appconfig.EndNodeConfig{AllowLoopbackChainTarget: true},
		containerMgr: mgr,
		certManager:  certs,
	}
	srv.SetInboundStore(sm.InboundStore())

	// 1. Same-container direct FastAddInbound re-enables disabled hysteria-default and snell-default.
	rsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_Hysteria2SettingBuilderType,
		ContainerType:      "hysteria",
		Tag:                "hysteria-default",
		Domain:             "hy2.example.com",
		InboundType:        "direct",
		AccessTags:         []string{"vip"},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code, "disabled hysteria-default should be re-enableable: %s", rsp.Msg)
	assert.Equal(t, "hysteria-default", hysteriaStub.lastTag)

	// Re-enabling a disabled Mode A inbound as chain must still be rejected.
	rsp, err = srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_Hysteria2SettingBuilderType,
		ContainerType:      "hysteria",
		Tag:                "hysteria-default",
		Domain:             "hy2.example.com",
		InboundType:        "chain",
		ChainUri:           "socks5://127.0.0.1:7890",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1020), rsp.Code)

	rsp, err = srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		ContainerType:      "snell",
		Tag:                "snell-default",
		Security:           "none",
		InboundType:        "direct",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code, "disabled snell-default should be re-enableable: %s", rsp.Msg)
	assert.Equal(t, "snell-default", snellStub.lastTag)

	// 2. Cross-container collision with disabled hysteria-default is still REJECTED.
	rsp, err = srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		ContainerType:      "xray",
		Tag:                "hysteria-default",
		Security:           "none",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1020), rsp.Code)
	assert.Contains(t, rsp.Msg, "already exists in container hysteria")

	// 3. Enabled record in same container is still REJECTED.
	rsp, err = srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		ContainerType:      "xray",
		Tag:                "xray-enabled",
		Security:           "none",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1020), rsp.Code)
	assert.Contains(t, rsp.Msg, "already exists in container xray")

	// 4. Chain inbound rejects non-loopback extra_params["listen_addr"] before ObtainNewCert,
	//    and valid chain locks both listen and listen_addr to 127.0.0.1.
	certs.obtainCalls = nil
	rsp, err = srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		ContainerType:      "xray",
		Domain:             "listenaddr.example.com",
		Security:           "tls",
		InboundType:        "chain",
		ChainUri:           "socks5://127.0.0.1:7890",
		ExtraParams:        map[string]string{"listen_addr": "0.0.0.0"},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1020), rsp.Code)
	assert.Contains(t, rsp.Msg, "127.0.0.1")
	assert.Empty(t, certs.obtainCalls, "ObtainNewCert must not run when listen_addr is invalid")

	rsp, err = srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VLESSSettingBuilderType,
		ContainerType:      "xray",
		Domain:             "listenaddr-ok.example.com",
		Security:           "tls",
		InboundType:        "chain",
		ChainUri:           "socks5://127.0.0.1:7890",
		ExtraParams:        map[string]string{"listen_addr": "127.0.0.1"},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code, "unexpected error: %s", rsp.Msg)
	assert.Equal(t, "127.0.0.1", xrayStub.lastParams["listen"])
	assert.Equal(t, "127.0.0.1", xrayStub.lastParams["listen_addr"])
}

func TestUpdateUsers_ErrorReturnsNonZeroCode(t *testing.T) {
	um := usermanager.NewUserManager(nil, "node-1")
	srv := &EndNodeServer{userMgr: um}

	// Updating a non-existent user must return non-zero Code so RPC clients / HTTP / MCP propagate errors.
	rsp, err := srv.UpdateUsers(context.Background(), &proto.UserOpReq{
		Users: []*proto.User{
			{Name: "does-not-exist", Role: "admin"},
		},
	})
	require.NoError(t, err)
	assert.NotEqual(t, int32(0), rsp.GetCode(), "UpdateUsers must set non-zero Code on error")
	assert.NotEmpty(t, rsp.GetMsg())

	// Also verify invalid inbound_tags returns non-zero Code even for an existing user.
	require.NoError(t, um.AddUser(usermanager.AddUserRequest{Username: "alice", Password: "pw"}))
	rsp2, err := srv.UpdateUsers(context.Background(), &proto.UserOpReq{
		Users: []*proto.User{
			{Name: "alice", UpdateInboundTags: true, InboundTags: []string{"bad,tag"}},
		},
	})
	require.NoError(t, err)
	assert.NotEqual(t, int32(0), rsp2.GetCode(), "UpdateUsers must set non-zero Code on invalid inbound_tags")
	assert.NotEmpty(t, rsp2.GetMsg())
}

func TestGetProfile_SynchronousLayer2VisibilityFilter(t *testing.T) {
	ngs := newTestNGStore("vip")
	fm := &chainForwardMgrStub{
		rules: []*forward.ForwardRule{
			{Username: "alice", InboundTag: "direct-pub", ContainerType: contracts.ContainerXray, ListenPort: 10001},
			{Username: "alice", InboundTag: "chain-vip", ContainerType: contracts.ContainerXray, ListenPort: 10002},
		},
	}
	um := usermanager.NewUserManager(fm, "node-1")
	um.EnableClusterSync("default", ngs)

	require.NoError(t, um.AddUser(usermanager.AddUserRequest{
		Username:    "alice",
		Password:    "pw",
		TargetGroup: "vip",
		InboundTags: nil, // lacks "vip" tag even though stale forward rule still exists in fm
	}))

	xray := &chainContainerStub{
		captureContainer: captureContainer{kind: contracts.ContainerXray},
		metas: map[string]contracts.InboundMeta{
			"direct-pub": {InboundType: contracts.InboundTypeDirect, AccessTags: nil},
			"chain-vip":  {InboundType: contracts.InboundTypeChain, AccessTags: []string{"vip"}},
		},
	}
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, xray)

	srv := &EndNodeServer{
		userMgr:      um,
		containerMgr: mgr,
	}

	prof, err := srv.GetProfile(context.Background(), &proto.GetProfileReq{Username: "alice"})
	require.NoError(t, err)
	assert.Equal(t, int32(0), prof.GetCode())
	require.Len(t, prof.GetInbounds(), 1, "GetProfile must synchronously exclude Layer-2 unauthorized inbound")
	assert.Equal(t, "direct-pub", prof.GetInbounds()[0].GetTag())

	// Granting "vip" immediately exposes chain-vip in GetProfile.
	require.NoError(t, um.SetUserInboundTags("alice", []string{"vip"}))
	prof2, err := srv.GetProfile(context.Background(), &proto.GetProfileReq{Username: "alice"})
	require.NoError(t, err)
	assert.Len(t, prof2.GetInbounds(), 2)
}
