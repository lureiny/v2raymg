package server

import (
	"context"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubInbound is a minimal inbound.Inbound implementation for ListInbound tests.
// Only Tag() is exercised; everything else returns zero values.
type stubInbound struct {
	tag string
}

func (s *stubInbound) Tag() string                       { return s.tag }
func (s *stubInbound) Protocol() contracts.Protocol      { return "" }
func (s *stubInbound) Port() uint32                      { return 0 }
func (s *stubInbound) ListenAddr() string                { return "" }
func (s *stubInbound) Config() *inbound.Config           { return nil }
func (s *stubInbound) Extra() map[string]any             { return nil }
func (s *stubInbound) ToNative() ([]byte, error)         { return nil, nil }
func (s *stubInbound) Validate() error                   { return nil }

// --- getContainerByType ---

func TestGetContainerByType(t *testing.T) {
	xray := &captureContainer{kind: contracts.ContainerXray}
	snell := &captureContainer{kind: contracts.ContainerSnell}
	hysteria := &captureContainer{kind: contracts.ContainerHysteria}
	mihomo := &captureContainer{kind: contracts.ContainerMihomo}

	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, xray)
	mgr.SetForTest(contracts.ContainerSnell, snell)
	mgr.SetForTest(contracts.ContainerHysteria, hysteria)
	mgr.SetForTest(contracts.ContainerMihomo, mihomo)
	srv := &EndNodeServer{containerMgr: mgr}

	cases := []struct {
		name    string
		input   string
		wantErr bool
		want    container.Container
	}{
		{"empty defaults to xray", "", false, xray},
		{"xray explicit", "xray", false, xray},
		{"snell", "snell", false, snell},
		{"hysteria", "hysteria", false, hysteria},
		{"hysteria2 alias", "hysteria2", false, hysteria},
		{"mihomo", "mihomo", false, mihomo},
		{"unknown returns not-found err", "bogus", true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := srv.getContainerByType(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				assert.Nil(t, got)
				assert.Contains(t, err.Error(), "not found or not enabled")
				return
			}
			require.NoError(t, err)
			assert.Same(t, tc.want, got)
		})
	}
}

func TestGetContainerByType_NotRegistered(t *testing.T) {
	// Only xray is registered; mihomo lookup must surface "not found".
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, &captureContainer{})
	srv := &EndNodeServer{containerMgr: mgr}

	got, err := srv.getContainerByType("mihomo")
	assert.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), `"mihomo"`)
}

// --- ListInbound ---

func TestListInbound_PolymorphicAcrossContainers(t *testing.T) {
	xray := &captureContainer{
		kind:     contracts.ContainerXray,
		inbounds: []inbound.Inbound{&stubInbound{tag: "vless-tcp"}, &stubInbound{tag: "vmess-ws"}},
	}
	mihomo := &captureContainer{
		kind:     contracts.ContainerMihomo,
		inbounds: []inbound.Inbound{&stubInbound{tag: "mihomo-vmess"}},
	}
	snell := &captureContainer{
		kind:     contracts.ContainerSnell,
		inbounds: []inbound.Inbound{&stubInbound{tag: "snell-main"}},
	}

	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, xray)
	mgr.SetForTest(contracts.ContainerMihomo, mihomo)
	mgr.SetForTest(contracts.ContainerSnell, snell)
	srv := &EndNodeServer{containerMgr: mgr}

	rsp, err := srv.ListInbound(context.Background(), &proto.ListInboundReq{})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code)

	// Index returned entries by "container:tag" so order from map iteration doesn't matter.
	got := map[string]bool{}
	for _, info := range rsp.Inbounds {
		got[info.GetContainer()+":"+info.GetName()] = true
	}
	assert.Equal(t, map[string]bool{
		"xray:vless-tcp":      true,
		"xray:vmess-ws":       true,
		"mihomo:mihomo-vmess": true,
		"snell:snell-main":    true,
	}, got)
}

