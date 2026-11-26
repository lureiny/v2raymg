package xray

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/app/proxyman"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/app/proxyman/command"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

const testChainVLESSURI = "vless://11111111-1111-4111-8111-111111111111@hk.example.com:443?security=tls&type=ws&path=%2Fws#HK-Node"

// memNodeGroupsStore is an in-memory NodeGroupsStore for testing two-layer isolation.
type memNodeGroupsStore struct {
	mu     sync.Mutex
	groups []string
}

func (m *memNodeGroupsStore) List() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.groups), nil
}

func (m *memNodeGroupsStore) Set(groups []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.groups = slices.Clone(groups)
	return nil
}

// chainTestHarness bundles an in-process fake xray HandlerService gRPC server,
// an injectable routing CLI recorder, a SQLite StoreManager, and a real
// DefaultForwardManager (acting as both relay manager and PortClaimer).
type chainTestHarness struct {
	command.UnimplementedHandlerServiceServer

	grpcAddr string
	grpcSrv  *grpc.Server
	storeMgr *store.StoreManager
	fm       forward.ForwardManager
	userMgr  *usermanager.UserManager

	mu            sync.Mutex
	events        []string
	grpcInbounds  map[string]*proxyman.InboundHandlerConfig
	cliOutbounds  map[string]json.RawMessage
	cliRules      map[string]string
	failAddInbErr error
	failCLI       func(subcmd string, args []string, stdin []byte) error
}

func newChainTestHarness(t *testing.T, withUserMgr bool) *chainTestHarness {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	dbPath := filepath.Join(t.TempDir(), "chain_test.db")
	storeMgr, err := store.NewStoreManager(dbPath, migrations.All)
	require.NoError(t, err)

	fm, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort:   36000,
		MaxPort:   36999,
		UseRandom: true,
	})
	require.NoError(t, err)

	h := &chainTestHarness{
		grpcAddr:     ln.Addr().String(),
		grpcSrv:      grpc.NewServer(),
		storeMgr:     storeMgr,
		fm:           fm,
		grpcInbounds: make(map[string]*proxyman.InboundHandlerConfig),
		cliOutbounds: make(map[string]json.RawMessage),
		cliRules:     make(map[string]string),
	}

	if withUserMgr {
		um, umErr := usermanager.NewUserManagerWithStore(fm, storeMgr, "local-node")
		require.NoError(t, umErr)
		ngs := &memNodeGroupsStore{}
		um.EnableClusterSync("default", ngs)
		require.NoError(t, um.SetNodeGroups([]string{"vip"}))
		h.userMgr = um
	}

	command.RegisterHandlerServiceServer(h.grpcSrv, h)
	go func() {
		_ = h.grpcSrv.Serve(ln)
	}()

	t.Cleanup(func() {
		h.grpcSrv.Stop()
		_ = h.fm.Close()
		_ = h.storeMgr.Close()
	})

	return h
}

func (h *chainTestHarness) AddInbound(ctx context.Context, req *command.AddInboundRequest) (*command.AddInboundResponse, error) {
	if req.GetInbound() == nil {
		return nil, fmt.Errorf("nil inbound")
	}
	tag := req.GetInbound().GetTag()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failAddInbErr != nil {
		return nil, h.failAddInbErr
	}
	if _, exists := h.grpcInbounds[tag]; exists {
		return nil, fmt.Errorf("existing tag found: %s", tag)
	}
	h.events = append(h.events, "grpc:AddInbound:"+tag)
	h.grpcInbounds[tag] = req.GetInbound()
	return &command.AddInboundResponse{}, nil
}

func (h *chainTestHarness) RemoveInbound(ctx context.Context, req *command.RemoveInboundRequest) (*command.RemoveInboundResponse, error) {
	tag := req.GetTag()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, "grpc:RemoveInbound:"+tag)
	delete(h.grpcInbounds, tag)
	return &command.RemoveInboundResponse{}, nil
}

func (h *chainTestHarness) AlterInbound(ctx context.Context, req *command.AlterInboundRequest) (*command.AlterInboundResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, "grpc:AlterInbound:"+req.GetTag())
	return &command.AlterInboundResponse{}, nil
}

func (h *chainTestHarness) ListInbounds(ctx context.Context, req *command.ListInboundsRequest) (*command.ListInboundsResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*proxyman.InboundHandlerConfig, 0, len(h.grpcInbounds))
	for _, inb := range h.grpcInbounds {
		out = append(out, inb)
	}
	return &command.ListInboundsResponse{Inbounds: out}, nil
}

func (h *chainTestHarness) cliRunner() cliRunner {
	return func(ctx context.Context, binaryPath string, args []string, stdin []byte) ([]byte, error) {
		if len(args) < 2 || args[0] != "api" {
			return nil, fmt.Errorf("unexpected cli args: %v", args)
		}
		subcmd := args[1]

		h.mu.Lock()
		failFn := h.failCLI
		h.mu.Unlock()
		if failFn != nil {
			if err := failFn(subcmd, args, stdin); err != nil {
				return []byte("mock error"), err
			}
		}

		h.mu.Lock()
		defer h.mu.Unlock()

		switch subcmd {
		case "rmrules":
			rTag := args[len(args)-1]
			h.events = append(h.events, "cli:rmrules:"+rTag)
			delete(h.cliRules, rTag)
		case "rmo":
			outTag := args[len(args)-1]
			h.events = append(h.events, "cli:rmo:"+outTag)
			delete(h.cliOutbounds, outTag)
		case "ado":
			var payload struct {
				Outbounds []json.RawMessage `json:"outbounds"`
			}
			if err := json.Unmarshal(stdin, &payload); err != nil || len(payload.Outbounds) != 1 {
				return nil, fmt.Errorf("invalid ado stdin: %v", err)
			}
			var meta struct {
				Tag string `json:"tag"`
			}
			if err := json.Unmarshal(payload.Outbounds[0], &meta); err != nil {
				return nil, err
			}
			h.events = append(h.events, "cli:ado:"+meta.Tag)
			h.cliOutbounds[meta.Tag] = payload.Outbounds[0]
		case "adrules":
			var payload struct {
				Routing struct {
					Rules []struct {
						RuleTag     string   `json:"ruleTag"`
						InboundTag  []string `json:"inboundTag"`
						OutboundTag string   `json:"outboundTag"`
					} `json:"rules"`
				} `json:"routing"`
			}
			if err := json.Unmarshal(stdin, &payload); err != nil || len(payload.Routing.Rules) != 1 {
				return nil, fmt.Errorf("invalid adrules stdin: %v", err)
			}
			r := payload.Routing.Rules[0]
			h.events = append(h.events, fmt.Sprintf("cli:adrules:%s:%s->%s", r.RuleTag, strings.Join(r.InboundTag, ","), r.OutboundTag))
			h.cliRules[r.RuleTag] = r.OutboundTag
		default:
			return nil, fmt.Errorf("unsupported subcmd %s", subcmd)
		}
		return []byte("ok"), nil
	}
}

