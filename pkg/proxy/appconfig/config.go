package appconfig

import (
	"os"

	certmgmtservice "github.com/lureiny/v2raymg/pkg/certmgmt/service"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

// LogConfig holds logging configuration.
type LogConfig struct {
	// Level is the minimum log level: debug, info, warn, error. Default: info.
	Level string `yaml:"level" json:"level"`
	// Format is the output format: text or json. Default: text.
	Format string `yaml:"format" json:"format"`
	// Output is the output target: stdout, stderr, or a file path. Default: stdout.
	Output string `yaml:"output" json:"output"`
	// AddSource includes caller file and line number in log output. Default: false.
	AddSource bool `yaml:"add_source" json:"add_source"`
}

// ApplyToLogger builds a log.Logger from LogConfig.
// Zero values fall back to defaults (info level, text format, stdout).
func (c LogConfig) ApplyToLogger() log.Logger {
	var opts []log.Option

	switch c.Level {
	case "debug":
		opts = append(opts, log.WithLevel(log.LevelDebug))
	case "warn":
		opts = append(opts, log.WithLevel(log.LevelWarn))
	case "error":
		opts = append(opts, log.WithLevel(log.LevelError))
	default:
		opts = append(opts, log.WithLevel(log.LevelInfo))
	}

	switch c.Format {
	case "json":
		opts = append(opts, log.WithFormat(log.FormatJSON))
	default:
		opts = append(opts, log.WithFormat(log.FormatText))
	}

	switch c.Output {
	case "", "stdout":
		opts = append(opts, log.WithOutput(os.Stdout))
	case "stderr":
		opts = append(opts, log.WithOutput(os.Stderr))
	default:
		// treat as file path
		if f, err := os.OpenFile(c.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			opts = append(opts, log.WithOutput(f))
		}
	}

	if c.AddSource {
		opts = append(opts, log.WithAddSource(true))
	}

	return log.New(opts...)
}

// StoreConfig holds persistence configuration.
type StoreConfig struct {
	// DSN is the SQLite database file path (e.g. "/var/lib/v2raymg/data.db").
	DSN string `yaml:"dsn" json:"dsn"`
}

// SubscriptionConfig holds subscription-related configuration.
type SubscriptionConfig struct {
	// RemoteURLs is a list of remote subscription URLs to fetch and merge.
	// Each URL should return a base64-encoded list of proxy URIs (common format).
	RemoteURLs []string `yaml:"remote_urls" json:"remote_urls"`

	// ClashTemplateURLs is the ordered list of external Clash sub-converter
	// template services queried (with a throwaway node) to obtain a full Clash
	// config skeleton (proxy-groups/rules) that generated proxies are injected
	// into. They are tried in order; the first that responds wins. Each entry
	// must contain a single "%s" placeholder, replaced with the URL-encoded
	// trigger subscription. There is deliberately no built-in default — the
	// recommended public services live in config.example.yaml. When this list is
	// empty (or every URL fails), /sub degrades to a built-in minimal template
	// instead of failing the request.
	ClashTemplateURLs []string `yaml:"clash_template_urls" json:"clash_template_urls"`
	// EnableUserInfoHeader controls whether the /sub response carries the
	// Subscription-Userinfo header (upload/download/total/expire). Default: true.
	// When enabled, upload/download are aggregated from all target nodes; total
	// and expire are read from the access node only (see sub_handler.go for the
	// caveat about per-node config divergence).
	EnableUserInfoHeader bool `yaml:"enable_userinfo_header" json:"enable_userinfo_header"`

	// UserinfoHeaderFormat customises the Subscription-Userinfo header schema.
	// ${var} placeholders are expanded against a fixed variable set; see the
	// http handler (sub_userinfo_format.go) for the catalogue. When empty the
	// handler falls back to its built-in default.
	UserinfoHeaderFormat string `yaml:"userinfo_header_format" json:"userinfo_header_format"`
}

// StaticNodeConfig is a statically configured peer node used for initial cluster discovery.
type StaticNodeConfig struct {
	Name string `yaml:"name" json:"name"`
	Host string `yaml:"host" json:"host"`
	Port int32  `yaml:"port" json:"port"`
}

// ClusterConfig holds cluster membership and authentication configuration.
type ClusterConfig struct {
	// Token is the shared secret that both authenticates cluster members and keys
	// (via HKDF) all end<->end gRPC traffic. It is the ONLY membership boundary —
	// the former `name` field was a second, cosmetic factor and was removed in
	// 2026-07 along with the center node.
	Token string `yaml:"token" json:"token"`
	// HeartbeatIntervalSec is how often this node heartbeats to every cluster peer.
	// Default 10s when unset (0).
	//
	// It is the clock the whole cluster plane runs on: node liveness
	// (cluster.NodeTimeOut, 60s), directory convergence, and cluster-user sync all
	// advance one step per interval. Raising it cuts steady-state chatter on large
	// clusters at the cost of slower failure detection — do not raise it past
	// NodeTimeOut/2 or peers will expire between beats. Integration tests drop it
	// to 1s so multi-round convergence assertions finish in seconds.
	HeartbeatIntervalSec int `yaml:"heartbeat_interval_sec" json:"heartbeat_interval_sec"`
	// NodeSumSync enables node-directory delta sync on the end<->end heartbeat.
	// When on (the default), a heartbeat carries only a digest of the sender's
	// node set and the full directory is exchanged just once per disagreement,
	// instead of on every tick — the difference between O(N) and O(1) bytes per
	// heartbeat, i.e. O(N^2) and O(N) per node per round.
	//
	// Turning it off restores the previous behaviour on both sides (this node
	// stops sending a digest, and answers every heartbeat with the full map),
	// which is safe to do on one node at a time: a peer that receives no digest
	// is treated as legacy and keeps getting the full directory.
	NodeSumSync bool `yaml:"node_sum_sync" json:"node_sum_sync"`
	// StaticNodes seeds discovery. It is the ONLY bootstrap path since the center
	// node was removed in 2026-07, and it is strictly better than the center was:
	// a static peer is never reclaimed by the node timeout (see Node.isLocal), and
	// registration is bidirectional, so a seed learns the joining node at once
	// rather than on its own next tick. A node with no static peers and an empty
	// persisted directory can never discover anyone.
	StaticNodes []StaticNodeConfig `yaml:"static_nodes" json:"static_nodes"`
}

// NodeConfig holds the node identity and listen configuration.
type NodeConfig struct {
	// Listen is the network interface to bind (e.g. "0.0.0.0" or "127.0.0.1").
	Listen string `yaml:"listen" json:"listen"`
	// RpcPort is the gRPC server port.
	RpcPort int `yaml:"rpc_port" json:"rpc_port"`
	// Name is the unique node name within the cluster.
	Name string `yaml:"name" json:"name"`
	// ProxyHost is the public hostname/IP used in subscription links and metric labels.
	ProxyHost string `yaml:"proxy_host" json:"proxy_host"`
}

// PingNodeSource defines a source for loading ping nodes.
type PingNodeSource struct {
	// Type is the source type: "file" or "remote".
	Type string `yaml:"type" json:"type"`
	// Source is the file path (for "file") or URL (for "remote").
	Source string `yaml:"source" json:"source"`
	// UpdateInterval is the reload interval in seconds for "remote" sources.
	// Default: 300 (5 minutes). Ignored for "file" type.
	UpdateInterval int `yaml:"update_interval" json:"update_interval"`
}

// PingConfig holds ping checker configuration.
type PingConfig struct {
	// EnableICMPPing enables local ICMP ping probing (requires root/privileges).
	EnableICMPPing bool `yaml:"enable_icmp_ping" json:"enable_icmp_ping"`
	// ICMPPingInterval is the interval between ICMP pings in seconds. Default: 5.
	ICMPPingInterval int `yaml:"icmp_ping_interval" json:"icmp_ping_interval"`
	// ICMPPingTimeout is the ICMP ping timeout in seconds. Default: 1.
	ICMPPingTimeout int `yaml:"icmp_ping_timeout" json:"icmp_ping_timeout"`
	// EnableTCPPing enables TCP-based latency probing.
	EnableTCPPing bool `yaml:"enable_tcp_ping" json:"enable_tcp_ping"`
	// TCPPingInterval is the interval between TCP pings in seconds. Default: 5.
	TCPPingInterval int `yaml:"tcp_ping_interval" json:"tcp_ping_interval"`
	// TCPPingTimeout is the TCP ping timeout in seconds. Default: 1.
	TCPPingTimeout int `yaml:"tcp_ping_timeout" json:"tcp_ping_timeout"`
	// NodeSources defines where to load ping nodes from.
	// Multiple sources are supported; later sources override earlier ones by host.
	// Default: [{type: "file", source: "./config/ping_nodes.yaml"}].
	NodeSources []PingNodeSource `yaml:"node_sources" json:"node_sources"`
}

// EndNodeConfig holds End Node specific configuration.
type EndNodeConfig struct {
	NodeConfig `yaml:",inline"`
	// Cluster holds cluster membership settings.
	Cluster ClusterConfig `yaml:"cluster" json:"cluster"`
	// OnlyGateway enables gateway-only mode: the node forwards traffic but does not
	// expose proxy services itself.
	OnlyGateway bool `yaml:"only_gateway" json:"only_gateway"`
	// Ping holds ping checker configuration.
	Ping PingConfig `yaml:"ping" json:"ping"`
	// HttpPort is the HTTP management interface port.
	HttpPort int `yaml:"http_port" json:"http_port"`
	// HttpListen overrides the listen address for the HTTP server only.
	// When empty, falls back to the shared Listen address.
	// Use "127.0.0.1" to restrict HTTP to localhost while gRPC listens on 0.0.0.0.
	HttpListen string `yaml:"http_listen" json:"http_listen"`
	// HttpToken is the HTTP management interface authentication token.
	HttpToken string `yaml:"http_token" json:"http_token"`
	// JWTSecret is the signing key for JWT tokens issued by POST /login.
	// Should be a random string of at least 32 bytes. Do not commit to version control.
	JWTSecret string `yaml:"jwt_secret" json:"jwt_secret"`
	// JWTExpireHours is the JWT token expiry in hours. Default: 24.
	JWTExpireHours int `yaml:"jwt_expire_hours" json:"jwt_expire_hours"`
	// EnablePrometheus enables the Prometheus metrics endpoint.
	EnablePrometheus bool `yaml:"enable_prometheus" json:"enable_prometheus"`
	// MonitorInterfaces is an optional list of network interface names to use for
	// bandwidth speed monitoring (e.g. ["eth0", "ens3"]).
	// When empty, the interface with the default route is detected automatically.
	// If auto-detection fails, all non-loopback interfaces are summed (fallback).
	MonitorInterfaces []string `yaml:"monitor_interfaces" json:"monitor_interfaces"`
}

// ClusterUserConfig controls the ClusterUser sync layer and placement controller behaviour.
type ClusterUserConfig struct {
	// Enabled enables the ClusterUser sync layer and placement controller. Default: true.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// SyncIntervalSec is the heartbeat delta-sync period in seconds. Default: 5.
	SyncIntervalSec int `yaml:"sync_interval_sec" json:"sync_interval_sec"`
	// BootstrapFromLocal controls whether the node imports existing usermgr users
	// when cluster_users is empty for the first time. Default: true.
	BootstrapFromLocal bool `yaml:"bootstrap_from_local" json:"bootstrap_from_local"`
	// DefaultGroup is the default group name. Default: "default".
	DefaultGroup string `yaml:"default_group" json:"default_group"`
}

// DefaultClusterUserConfig returns the default values for ClusterUserConfig.
func DefaultClusterUserConfig() ClusterUserConfig {
	return ClusterUserConfig{
		Enabled:            true,
		SyncIntervalSec:    5,
		BootstrapFromLocal: true,
		DefaultGroup:       "default",
	}
}

// AppConfig is the top-level configuration for the v2raymg proxy refactor stack.
// It is loaded from a YAML or JSON file and used to initialize all subsystems.
type AppConfig struct {
	// NodeType is the node role: "center" or "end". Default: "end".
	NodeType     string                       `yaml:"node_type"    json:"node_type"`
	Log          LogConfig                    `yaml:"log"          json:"log"`
	Store        StoreConfig                  `yaml:"store"        json:"store"`
	Forward      forward.PortAllocatorConfig  `yaml:"forward"      json:"forward"`
	CertMgmt     certmgmtservice.Config       `yaml:"cert_mgmt"    json:"cert_mgmt"`
	Containers   container.ContainerMgrConfig `yaml:"containers"   json:"containers"`
	Subscription SubscriptionConfig           `yaml:"subscription" json:"subscription"`
	EndNode      EndNodeConfig                `yaml:"end_node"     json:"end_node"`
	ClusterUser  ClusterUserConfig            `yaml:"cluster_user" json:"cluster_user"`
}