// TestListInbound_Sorted asserts the (container, name) ordering guarantee so
// script consumers get stable output across calls. Without the sort, Go's
// map iteration would reshuffle the result.
func TestListInbound_Sorted(t *testing.T) {
	xray := &captureContainer{
		kind: contracts.ContainerXray,
		inbounds: []inbound.Inbound{
			&stubInbound{tag: "zeta"},
			&stubInbound{tag: "alpha"},
			&stubInbound{tag: "mid"},
		},
	}
	mihomo := &captureContainer{
		kind:     contracts.ContainerMihomo,
		inbounds: []inbound.Inbound{&stubInbound{tag: "beta"}},
	}
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, xray)
	mgr.SetForTest(contracts.ContainerMihomo, mihomo)
	srv := &EndNodeServer{containerMgr: mgr}

	rsp, err := srv.ListInbound(context.Background(), &proto.ListInboundReq{})
	require.NoError(t, err)

	var got []string
	for _, info := range rsp.Inbounds {
		got = append(got, info.GetContainer()+":"+info.GetName())
	}
	// Alphabetical by container first ("mihomo" < "xray"), then by name.
	assert.Equal(t, []string{
		"mihomo:beta",
		"xray:alpha",
		"xray:mid",
		"xray:zeta",
	}, got)
}

func TestListInbound_Empty(t *testing.T) {
	// No containers registered → empty list, no error.
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	srv := &EndNodeServer{containerMgr: mgr}

	rsp, err := srv.ListInbound(context.Background(), &proto.ListInboundReq{})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code)
	assert.Empty(t, rsp.Inbounds)
}

// --- FastAddInbound + mihomo end-to-end ---

// TestFastAddInbound_MihomoRouted verifies that a FastAddInbound request with
// ContainerType="mihomo" is routed to the mihomo container (not xray) and that
// params are forwarded intact, matching the pattern exercised by the xray
// coverage in fastadd_params_test.go.
func TestFastAddInbound_MihomoRouted(t *testing.T) {
	xray := &captureContainer{kind: contracts.ContainerXray}
	mihomo := &captureContainer{kind: contracts.ContainerMihomo}

	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, xray)
	mgr.SetForTest(contracts.ContainerMihomo, mihomo)
	srv := &EndNodeServer{
		containerMgr: mgr,
		certManager:  &noopCertMgr{},
	}

	rsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VMESSSettingBuilderType,
		Tag:                "mihomo-vmess-ws",
		Port:               12345,
		Transport:          "ws",
		Security:           "none",
		ContainerType:      "mihomo",
		ExtraParams: map[string]string{
			"uuid":    "11111111-2222-3333-4444-555555555555",
			"ws_path": "/mihomo",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), rsp.Code, "error: %s", rsp.Msg)

	// Routed to mihomo — xray was not touched.
	assert.Equal(t, "mihomo-vmess-ws", mihomo.lastTag)
	assert.Equal(t, "", xray.lastTag, "xray should not receive the call")

	// Params carried through the switch into the mihomo container.
	assert.Equal(t, "vmess", mihomo.lastParams["protocol"])
	assert.Equal(t, "ws", mihomo.lastParams["transport"])
	assert.Equal(t, int32(12345), mihomo.lastParams["port"])
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", mihomo.lastParams["uuid"])
	assert.Equal(t, "/mihomo", mihomo.lastParams["ws_path"])
}

func TestFastAddInbound_MihomoNotRegistered(t *testing.T) {
	// Only xray is registered; FastAddInbound with container="mihomo" must fail
	// with a not-found error, not be silently dispatched to xray.
	xray := &captureContainer{kind: contracts.ContainerXray}
	mgr := container.NewContainerMgr(nil, container.BuildOptions{})
	mgr.SetForTest(contracts.ContainerXray, xray)
	srv := &EndNodeServer{
		containerMgr: mgr,
		certManager:  &noopCertMgr{},
	}

	rsp, err := srv.FastAddInbound(context.Background(), &proto.FastAddInboundReq{
		InboundBuilderType: proto.BuilderType_VMESSSettingBuilderType,
		Tag:                "mihomo-vmess",
		Port:               12345,
		Transport:          "tcp",
		Security:           "none",
		ContainerType:      "mihomo",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1020), rsp.Code)
	assert.Contains(t, rsp.Msg, "mihomo")
	assert.Equal(t, "", xray.lastTag, "xray must not receive the mihomo call")
}