func (h *chainTestHarness) snapshotEvents() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.events)
}

func (h *chainTestHarness) clearEvents() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = nil
}

func (h *chainTestHarness) newExecutor(t *testing.T, binaryPath, configPath string) *Executor {
	t.Helper()
	if binaryPath == "" {
		binaryPath = "/nonexistent/xray"
	}
	if configPath == "" {
		configPath = filepath.Join(t.TempDir(), "xray.json")
	}
	exec, err := NewExecutor(ExecutorConfig{
		BinaryPath:        binaryPath,
		ConfigFilePath:    configPath,
		ContainerType:     container.ContainerXray,
		GRPCAPIAddress:    h.grpcAddr,
		PortClaimer:       h.fm,
		StoreMgr:          h.storeMgr,
		UserManager:       h.userMgr,
		ReconcileInterval: 0,
	})
	require.NoError(t, err)
	exec.routingRunner = h.cliRunner()
	t.Cleanup(func() {
		exec.Close()
	})
	return exec
}

func mustXrayInbound(t *testing.T, exec *Executor, tag string) *XrayInbound {
	t.Helper()
	inb, err := exec.GetInboundConfig(tag)
	require.NoError(t, err, "expected inbound %q to exist", tag)
	xi, ok := inb.(*XrayInbound)
	require.True(t, ok, "expected *XrayInbound for %q", tag)
	return xi
}

func assertNoInbound(t *testing.T, exec *Executor, tag string) {
	t.Helper()
	_, err := exec.GetInboundConfig(tag)
	assert.Error(t, err, "expected inbound %q not to exist", tag)
}

func TestEnsureConfig_RoutingServiceAndWarnings(t *testing.T) {
	t.Run("creates fresh config containing RoutingService", func(t *testing.T) {
		h := newChainTestHarness(t, false)
		cfgPath := filepath.Join(t.TempDir(), "fresh.json")
		exec := h.newExecutor(t, "", cfgPath)

		require.NoError(t, exec.EnsureConfig())
		generatedPath := exec.ConfigFile()
		t.Cleanup(func() { _ = os.Remove(generatedPath) })
		data, err := os.ReadFile(generatedPath)
		require.NoError(t, err)

		var cfg map[string]any
		require.NoError(t, json.Unmarshal(data, &cfg))
		apiMap := cfg["api"].(map[string]any)
		services := apiMap["services"].([]any)
		assert.Contains(t, services, "RoutingService")
		assert.Contains(t, services, "HandlerService")
	})

	t.Run("patches missing RoutingService into existing config cleanly", func(t *testing.T) {
		h := newChainTestHarness(t, false)
		cfgPath := filepath.Join(t.TempDir(), "existing.json")
		existing := `{
  "api": {
    "tag": "api",
    "services": ["HandlerService", "LoggerService", "StatsService"]
  },
  "outbounds": [
    {"tag": "proxy-first", "protocol": "socks"},
    {"tag": "direct", "protocol": "freedom"}
  ],
  "routing": {
    "rules": [
      {"type": "field", "network": "tcp,udp", "outboundTag": "proxy-first"}
    ]
  }
}`
		require.NoError(t, os.WriteFile(cfgPath, []byte(existing), 0644))

		exec := h.newExecutor(t, "", cfgPath)
		require.NoError(t, exec.EnsureConfig())

		data, err := os.ReadFile(cfgPath)
		require.NoError(t, err)

		var cfg map[string]any
		require.NoError(t, json.Unmarshal(data, &cfg))
		apiMap := cfg["api"].(map[string]any)
		assert.Equal(t, "api", apiMap["tag"])
		assert.ElementsMatch(t,
			[]any{"HandlerService", "LoggerService", "StatsService", "RoutingService"},
			apiMap["services"].([]any),
		)

		// Existing outbounds & rules stay preserved.
		outbounds := cfg["outbounds"].([]any)
		require.Len(t, outbounds, 2)
		assert.Equal(t, "proxy-first", outbounds[0].(map[string]any)["tag"])
	})
}

func TestChainCapable_InterfaceAndValidation(t *testing.T) {
	h := newChainTestHarness(t, false)
	exec := h.newExecutor(t, "", "")

	var cc container.ChainCapable = exec
	var tagUpdater container.InboundTagUpdater = exec
	var metaProvider container.InboundMetaProvider = exec
	assert.True(t, cc.SupportsChainOutbound())
	_ = tagUpdater
	_ = metaProvider

	validURIs := []string{
		testChainVLESSURI,
		"trojan://supersecret@trojan.example.com:443?sni=trojan.example.com#TR",
		"ss://YWVzLTI1Ni1nY206c3VwZXJzZWNyZXQ@ss.example.com:8388#SS",
		"socks5://user:pass@socks.example.com:1080#S5",
		"http://user:pass@http.example.com:8080#HTTP",
	}
	for _, uri := range validURIs {
		assert.NoError(t, exec.ValidateChainURI(uri), "expected valid URI: %s", uri)
	}

	// Unsupported xray protocols suggest mihomo and never leak credentials.
	err := exec.ValidateChainURI("hy2://secret-hy2-token@hy2.example.com:443#HY2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mihomo")
	assert.NotContains(t, err.Error(), "secret-hy2-token")

	err = exec.ValidateChainURI("tuic://11111111-1111-4111-8111-111111111111:tuicpass@tuic.example.com:443#TUIC")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mihomo")
	assert.NotContains(t, err.Error(), "tuicpass")

	err = exec.ValidateChainURI("ss://YWVzLTI1Ni1nY206c3VwZXJzZWNyZXQ@ss.example.com:8388?plugin=obfs-local%3Bobfs%3Dhttp#SSPlugin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mihomo")
	assert.NotContains(t, err.Error(), "supersecret")

	// Xray 26.2.6 removed allowInsecure, network=http (h2), and security=xtls.
	err = exec.ValidateChainURI("vless://11111111-1111-4111-8111-111111111111@hk.example.com:443?security=tls&insecure=1#Insec")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mihomo")
	assert.NotContains(t, err.Error(), "11111111-1111-4111-8111-111111111111")

	err = exec.ValidateChainURI("trojan://secret-h2@hk.example.com:443?security=tls&type=h2&path=%2Fh2#H2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mihomo")
	assert.Contains(t, err.Error(), "xhttp")
	assert.NotContains(t, err.Error(), "secret-h2")

	err = exec.ValidateChainURI("vless://22222222-2222-4222-8222-222222222222@hk.example.com:443?security=xtls#XTLS")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mihomo")
	assert.NotContains(t, err.Error(), "22222222-2222-4222-8222-222222222222")
}

