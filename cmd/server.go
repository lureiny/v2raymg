package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lureiny/v2raymg/pkg/buildinfo"
	certmgmtservice "github.com/lureiny/v2raymg/pkg/certmgmt/service"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/collecter"
	"github.com/lureiny/v2raymg/pkg/http"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	_ "github.com/lureiny/v2raymg/pkg/proxy/containers/hysteria" // register hysteria factory
	_ "github.com/lureiny/v2raymg/pkg/proxy/containers/mihomo"   // register mihomo factory
	_ "github.com/lureiny/v2raymg/pkg/proxy/containers/snell"    // register snell factory
	_ "github.com/lureiny/v2raymg/pkg/proxy/containers/xray"     // register xray factory
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
	converter "github.com/lureiny/v2raymg/pkg/proxy/core/subscription/converter" // register converters + configure clash template URLs
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/lureiny/v2raymg/pkg/rpc/server"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start v2raymg server",
	Run:   startServer,
}

var serverConfig string
var serverMigrate bool

func init() {
	serverCmd.Flags().StringVar(&serverConfig, "conf", "/usr/local/etc/v2raymg/config.yaml", "path to config file")
	serverCmd.Flags().BoolVar(&serverMigrate, "migrate", false, "migrate legacy config to new format and overwrite the config file, then exit")
}

// printBanner prints the ASCII logo and build info to stdout.
func printBanner() {
	fmt.Print(`
 ██╗   ██╗██████╗ ██████╗  █████╗ ██╗   ██╗███╗   ███╗ ██████╗
 ██║   ██║╚════██╗██╔══██╗██╔══██╗╚██╗ ██╔╝████╗ ████║██╔════╝
 ██║   ██║ █████╔╝██████╔╝███████║ ╚████╔╝ ██╔████╔██║██║  ███╗
 ╚██╗ ██╔╝██╔═══╝ ██╔══██╗██╔══██║  ╚██╔╝  ██║╚██╔╝██║██║   ██║
  ╚████╔╝ ███████╗██║  ██║██║  ██║   ██║   ██║ ╚═╝ ██║╚██████╔╝
   ╚═══╝  ╚══════╝╚═╝  ╚═╝╚═╝  ╚═╝   ╚═╝   ╚═╝     ╚═╝ ╚═════╝
`)
	fmt.Printf("  Version   : %s\n", buildinfo.Version)
	fmt.Printf("  Commit    : %s\n", buildinfo.Commit)
	fmt.Printf("  Built at  : %s\n", buildinfo.BuildTime)
	fmt.Println()
}

