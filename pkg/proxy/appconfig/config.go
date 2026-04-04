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
}

// StaticNodeConfig is a statically configured peer node used for initial cluster discovery.
type StaticNodeConfig struct {
	Name string `yaml:"name" json:"name"`
	Host string `yaml:"host" json:"host"`
	Port int32  `yaml:"port" json:"port"`
}

// ClusterConfig holds cluster membership and authentication configuration.
type ClusterConfig struct {
	// Name is the cluster name. All nodes in the same cluster must share this value.
	Name string `yaml:"name" json:"name"`
	// Token is the shared secret used to authenticate cluster members and encrypt gRPC traffic.
	Token string `yaml:"token" json:"token"`
	// CenterNodeHost is the optional center node host for initial node discovery.
	// Leave empty to operate in fully decentralized mode.
	CenterNodeHost string `yaml:"center_node_host" json:"center_node_host"`
	// CenterNodePort is the center node gRPC port.
	CenterNodePort int `yaml:"center_node_port" json:"center_node_port"`
	// StaticNodes is an optional list of known peer nodes for bootstrapping discovery
	// without a center node.
	StaticNodes []StaticNodeConfig `yaml:"static_nodes" json:"static_nodes"`
}

// NodeConfig holds configuration shared by both EndNode and CenterNode.
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
}

// CenterNodeConfig holds Center Node specific configuration.
type CenterNodeConfig struct {
	NodeConfig `yaml:",inline"`
}

// ClusterUserConfig controls the ClusterUser sync layer and placement controller behaviour.
type ClusterUserConfig struct {
	// Enabled enables the ClusterUser sync layer and placement controller. Default: false.
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
		Enabled:            false,
		SyncIntervalSec:    5,
		BootstrapFromLocal: true,
		DefaultGroup:       "default",
	}
}

// AppConfig is the top-level configuration for the v2raymg proxy refactor stack.
// It is loaded from a YAML or JSON file and used to initialize all subsystems.
type AppConfig struct {
	// NodeType is the node role: "center" or "end". Default: "end".
	NodeType     string                         `yaml:"node_type"    json:"node_type"`
	Log          LogConfig                      `yaml:"log"          json:"log"`
	Store        StoreConfig                    `yaml:"store"        json:"store"`
	Forward      forward.PortAllocatorConfig    `yaml:"forward"      json:"forward"`
	CertMgmt     certmgmtservice.Config         `yaml:"cert_mgmt"    json:"cert_mgmt"`
	Containers   container.ContainerMgrConfig   `yaml:"containers"   json:"containers"`
	Subscription SubscriptionConfig             `yaml:"subscription" json:"subscription"`
	EndNode      EndNodeConfig                  `yaml:"end_node"     json:"end_node"`
	CenterNode   CenterNodeConfig               `yaml:"center_node"  json:"center_node"`
	ClusterUser  ClusterUserConfig              `yaml:"cluster_user" json:"cluster_user"`
}