func TestFastAddInbound_ListenDefaultAndChainValidation(t *testing.T) {
	t.Run("direct defaults listen to 127.0.0.1 and allows 0.0.0.0 override", func(t *testing.T) {
		h := newChainTestHarness(t, false)
		exec := h.newExecutor(t, "", "")

		require.NoError(t, exec.FastAddInbound("direct-def", map[string]any{
			"protocol": "vmess",
			"security": "none",
		}))
		inDef := mustXrayInbound(t, exec, "direct-def")
		assert.Equal(t, "127.0.0.1", inDef.ListenAddr())
		assert.Equal(t, contracts.InboundTypeDirect, inDef.GetInboundType())

		require.NoError(t, exec.FastAddInbound("direct-pub", map[string]any{
			"protocol": "vmess",
			"security": "none",
			"listen":   "0.0.0.0",
			"port":     int32(36588),
		}))
		inPub := mustXrayInbound(t, exec, "direct-pub")
		assert.Equal(t, "0.0.0.0", inPub.ListenAddr())
		assert.Equal(t, uint32(36588), inPub.Port(), "int32 port from RPC FastAddInboundReq must be honored")
	})

	t.Run("chain validation rejects non-127.0.0.1 listen, invalid tag, mismatched fields", func(t *testing.T) {
		h := newChainTestHarness(t, false)
		exec := h.newExecutor(t, "", "")

		// Reject reserved __ prefix tag.
		err := exec.FastAddInbound("__chain_bad", map[string]any{
			"protocol": "vmess",
			"security": "none",
		})
		require.Error(t, err)

		// Reject explicit non-127.0.0.1 listen on chain inbound.
		err = exec.FastAddInbound("chain-pub", map[string]any{
			"protocol":     "vmess",
			"security":     "none",
			"inbound_type": "chain",
			"chain_uri":    testChainVLESSURI,
			"listen":       "0.0.0.0",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "127.0.0.1")

		// Reject explicit non-127.0.0.1 listen_addr on chain inbound.
		err = exec.FastAddInbound("chain-pub-addr", map[string]any{
			"protocol":     "vmess",
			"security":     "none",
			"inbound_type": "chain",
			"chain_uri":    testChainVLESSURI,
			"listen_addr":  "0.0.0.0",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "127.0.0.1")

		// Reject direct inbound with non-empty chain_uri.
		err = exec.FastAddInbound("direct-with-uri", map[string]any{
			"protocol":     "vmess",
			"security":     "none",
			"inbound_type": "direct",
			"chain_uri":    testChainVLESSURI,
		})
		require.Error(t, err)

		// Reject chain inbound with empty chain_uri.
		err = exec.FastAddInbound("chain-no-uri", map[string]any{
			"protocol":     "vmess",
			"security":     "none",
			"inbound_type": "chain",
		})
		require.Error(t, err)

		// Reject invalid access_tags.
		err = exec.FastAddInbound("bad-tags", map[string]any{
			"protocol":     "vmess",
			"security":     "none",
			"access_tags":  []string{"bad:tag"},
			"inbound_type": "direct",
		})
		require.Error(t, err)

		// No CLI calls were made on validation failures.
		assert.Empty(t, h.snapshotEvents())
	})

	t.Run("chain defaults remark from URI fragment, forces sniffing routeOnly=true, and duplicate add does not touch live routing", func(t *testing.T) {
		h := newChainTestHarness(t, false)
		exec := h.newExecutor(t, "", "")

		require.NoError(t, exec.FastAddInbound("ch-sniff", map[string]any{
			"protocol":         "vmess",
			"security":         "none",
			"inbound_type":     "chain",
			"chain_uri":        testChainVLESSURI,
			"access_tags":      []string{"vip", "hk", "vip"},
			"sniffing_enabled": true,
		}))

		xi := mustXrayInbound(t, exec, "ch-sniff")
		assert.Equal(t, "127.0.0.1", xi.ListenAddr())
		assert.Equal(t, contracts.InboundTypeChain, xi.GetInboundType())
		assert.Equal(t, "HK-Node", xi.GetRemark())
		assert.Equal(t, []string{"hk", "vip"}, xi.GetAccessTags())

		var native map[string]any
		require.NoError(t, json.Unmarshal(xi.nativeJSON, &native))
		assert.Equal(t, "127.0.0.1", native["listen"])
		sniffing, ok := native["sniffing"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, sniffing["enabled"])
		assert.Equal(t, true, sniffing["routeOnly"])

		// Duplicate FastAddInbound on existing tag fails before running cleanup CLI commands.
		h.clearEvents()
		err := exec.FastAddInbound("ch-sniff", map[string]any{
			"protocol":     "vmess",
			"security":     "none",
			"inbound_type": "chain",
			"chain_uri":    testChainVLESSURI,
		})
		require.Error(t, err)
		assert.Empty(t, h.snapshotEvents())
	})
}

func TestFastAddInbound_ChainZeroLeakOrderAndRollbacks(t *testing.T) {
	t.Run("strict zero-leak add sequence and metadata persistence", func(t *testing.T) {
		h := newChainTestHarness(t, false)
		exec := h.newExecutor(t, "", "")

		err := exec.FastAddInbound("ch1", map[string]any{
			"protocol":     "vmess",
			"security":     "none",
			"port":         36501,
			"inbound_type": "chain",
			"chain_uri":    testChainVLESSURI,
			"access_tags":  []string{"vip", "asia"},
			"remark":       "CustomRemark",
		})
		require.NoError(t, err)

		assert.Equal(t, []string{
			"cli:rmrules:__chain_rule__:ch1",
			"cli:rmo:__chain_out__:ch1",
			"cli:ado:__chain_out__:ch1",
			"cli:adrules:__chain_rule__:ch1:ch1->__chain_out__:ch1",
			"grpc:AddInbound:ch1",
		}, h.snapshotEvents())

		rec, err := h.storeMgr.InboundStore().Get("ch1")
		require.NoError(t, err)
		require.NotNil(t, rec)
		assert.Equal(t, "chain", rec.InboundType)
		assert.Equal(t, []string{"asia", "vip"}, rec.AccessTags)
		assert.Equal(t, testChainVLESSURI, rec.ChainOutboundURI)
		assert.Equal(t, "CustomRemark", rec.Remark)
	})

	t.Run("AddRule failure rolls back RemoveOutbound and releases port", func(t *testing.T) {
		h := newChainTestHarness(t, false)
		h.mu.Lock()
		h.failCLI = func(subcmd string, args []string, stdin []byte) error {
			if subcmd == "adrules" {
				return errors.New("boom adrules")
			}
			return nil
		}
		h.mu.Unlock()
		exec := h.newExecutor(t, "", "")

		err := exec.FastAddInbound("ch-rb1", map[string]any{
			"protocol":     "vmess",
			"security":     "none",
			"port":         36502,
			"inbound_type": "chain",
			"chain_uri":    "trojan://very-secret-pwd@tr.example.com:443#TR",
		})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "very-secret-pwd")

		assert.Equal(t, []string{
			"cli:rmrules:__chain_rule__:ch-rb1",
			"cli:rmo:__chain_out__:ch-rb1",
			"cli:ado:__chain_out__:ch-rb1",
			"cli:rmo:__chain_out__:ch-rb1",
		}, h.snapshotEvents())
		assertNoInbound(t, exec, "ch-rb1")
		rec, getErr := h.storeMgr.InboundStore().Get("ch-rb1")
		require.NoError(t, getErr)
		assert.Nil(t, rec)

		// Port 36502 must be freed back to PortClaimer.
		require.NoError(t, h.fm.AllocateSpecificPort(36502))
		h.fm.ReleasePort(36502)
	})

	t.Run("grpc AddInbound failure rolls back RemoveRule + RemoveOutbound + port", func(t *testing.T) {
		h := newChainTestHarness(t, false)
		h.mu.Lock()
		h.failAddInbErr = errors.New("grpc add failed")
		h.mu.Unlock()
		exec := h.newExecutor(t, "", "")

		err := exec.FastAddInbound("ch-rb2", map[string]any{
			"protocol":     "vless",
			"security":     "tls",
			"self_signed":  true,
			"port":         36503,
			"inbound_type": "chain",
			"chain_uri":    testChainVLESSURI,
		})
		require.Error(t, err)

		assert.Equal(t, []string{
			"cli:rmrules:__chain_rule__:ch-rb2",
			"cli:rmo:__chain_out__:ch-rb2",
			"cli:ado:__chain_out__:ch-rb2",
			"cli:adrules:__chain_rule__:ch-rb2:ch-rb2->__chain_out__:ch-rb2",
			"cli:rmrules:__chain_rule__:ch-rb2",
			"cli:rmo:__chain_out__:ch-rb2",
		}, h.snapshotEvents())
		assertNoInbound(t, exec, "ch-rb2")

		// Port 36503 must be freed.
		require.NoError(t, h.fm.AllocateSpecificPort(36503))
		h.fm.ReleasePort(36503)
	})

	t.Run("store Save failure rolls back grpc AddInbound + RemoveRule + RemoveOutbound + port", func(t *testing.T) {
		h := newChainTestHarness(t, false)
		exec := h.newExecutor(t, "", "")

		// Inject SQLite trigger so INSERT into inbounds fails cleanly.
		_, sqlErr := h.storeMgr.DB().DB().Exec(`CREATE TRIGGER fail_inbound_insert BEFORE INSERT ON inbounds BEGIN SELECT RAISE(ABORT, 'disk full'); END;`)
		require.NoError(t, sqlErr)

		err := exec.FastAddInbound("ch-rb3", map[string]any{
			"protocol":     "vmess",
			"security":     "none",
			"port":         36504,
			"inbound_type": "chain",
			"chain_uri":    testChainVLESSURI,
		})
		require.Error(t, err)

		assert.Equal(t, []string{
			"cli:rmrules:__chain_rule__:ch-rb3",
			"cli:rmo:__chain_out__:ch-rb3",
			"cli:ado:__chain_out__:ch-rb3",
			"cli:adrules:__chain_rule__:ch-rb3:ch-rb3->__chain_out__:ch-rb3",
			"grpc:AddInbound:ch-rb3",
			"grpc:RemoveInbound:ch-rb3",
			"cli:rmrules:__chain_rule__:ch-rb3",
			"cli:rmo:__chain_out__:ch-rb3",
		}, h.snapshotEvents())
		assertNoInbound(t, exec, "ch-rb3")

		require.NoError(t, h.fm.AllocateSpecificPort(36504))
		h.fm.ReleasePort(36504)
	})
}

func TestRemoveInboundConfig_ChainCleanupOrder(t *testing.T) {
	h := newChainTestHarness(t, true)
	exec := h.newExecutor(t, "", "")

	require.NoError(t, h.userMgr.AddUser(usermanager.AddUserRequest{
		Username:    "alice",
		Password:    "pass123",
		Role:        "user",
		TargetGroup: "vip",
		InboundTags: []string{"vip"},
	}))

	require.NoError(t, exec.FastAddInbound("ch-del", map[string]any{
		"protocol":     "vless",
		"security":     "tls",
		"self_signed":  true,
		"port":         36505,
		"inbound_type": "chain",
		"chain_uri":    testChainVLESSURI,
		"access_tags":  []string{"vip"},
	}))

	xi := mustXrayInbound(t, exec, "ch-del")
	require.Len(t, xi.tempCertFiles, 2)
	certPath := xi.tempCertFiles[0]
	keyPath := xi.tempCertFiles[1]
	_, certErr := os.Stat(certPath)
	require.NoError(t, certErr)

	// Reconcile so alice holds an active forward port bound to ch-del.
	exec.reconcileUsers()
	require.Len(t, h.fm.GetAllRules(), 1)

	h.clearEvents()
	require.NoError(t, exec.RemoveInboundConfig("ch-del"))

	assert.Equal(t, []string{
		"grpc:RemoveInbound:ch-del",
		"cli:rmrules:__chain_rule__:ch-del",
		"cli:rmo:__chain_out__:ch-del",
	}, h.snapshotEvents())

	// Store record, memory map, forward rules, temp cert files, and port claim must all be cleaned.
	rec, err := h.storeMgr.InboundStore().Get("ch-del")
	require.NoError(t, err)
	assert.Nil(t, rec)
	assertNoInbound(t, exec, "ch-del")
	assert.Empty(t, h.fm.GetAllRules())
	assert.True(t, os.IsNotExist(statErr(certPath)))
	assert.True(t, os.IsNotExist(statErr(keyPath)))

	require.NoError(t, h.fm.AllocateSpecificPort(36505))
	h.fm.ReleasePort(36505)
}

func TestRestore_Restart_And_AddInboundNativePreservesMeta(t *testing.T) {
	t.Run("AddInboundNative preserves existing store InboundMeta and Restore replays chain in order", func(t *testing.T) {
		h := newChainTestHarness(t, false)
		exec1 := h.newExecutor(t, "", "")

		// Persist one direct and one TLS self-signed chain inbound.
		require.NoError(t, exec1.FastAddInbound("a-chain", map[string]any{
			"protocol":     "vless",
			"security":     "tls",
			"self_signed":  true,
			"port":         36506,
			"inbound_type": "chain",
			"chain_uri":    testChainVLESSURI,
			"access_tags":  []string{"vip"},
			"remark":       "SG",
		}))
		require.NoError(t, exec1.FastAddInbound("b-direct", map[string]any{
			"protocol":    "vmess",
			"security":    "none",
			"port":        36507,
			"access_tags": []string{"basic"},
		}))

		// Simulate AddInboundNative called against existing tag in store (preserving chain meta).
		native := mustXrayInbound(t, exec1, "a-chain").nativeJSON
		h.mu.Lock()
		delete(h.grpcInbounds, "a-chain")
		h.mu.Unlock()
		require.NoError(t, exec1.AddInboundNative(native))
		metaAfterNative, ok := exec1.GetInboundMeta("a-chain")
		require.True(t, ok)
		assert.Equal(t, contracts.InboundTypeChain, metaAfterNative.InboundType)
		assert.Equal(t, []string{"vip"}, metaAfterNative.AccessTags)
		assert.Equal(t, testChainVLESSURI, metaAfterNative.ChainOutboundURI)
		assert.Equal(t, "SG", metaAfterNative.Remark)

		// Now simulate fresh process Restore on a brand-new harness gRPC state & Executor sharing the DB.
		h.mu.Lock()
		h.grpcInbounds = make(map[string]*proxyman.InboundHandlerConfig)
		h.cliOutbounds = make(map[string]json.RawMessage)
		h.cliRules = make(map[string]string)
		h.events = nil
		h.mu.Unlock()
		// Release ports from fm as if fresh process restarted.
		h.fm.ReleasePort(36506)
		h.fm.ReleasePort(36507)

		exec2 := h.newExecutor(t, "", "")
		require.NoError(t, exec2.Restore(context.Background()))

		assert.Equal(t, []string{
			"cli:rmrules:__chain_rule__:a-chain",
			"cli:rmo:__chain_out__:a-chain",
			"cli:ado:__chain_out__:a-chain",
			"cli:adrules:__chain_rule__:a-chain:a-chain->__chain_out__:a-chain",
			"grpc:AddInbound:a-chain",
			"grpc:AddInbound:b-direct",
		}, h.snapshotEvents())

		meta, ok := exec2.GetInboundMeta("a-chain")
		require.True(t, ok)
		assert.Equal(t, contracts.InboundTypeChain, meta.InboundType)
		assert.Equal(t, []string{"vip"}, meta.AccessTags)
		assert.Equal(t, testChainVLESSURI, meta.ChainOutboundURI)
		assert.Equal(t, "SG", meta.Remark)
		// Returned AccessTags slice must be a clone.
		meta.AccessTags[0] = "mutated"
		metaCheck, _ := exec2.GetInboundMeta("a-chain")
		assert.Equal(t, []string{"vip"}, metaCheck.AccessTags)

		// Calling Restore again replaces existing temp cert files cleanly without port conflict.
		oldCertFiles := slices.Clone(mustXrayInbound(t, exec2, "a-chain").tempCertFiles)
		require.Len(t, oldCertFiles, 2)
		h.mu.Lock()
		delete(h.grpcInbounds, "a-chain")
		delete(h.grpcInbounds, "b-direct")
		h.mu.Unlock()
		require.NoError(t, exec2.Restore(context.Background()))
		for _, oldFile := range oldCertFiles {
			assert.True(t, os.IsNotExist(statErr(oldFile)))
		}
		newCertFiles := mustXrayInbound(t, exec2, "a-chain").tempCertFiles
		require.Len(t, newCertFiles, 2)
		assert.NotEqual(t, oldCertFiles, newCertFiles)
	})

	t.Run("Executor.Restart triggers Restore of persisted chain inbound", func(t *testing.T) {
		h := newChainTestHarness(t, true)
		tmpDir := t.TempDir()
		scriptPath := filepath.Join(tmpDir, "fake-xray")
		require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexec sleep 30\n"), 0755))
		cfgPath := filepath.Join(tmpDir, "xray.json")
		require.NoError(t, os.WriteFile(cfgPath, []byte(`{"inbounds":[],"outbounds":[{"tag":"direct","protocol":"freedom"}]}`), 0644))

		exec := h.newExecutor(t, scriptPath, cfgPath)
		require.NoError(t, exec.EnsureConfig())
		require.NoError(t, exec.FastAddInbound("ch-restart", map[string]any{
			"protocol":     "vmess",
			"security":     "none",
			"port":         36508,
			"inbound_type": "chain",
			"chain_uri":    testChainVLESSURI,
		}))

		// Start the dummy subprocess via embedded Runner so Start()'s stale-port SIGKILL
		// does not target our in-process fake gRPC test server.
		require.NoError(t, exec.Runner.Start())
		t.Cleanup(func() { _ = exec.Runner.Stop() })

		// Simulate xray process losing in-process state on restart.
		h.mu.Lock()
		h.grpcInbounds = make(map[string]*proxyman.InboundHandlerConfig)
		h.cliOutbounds = make(map[string]json.RawMessage)
		h.cliRules = make(map[string]string)
		h.events = nil
		h.mu.Unlock()

		require.NoError(t, exec.Restart())
		assert.True(t, exec.IsRunning())
		assert.Equal(t, []string{
			"cli:rmrules:__chain_rule__:ch-restart",
			"cli:rmo:__chain_out__:ch-restart",
			"cli:ado:__chain_out__:ch-restart",
			"cli:adrules:__chain_rule__:ch-restart:ch-restart->__chain_out__:ch-restart",
			"grpc:AddInbound:ch-restart",
		}, h.snapshotEvents())
	})
}

func TestSetInboundAccessTags_NoRestartImmediateReconcileAndRollback(t *testing.T) {
	h := newChainTestHarness(t, true)
	exec := h.newExecutor(t, "", "")

	require.NoError(t, h.userMgr.AddUser(usermanager.AddUserRequest{
		Username:    "alice",
		Password:    "pass123",
		Role:        "user",
		TargetGroup: "vip",
		InboundTags: nil, // public-only initially
	}))
	require.NoError(t, h.userMgr.AddUser(usermanager.AddUserRequest{
		Username:    "bob",
		Password:    "pass123",
		Role:        "user",
		TargetGroup: "vip",
		InboundTags: []string{"vip"},
	}))

	require.NoError(t, exec.FastAddInbound("ch-tag", map[string]any{
		"protocol":     "vmess",
		"security":     "none",
		"port":         36509,
		"inbound_type": "chain",
		"chain_uri":    testChainVLESSURI,
		"access_tags":  []string{"vip"},
	}))
	exec.reconcileUsers()

	xi := mustXrayInbound(t, exec, "ch-tag")
	assert.False(t, xi.hasAddedUser("alice"))
	assert.True(t, xi.hasAddedUser("bob"))
	require.Len(t, h.fm.GetAllRules(), 1)

	// Clear events so we can assert zero gRPC/CLI calls during tag update.
	h.clearEvents()

	// Clear access_tags -> open to all visible users; alice immediately gets bound.
	require.NoError(t, exec.SetInboundAccessTags("ch-tag", nil))
	assert.Empty(t, h.snapshotEvents(), "SetInboundAccessTags must not restart or mutate xray runtime")
	assert.Empty(t, xi.GetAccessTags())
	assert.True(t, xi.hasAddedUser("alice"))
	assert.True(t, xi.hasAddedUser("bob"))
	assert.Len(t, h.fm.GetAllRules(), 2)

	// Re-apply ["vip"] -> alice immediately revoked, bob remains.
	require.NoError(t, exec.SetInboundAccessTags("ch-tag", []string{"vip", "vip"}))
	assert.Empty(t, h.snapshotEvents())
	assert.Equal(t, []string{"vip"}, xi.GetAccessTags())
	assert.False(t, xi.hasAddedUser("alice"))
	assert.True(t, xi.hasAddedUser("bob"))
	assert.Len(t, h.fm.GetAllRules(), 1)

	// Store Save (INSERT OR REPLACE) error rollback preserves previous tags in memory.
	_, sqlErr := h.storeMgr.DB().DB().Exec(`CREATE TRIGGER fail_inbound_save BEFORE INSERT ON inbounds BEGIN SELECT RAISE(ABORT, 'disk full'); END;`)
	require.NoError(t, sqlErr)
	err := exec.SetInboundAccessTags("ch-tag", []string{"other"})
	require.Error(t, err)
	assert.Equal(t, []string{"vip"}, xi.GetAccessTags())
}

func TestTwoLayerIsolation_UserEventsReconcileAndSubscription(t *testing.T) {
	h := newChainTestHarness(t, true)
	exec := h.newExecutor(t, "", "")

	// Three inbounds:
	// - ch-vip: chain, access_tags=["vip"], remark="Tokyo"
	// - ch-noremark: chain, access_tags=["vip"], remark=""
	// - direct-open: direct, access_tags=[]
	require.NoError(t, exec.FastAddInbound("ch-vip", map[string]any{
		"protocol":     "vmess",
		"security":     "none",
		"port":         36510,
		"inbound_type": "chain",
		"chain_uri":    testChainVLESSURI,
		"access_tags":  []string{"vip"},
		"remark":       "Tokyo",
	}))
	require.NoError(t, exec.FastAddInbound("ch-noremark", map[string]any{
		"protocol":     "vmess",
		"security":     "none",
		"port":         36511,
		"inbound_type": "chain",
		"chain_uri":    "vless://11111111-1111-4111-8111-111111111111@jp.example.com:443?security=tls",
		"access_tags":  []string{"vip"},
	}))
	require.NoError(t, exec.FastAddInbound("direct-open", map[string]any{
		"protocol": "vmess",
		"security": "none",
		"port":     36512,
	}))

	// 1. User in node group "vip" with empty InboundTags -> allowed ONLY on direct-open (same-name trap).
	require.NoError(t, h.userMgr.AddUser(usermanager.AddUserRequest{
		Username:    "u_vip_notags",
		Password:    "pass1",
		Role:        "user",
		TargetGroup: "vip",
		InboundTags: nil,
	}))
	// 2. User in wrong group "basic" WITH matching InboundTags=["vip"] -> denied by Layer 1 everywhere.
	require.NoError(t, h.userMgr.AddUser(usermanager.AddUserRequest{
		Username:    "u_basic_viptag",
		Password:    "pass2",
		Role:        "user",
		TargetGroup: "basic",
		InboundTags: []string{"vip"},
	}))
	// 3. User with BOTH group "vip" AND InboundTags=["vip"] -> allowed everywhere.
	require.NoError(t, h.userMgr.AddUser(usermanager.AddUserRequest{
		Username:    "u_both",
		Password:    "pass3",
		Role:        "user",
		TargetGroup: "vip",
		InboundTags: []string{"vip"},
	}))

	for _, uname := range []string{"u_vip_notags", "u_basic_viptag", "u_both"} {
		u := h.userMgr.GetUserForSync(uname)
		require.NotNil(t, u)
		require.NoError(t, exec.handleUserEvent(usermanager.UserEvent{
			Type:     usermanager.UserEventAdd,
			Username: uname,
			User:     u,
		}))
	}

	chVIP := mustXrayInbound(t, exec, "ch-vip")
	chNoRem := mustXrayInbound(t, exec, "ch-noremark")
	directOpen := mustXrayInbound(t, exec, "direct-open")

	// Same-name tag isolation on UserEventAdd:
	assert.False(t, chVIP.hasAddedUser("u_vip_notags"))
	assert.False(t, chNoRem.hasAddedUser("u_vip_notags"))
	assert.True(t, directOpen.hasAddedUser("u_vip_notags"))

	// Layer 1 group mismatch denies all inbounds even with matching InboundTags:
	assert.False(t, chVIP.hasAddedUser("u_basic_viptag"))
	assert.False(t, chNoRem.hasAddedUser("u_basic_viptag"))
	assert.False(t, directOpen.hasAddedUser("u_basic_viptag"))

	// Authorized user added to all three:
	assert.True(t, chVIP.hasAddedUser("u_both"))
	assert.True(t, chNoRem.hasAddedUser("u_both"))
	assert.True(t, directOpen.hasAddedUser("u_both"))

	// Subscription gating + anti-collision NodeName checks:
	// Pass minimal UserSpec (only Username) to verify CanUserAccessInboundByUsername lookup!
	uNoTagsSpec := contracts.UserSpec{Username: "u_vip_notags"}
	_, err := chVIP.GetSub(contracts.SubscriptionRequest{User: uNoTagsSpec, Host: "edge.example.com", NodeName: "NodeA"})
	require.Error(t, err)

	subsNoTags, err := exec.GetUserSubscriptions(contracts.SubscriptionRequest{User: uNoTagsSpec, Host: "edge.example.com", NodeName: "NodeA"})
	require.NoError(t, err)
	require.Len(t, subsNoTags, 1)
	assert.Equal(t, "direct-open", subsNoTags[0].InboundTag)
	assert.Equal(t, "NodeA", subsNoTags[0].NodeName)

	// Anti-collision NodeName across chain with remark, chain without remark, and direct.
	subsBoth, err := exec.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     contracts.UserSpec{Username: "u_both"},
		Host:     "edge.example.com",
		NodeName: "NodeA",
	})
	require.NoError(t, err)
	require.Len(t, subsBoth, 3)
	nodeNames := map[string]string{}
	for _, s := range subsBoth {
		nodeNames[s.InboundTag] = s.NodeName
	}
	assert.Equal(t, "NodeA-Tokyo", nodeNames["ch-vip"])
	assert.Equal(t, "NodeA-ch-noremark", nodeNames["ch-noremark"])
	assert.Equal(t, "NodeA", nodeNames["direct-open"])

	// When req.NodeName == "":
	subsEmptyPrefix, err := exec.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "u_both"},
		Host: "edge.example.com",
	})
	require.NoError(t, err)
	emptyNames := map[string]string{}
	for _, s := range subsEmptyPrefix {
		emptyNames[s.InboundTag] = s.NodeName
	}
	assert.Equal(t, "Tokyo", emptyNames["ch-vip"])
	assert.Equal(t, "ch-noremark", emptyNames["ch-noremark"])
	assert.Equal(t, "direct-open", emptyNames["direct-open"])

	// Even if a stale port mapping existed for u_vip_notags on chVIP's port, GetSub denies via CanUserAccessInboundByUsername.
	h.userMgr.SetPortMappingForTest("u_vip_notags", chVIP.Port(), 36999)
	_, err = chVIP.GetSub(contracts.SubscriptionRequest{User: uNoTagsSpec, Host: "edge.example.com", NodeName: "NodeA"})
	require.Error(t, err)

	// UserEventUpdate: remove "vip" tag from u_both and grant "vip" tag to u_vip_notags.
	require.NoError(t, h.userMgr.SetUserInboundTags("u_both", nil))
	require.NoError(t, h.userMgr.SetUserInboundTags("u_vip_notags", []string{"vip"}))
	require.NoError(t, exec.handleUserEvent(usermanager.UserEvent{
		Type:     usermanager.UserEventUpdate,
		Username: "u_both",
		User:     h.userMgr.GetUserForSync("u_both"),
	}))
	require.NoError(t, exec.handleUserEvent(usermanager.UserEvent{
		Type:     usermanager.UserEventUpdate,
		Username: "u_vip_notags",
		User:     h.userMgr.GetUserForSync("u_vip_notags"),
	}))

	assert.False(t, chVIP.hasAddedUser("u_both"))
	assert.False(t, chNoRem.hasAddedUser("u_both"))
	assert.True(t, directOpen.hasAddedUser("u_both"))

	assert.True(t, chVIP.hasAddedUser("u_vip_notags"))
	assert.True(t, chNoRem.hasAddedUser("u_vip_notags"))
	assert.True(t, directOpen.hasAddedUser("u_vip_notags"))
}

