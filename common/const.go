package common

// config
const (
	// proxy
	ConfigProxyConfigFile  = "proxy.config_file"
	ConfigProxyVersion     = "proxy.version"
	ConfigProxyDefaultTags = "proxy.default_tags"
	ConfigProxyHost        = "proxy.host"
	ConfigProxyPort        = "proxy.port"
	ConfigProxyAdaptive    = "proxy.adaptive"

	// server
	ConfigRpcServerType        = "server.rpc.type"
	ConfigServerListen         = "server.listen"
	ConfigServerHttpPort       = "server.http.port"
	ConfigServerHttpToken      = "server.http.token"
	ConfigServerName           = "server.name"
	ConfigSupportPrometheus    = "server.http.support_prometheus"
	ConfigServerRpcPort        = "server.rpc.port"
	ConfigServerRpcOnlyGateway = "server.rpc.only_gateway"

	// cluster
	ConfigClusterName    = "cluster.name"
	ConfigClusterToken   = "cluster.token"
	ConfigCenterNodeHost = "cluster.center_node.host"
	ConfigCenterNodePort = "cluster.center_node.port"
	ConfigClusterNodes   = "cluster.nodes"

	// user
	ConfigUsers = "users"

	// cert
	ConfigCertEmail       = "cert.email"
	ConfigCertSecrets     = "cert.secrets"
	ConfigCertDnsProvider = "cert.dns_provider"
	ConfigCertPath        = "cert.path"
	ConfigCertArgs        = "cert.args"
)

const (
	DefaultNodeType = "End"

	EndNodeType    = "End"
	CenterNodeType = "Center"
)

// cluster
const (
	// node连续60s没有更新则认为无效
	NodeTimeOut int64 = 60
)
