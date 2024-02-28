package common

// config
const (
	// proxy
	ConfigXrayOrV2rayProxyConfigFile = "proxy.xray_or_v2ray_config_file" // xray/v2ray config
	ConfigHysteriaProxyConfigFile    = "proxy.hysteria_config_file"      // hysteria config
	ConfigProxyVersion               = "proxy.version"
	ConfigProxyDefaultTags           = "proxy.default_tags"
	ConfigProxyHost                  = "proxy.host"
	ConfigProxyPort                  = "proxy.port"
	ConfigProxyAdaptive              = "proxy.adaptive"

	// sub
	ConfigRemoteSubAddress = "sub.remote_sub_address"

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
	EndNodeType    = "End"
	CenterNodeType = "Center"

	DefaultNodeType = EndNodeType
)

// cluster
const (
	// node连续60s没有更新则认为无效
	NodeTimeOut int64 = 60
)

// sign
const (
	ErrMsgSplitSign = "|"
)

// ping checker
const (
	PingPeBaseUrl      = "https://ping.pe"
	PingPeGetResultUrl = PingPeBaseUrl + "/ajax_getPingResults_v2.php?stream_id=%s"
	PingPeSubmitUrl    = PingPeBaseUrl + "/%s"
)

var UserAgents = []string{
	"Mozilla/5.0 (compatible; MSIE 7.0; Windows 98; Trident/3.0)",
	"Mozilla/5.0 (compatible; MSIE 9.0; Windows NT 10.0; Trident/3.0)",
	"Mozilla/5.0 (compatible; MSIE 8.0; Windows NT 6.0; Trident/3.0)",
	"Mozilla/5.0 (compatible; MSIE 7.0; Windows NT 6.0; Trident/4.1)",
	"Mozilla/5.0 (compatible; MSIE 5.0; Windows 98; Win 9x 4.90; Trident/3.0)",
	"Mozilla/5.0 (Windows NT 4.0) AppleWebKit/532.0 (KHTML, like Gecko) Chrome/43.0.837.0 Safari/532.0",
	"Mozilla/5.0 (Windows 98) AppleWebKit/532.2 (KHTML, like Gecko) Chrome/41.0.833.0 Safari/532.2",
	"Mozilla/5.0 (Windows; U; Windows NT 6.1) AppleWebKit/535.17.1 (KHTML, like Gecko) Version/4.1 Safari/535.17.1",
	"Mozilla/5.0 (Windows NT 6.1) AppleWebKit/536.0 (KHTML, like Gecko) Chrome/46.0.830.0 Safari/536.0",
	"Mozilla/5.0 (compatible; MSIE 8.0; Windows NT 5.2; Trident/4.1)",
}

// hysteria/xray/v2ray default config
const (
	// xray/v2ray
	DefaultXrayV2rayApiPort = 10085

	DefaultHysteriaListen             = ":443"
	DefaultHysteriaBandwidthUp        = "1 gbps"
	DefaultHysteriaBandwidthDown      = "1 gbps"
	DefaultIgnoreClientBandwidth      = false
	DefaultHysteriaTrafficStatsListen = "127.0.0.1:31413"
)

// template var
const (
	// hysteria
	TemplateHysteriaListen                = "template_hysteria_listen"
	TemplateHysteriaBandwidthUp           = "template_hysteria_bandwidth_up"
	TemplateHysteriaBandwidthDown         = "template_hysteria_bandwidth_down"
	TemplateHysteriaIgnoreClientBandwidth = "template_hysteria_ignore_client_bandwidth"
	TemplateHysteriaAuthUrl               = "template_hysteria_auth_url"
	TemplateHysteriaTrafficStatsListen    = "template_hysteria_traffic_stats_listen"
	TemplateHysteriaTrafficStatsSecret    = "template_hysteria_traffic_stats_secret"
	TemplateUseAcme                       = "template_use_acme"
	TemplateDomains                       = "template_domains"
	TemplateEmail                         = "template_email"

	// xray or v2ray
	TemplateXrayV2rayApiPort = "template_xray_v2ray_api_port"
)