func statErr(path string) error {
	_, err := os.Stat(path)
	return err
}

func TestGenerateDefaultConfig_UsesConfiguredGRPCPort(t *testing.T) {
	h := newChainTestHarness(t, false)
	cfgPath := filepath.Join(t.TempDir(), "custom-port.json")
	exec, err := NewExecutor(ExecutorConfig{
		BinaryPath:     "/nonexistent/xray",
		ConfigFilePath: cfgPath,
		ContainerType:  container.ContainerXray,
		GRPCAPIAddress: "127.0.0.1:54321",
		PortClaimer:    h.fm,
		StoreMgr:       h.storeMgr,
	})
	require.NoError(t, err)
	t.Cleanup(func() { exec.Close() })

	require.NoError(t, exec.EnsureConfig())
	generatedPath := exec.ConfigFile()
	t.Cleanup(func() { _ = os.Remove(generatedPath) })

	data, err := os.ReadFile(generatedPath)
	require.NoError(t, err)

	var cfg map[string]any
	require.NoError(t, json.Unmarshal(data, &cfg))
	inbounds, ok := cfg["inbounds"].([]any)
	require.True(t, ok)
	foundAPI := false
	for _, raw := range inbounds {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if m["tag"] == "api" {
			foundAPI = true
			assert.Equal(t, float64(54321), m["port"], "api inbound port should match configured GRPCAPIAddress")
		}
	}
	assert.True(t, foundAPI, "generated config must include api inbound")
}