func startServer(cmd *cobra.Command, args []string) {
	printBanner()
	cfg, err := appconfig.LoadFromFile(serverConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	// --migrate: save converted config back to file and exit (skip validation,
	// old configs may not have jwt_secret yet).
	if serverMigrate {
		if err := appconfig.SaveToFile(cfg, serverConfig); err != nil {
			fmt.Fprintf(os.Stderr, "migrate: write config: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "config migrated and saved to %s\n", serverConfig)
		return
	}

	// Validate configuration before starting any subsystems (fail-fast).
	if err := appconfig.Validate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config validation: %v\n", err)
		os.Exit(1)
	}

	logger := cfg.Log.ApplyToLogger()
	log.SetDefault(logger)

	for _, w := range appconfig.WarnRuntimeConfig(cfg) {
		log.Warn("config", "warning", w)
	}

	// Configure the external Clash sub-converter template services used when
	// building /sub Clash configs (subscription.clash_template_urls). An empty
	// list is valid — /sub then always uses the built-in minimal fallback
	// template rather than failing.
	converter.SetClashTemplateURLs(cfg.Subscription.ClashTemplateURLs)

	runEndNode(cfg)
}

func runEndNode(cfg *appconfig.AppConfig) {
	// 1. Store
	storeMgr, err := store.NewStoreManager(cfg.Store.DSN, migrations.All)
	if err != nil {
		log.Error("init store failed", "err", err)
		os.Exit(1)
	}
	defer storeMgr.Close()

	// 2. Forward Manager
	forwardMgr, err := forward.NewDefaultForwardManager(cfg.Forward)
	if err != nil {
		log.Error("init forward manager failed", "err", err)
		os.Exit(1)
	}
	defer forwardMgr.Close()

	// 3. User Manager
	// 3a. Init login_password for existing users that don't have one yet (blocking).
	//     Must run BEFORE NewUserManagerWithStore so that users are loaded with
	//     login_password already populated — no restart required.
	if err := storeMgr.InitLoginPasswords(auth.HashLoginPassword); err != nil {
		log.Error("init login passwords failed", "err", err)
		os.Exit(1)
	}
	userMgr, err := usermanager.NewUserManagerWithStore(forwardMgr, storeMgr, cfg.EndNode.Name)
	if err != nil {
		log.Error("init user manager failed", "err", err)
		os.Exit(1)
	}
	// 3b. Inject the login password hasher so AddUser/UpdateUser keep login_password in sync.
	userMgr.SetLoginPasswordHasher(auth.HashLoginPassword)

	// 4. Container Manager (deferred to after certMgr init — see step 7)

	// 6. Cluster Manager + LocalNode
	clusterMgr, localNode, err := cluster.NewEndNodeClusterManagerFromConfig(
		cluster.ClusterInitConfig{
			ClusterToken: cfg.EndNode.Cluster.Token,
			StaticNodes:  convertStaticNodes(cfg.EndNode.Cluster.StaticNodes),
		},
		cluster.NodeInitConfig{
			Name: cfg.EndNode.Name,
			Host: cfg.EndNode.ProxyHost,
			Port: int32(cfg.EndNode.RpcPort),
		},
	)
	if err != nil {
		log.Error("init cluster manager failed", "err", err)
		os.Exit(1)
	}

	// 7. Cert Manager + lifecycle context for background tasks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	certMgr := certmgmtservice.NewManager(cfg.CertMgmt)
	// Start auto-renew: scans every minute and renews certs inside the renewal
	// window (RenewBeforeHours, default 24h) in place. Proxy cores hot-reload the
	// renewed cert files from disk on their own (xray ~1h ticker / mihomo fswatch /
	// hysteria per-handshake), so no container restart is issued here by design.
	certMgr.StartAutoRenew(ctx, 0)

	// 4 (cont). Container Manager — needs certMgr and HTTPPort
	containerMgr := container.NewContainerMgr(storeMgr, container.BuildOptions{
		UserManager: userMgr,
		CertManager: certMgr,
		HTTPPort:    cfg.EndNode.HttpPort,
		ProxyHost:   cfg.EndNode.ProxyHost,
	})
	if err := containerMgr.LoadFromConfig(cfg.Containers); err != nil {
		log.Error("load container config failed", "err", err)
		os.Exit(1)
	}

	// 5. Subscription Manager
	subMgr := subscription.NewManager(containerMgr, userMgr)

	// 8. Collectors
	statsCol := usermanager.NewBandwidthStatsCollector(userMgr)
	pingCol := collecter.NewPingCollector(collecter.PingCollectorConfig{
		Host:             cfg.EndNode.ProxyHost,
		EnableICMPPing:   cfg.EndNode.Ping.EnableICMPPing,
		EnableTCPPing:    cfg.EndNode.Ping.EnableTCPPing,
		ICMPPingInterval: cfg.EndNode.Ping.ICMPPingInterval,
		ICMPPingTimeout:  cfg.EndNode.Ping.ICMPPingTimeout,
		TCPPingInterval:  cfg.EndNode.Ping.TCPPingInterval,
		TCPPingTimeout:   cfg.EndNode.Ping.TCPPingTimeout,
		NodeSources:      cfg.EndNode.Ping.NodeSources,
	})
	nodeMetricCol := collecter.DefaultNodeCollector()

	// 8b. Persisted node directory. Loaded BEFORE the heartbeat loop starts so the
	// first round already retries the peers this node knew when it went down —
	// which is the whole point: peers drop an unreachable node (address included)
	// after cluster.NodeTimeOut, so without this a restart longer than a minute
	// leaves the node isolated.
	nodeStore := store.NewSQLiteClusterNodesStore(storeMgr.DB())
	loadPersistedNodes(nodeStore, clusterMgr)

	// 9. End Node RPC Server
	rpcServer := server.GetEndNodeServer()
	rpcServer.Init(
		cfg.EndNode,
		certMgr,
		clusterMgr,
		userMgr,
		containerMgr,
		subMgr,
		statsCol,
		pingCol,
		nodeMetricCol,
		nodeStoreAdapter{nodeStore},
	)

	// 9a. Cluster sync — enable on UserManager when configured.
	if cfg.ClusterUser.Enabled {
		ngStore := store.NewSQLiteNodeGroupsStore(storeMgr.DB())
		userMgr.EnableClusterSync(cfg.ClusterUser.DefaultGroup, ngStore)

		// Seed node groups if empty.
		if groups, _ := ngStore.List(); len(groups) == 0 {
			_ = ngStore.Set([]string{cfg.ClusterUser.DefaultGroup})
		}

		// Backfill cluster fields for users loaded before cluster sync was enabled.
		userMgr.BackfillClusterFields()

		log.Info("cluster sync enabled",
			"default_group", cfg.ClusterUser.DefaultGroup,
		)
	}

	// 10. HTTP Server
	httpListen := cfg.EndNode.HttpListen
	if httpListen == "" {
		httpListen = "127.0.0.1"
	}
	httpServer := http.NewHttpServer()
	httpServer.Init(
		http.HttpServerConfig{
			Listen:                  httpListen,
			Port:                    cfg.EndNode.HttpPort,
			Token:                   cfg.EndNode.HttpToken,
			Name:                    cfg.EndNode.Name,
			JWTSecret:               cfg.EndNode.JWTSecret,
			JWTExpireHours:          cfg.EndNode.JWTExpireHours,
			EnableSubUserInfoHeader: cfg.Subscription.EnableUserInfoHeader,
			SubUserInfoHeaderFormat: cfg.Subscription.UserinfoHeaderFormat,
		},
		localNode,
		clusterMgr,
		certMgr,
		userMgr,
		cfg.ClusterUser.Enabled,
	)
	if cfg.EndNode.EnablePrometheus {
		http.RegisterPrometheus(httpServer)
	}

	// Bind both core listeners synchronously BEFORE serving either, so a bind
	// failure fails fast (os.Exit) instead of leaving a zombie node with no
	// management/RPC plane. Binding both first also avoids registering with the
	// center over RPC and then exiting because the HTTP bind failed.
	rpcListener, err := rpcServer.Listen()
	if err != nil {
		log.Error("bind rpc server failed", "err", err)
		os.Exit(1)
	}
	httpListener, err := httpServer.Listen()
	if err != nil {
		if rpcListener != nil {
			rpcListener.Close()
		}
		log.Error("bind http server failed", "err", err)
		os.Exit(1)
	}
	if rpcListener != nil { // nil = RPC disabled (address not configured)
		go rpcServer.Serve(rpcListener)
	}
	go httpServer.Serve(httpListener)

	// 11. Start background tasks
	userMgr.StartTrafficStats(0)
	userMgr.StartMaintenance(0)

	// 12. Start containers
	if err := containerMgr.StartAll(ctx); err != nil {
		log.Error("start containers failed", "err", err)
	}

	// 13. Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigCh
	log.Info("shutting down", "signal", sig.String())

	cancel() // stop auto-renew and other ctx-bound background tasks
	containerMgr.StopAll()
}

func convertStaticNodes(nodes []appconfig.StaticNodeConfig) []cluster.StaticNode {
	out := make([]cluster.StaticNode, len(nodes))
	for i, n := range nodes {
		out[i] = cluster.StaticNode{
			Name: n.Name,
			Host: n.Host,
			Port: n.Port,
		}
	}
	return out
}

// nodeStoreAdapter adapts the SQLite store to the narrow interface the RPC layer
// declares, so pkg/rpc/server never imports pkg/store.
type nodeStoreAdapter struct {
	s *store.SQLiteClusterNodesStore
}

func (a nodeStoreAdapter) ListNames() ([]string, error) {
	nodes, err := a.s.List()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	return names, nil
}

func (a nodeStoreAdapter) Upsert(name, host string, port int32) error {
	return a.s.Upsert(store.ClusterNode{Name: name, Host: host, Port: port})
}

func (a nodeStoreAdapter) Delete(name string) error { return a.s.Delete(name) }

// loadPersistedNodes seeds the in-memory directory from the database.
//
// Only CreateTime is stamped — deliberately NOT the heartbeat timestamps.
// IsValid() accepts a node whose CreateTime is recent, so the node gets a full
// NodeTimeOut window to be re-registered; but IsCompleteRegister() requires both
// heartbeat timestamps and does NOT verify any token, so back-dating those would
// make a freshly loaded peer look authenticated and put it straight into the set
// this node advertises to others — publishing a peer it has not talked to yet.
func loadPersistedNodes(st *store.SQLiteClusterNodesStore, clusterMgr *cluster.EndNodeClusterManager) {
	nodes, err := st.List()
	if err != nil {
		log.Error("load persisted cluster nodes failed", "err", err)
		return
	}
	loaded := 0
	for _, n := range nodes {
		if clusterMgr.Get(n.Name) != nil {
			continue // already present as a static peer; config wins
		}
		clusterMgr.Add(&cluster.Node{
			Node:       &proto.Node{Name: n.Name, Host: n.Host, Port: n.Port},
			CreateTime: time.Now().Unix(),
		})
		loaded++
	}
	if loaded > 0 {
		log.Info("loaded persisted cluster nodes", "count", loaded)
	}
}