func TestRestore_EvictsStaleInboundsAndClampsChainListen(t *testing.T) {
	h := newChainTestHarness(t, true)
	exec := h.newExecutor(t, "", "")

	require.NoError(t, h.userMgr.AddUser(usermanager.AddUserRequest{
		Username:    "alice",
		Password:    "pass123",
		Role:        "user",
		TargetGroup: "vip",
		InboundTags: []string{"vip"},
	}))

	require.NoError(t, exec.FastAddInbound("a-chain", map[string]any{
		"protocol":     "vmess",
		"security":     "none",
		"port":         36520,
		"inbound_type": "chain",
		"chain_uri":    testChainVLESSURI,
		"access_tags":  []string{"vip"},
	}))
	require.NoError(t, exec.FastAddInbound("b-stale", map[string]any{
		"protocol":    "vless",
		"security":    "tls",
		"self_signed": true,
		"port":        36521,
	}))
	exec.reconcileUsers()
	require.Len(t, h.fm.GetAllRules(), 2)

	staleCerts := slices.Clone(mustXrayInbound(t, exec, "b-stale").tempCertFiles)
	require.Len(t, staleCerts, 2)

	// Tamper persisted native_json for a-chain to have "listen": "0.0.0.0" to verify Restore clamps it to 127.0.0.1.
	recA, err := h.storeMgr.InboundStore().Get("a-chain")
	require.NoError(t, err)
	var nativeA map[string]any
	require.NoError(t, json.Unmarshal(recA.NativeJSON, &nativeA))
	nativeA["listen"] = "0.0.0.0"
	recA.NativeJSON, err = json.Marshal(nativeA)
	require.NoError(t, err)
	require.NoError(t, h.storeMgr.InboundStore().Save(recA))

	// Simulate b-stale failing/disappearing on restart: remove from store and clear xray runtime.
	require.NoError(t, h.storeMgr.InboundStore().Delete("b-stale"))
	h.mu.Lock()
	h.grpcInbounds = make(map[string]*proxyman.InboundHandlerConfig)
	h.cliOutbounds = make(map[string]json.RawMessage)
	h.cliRules = make(map[string]string)
	h.events = nil
	h.mu.Unlock()

	require.NoError(t, exec.Restore(context.Background()))

	// a-chain restored with listen clamped to 127.0.0.1.
	xiA := mustXrayInbound(t, exec, "a-chain")
	assert.Equal(t, "127.0.0.1", xiA.ListenAddr())
	var restoredNativeA map[string]any
	require.NoError(t, json.Unmarshal(xiA.nativeJSON, &restoredNativeA))
	assert.Equal(t, "127.0.0.1", restoredNativeA["listen"])

	// b-stale evicted from memory, forward rules released, temp cert files deleted.
	assertNoInbound(t, exec, "b-stale")
	assert.Nil(t, h.fm.GetRule("xray:b-stale:alice"), "stale inbound forward rule must be released")
	assert.NotNil(t, h.fm.GetRule("xray:a-chain:alice"), "restored inbound forward rule must remain active")
	for _, f := range staleCerts {
		assert.True(t, os.IsNotExist(statErr(f)), "stale temp cert file %s should be cleaned", f)
	}

	// Now simulate a persisted inbound that fails grpc AddInbound during Restore:
	// Restore must return an error AND evict it from e.inbounds / release its forward rules.
	h.mu.Lock()
	h.grpcInbounds = make(map[string]*proxyman.InboundHandlerConfig)
	h.failAddInbErr = errors.New("grpc rejected inbound")
	h.mu.Unlock()

	err = exec.Restore(context.Background())
	require.Error(t, err)
	assertNoInbound(t, exec, "a-chain")
	assert.Nil(t, h.fm.GetRule("xray:a-chain:alice"), "failed-to-restore inbound must have forward rule released")
}

func TestConcurrency_FastAddSameTagChainAndLifecycleOps(t *testing.T) {
	t.Run("concurrent same-tag FastAddInbound preserves winner routing and outbound", func(t *testing.T) {
		h := newChainTestHarness(t, true)
		exec := h.newExecutor(t, "", "")

		// Inject a tiny delay in CLI calls so unsynchronized callers would interleave rmrules/rmo with winner's ado/adrules.
		baseRunner := h.cliRunner()
		exec.routingRunner = func(ctx context.Context, binaryPath string, args []string, stdin []byte) ([]byte, error) {
			time.Sleep(5 * time.Millisecond)
			return baseRunner(ctx, binaryPath, args, stdin)
		}

		const workers = 6
		var successes atomic.Int32
		var failures atomic.Int32
		var wg sync.WaitGroup
		startCh := make(chan struct{})

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-startCh
				err := exec.FastAddInbound("race-chain", map[string]any{
					"protocol":     "vmess",
					"security":     "none",
					"port":         36600 + idx,
					"inbound_type": "chain",
					"chain_uri":    testChainVLESSURI,
				})
				if err == nil {
					successes.Add(1)
				} else {
					failures.Add(1)
				}
			}(i)
		}
		close(startCh)
		wg.Wait()

		assert.Equal(t, int32(1), successes.Load(), "exactly one concurrent same-tag FastAddInbound must succeed")
		assert.Equal(t, int32(workers-1), failures.Load(), "all losing concurrent callers must return error")

		// Winner's inbound, outbound, and routing rule MUST all remain intact (no direct traffic leak).
		_ = mustXrayInbound(t, exec, "race-chain")
		h.mu.Lock()
		_, hasInb := h.grpcInbounds["race-chain"]
		_, hasOut := h.cliOutbounds["__chain_out__:race-chain"]
		outTag, hasRule := h.cliRules["__chain_rule__:race-chain"]
		h.mu.Unlock()
		assert.True(t, hasInb, "winner grpc inbound missing")
		assert.True(t, hasOut, "winner chain outbound was clobbered by concurrent loser")
		assert.True(t, hasRule, "winner chain routing rule was clobbered by concurrent loser")
		assert.Equal(t, "__chain_out__:race-chain", outTag)
	})

	t.Run("concurrent FastAddInbound, SetInboundAccessTags, RemoveInboundConfig, and handleUserEvent", func(t *testing.T) {
		h := newChainTestHarness(t, true)
		exec := h.newExecutor(t, "", "")

		require.NoError(t, h.userMgr.AddUser(usermanager.AddUserRequest{
			Username:    "alice",
			Password:    "pass123",
			Role:        "user",
			TargetGroup: "vip",
			InboundTags: []string{"vip"},
		}))
		require.NoError(t, exec.FastAddInbound("shared-chain", map[string]any{
			"protocol":     "vmess",
			"security":     "none",
			"port":         36620,
			"inbound_type": "chain",
			"chain_uri":    testChainVLESSURI,
			"access_tags":  []string{"vip"},
		}))

		var wg sync.WaitGroup
		tagSets := [][]string{{"vip"}, {"other"}, {"vip", "other"}, nil}

		// Goroutines mutating tags on shared-chain
		for g := 0; g < 3; g++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for i := 0; i < 10; i++ {
					_ = exec.SetInboundAccessTags("shared-chain", tagSets[(id+i)%len(tagSets)])
					_, _ = exec.GetInboundMeta("shared-chain")
				}
			}(g)
		}

		// Goroutines adding and removing distinct chain inbounds
		for g := 0; g < 3; g++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				tag := fmt.Sprintf("dyn-chain-%d", id)
				for i := 0; i < 5; i++ {
					if err := exec.FastAddInbound(tag, map[string]any{
						"protocol":     "vmess",
						"security":     "none",
						"port":         36630 + id*10 + i,
						"inbound_type": "chain",
						"chain_uri":    testChainVLESSURI,
					}); err == nil {
						_ = exec.RemoveInboundConfig(tag)
					}
				}
			}(g)
		}

		// Goroutine firing user events concurrently
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				u := h.userMgr.GetUserForSync("alice")
				if u != nil {
					_ = exec.handleUserEvent(usermanager.UserEvent{
						Type:     usermanager.UserEventUpdate,
						Username: "alice",
						User:     u,
					})
				}
			}
		}()

		wg.Wait()
		// shared-chain should still be healthy and consistent.
		_ = mustXrayInbound(t, exec, "shared-chain")
	})
}
