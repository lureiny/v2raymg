package template

const HysteriaConfigTemplate = `# https://v2.hysteria.network/zh/docs/advanced/Full-Server-Config/#__tabbed_2_1
listen: {{ .template_hysteria_listen }}

bandwidth:
  up: {{ .template_hysteria_bandwidth_up }}
  down: {{ .template_hysteria_bandwidth_down }}

ignoreClientBandwidth: {{ .template_hysteria_ignore_client_bandwidth }}

auth:
  type: http
  http:
    url: {{ .template_hysteria_auth_url }}

# 流量统计
trafficStats:
  listen: {{ .template_hysteria_traffic_stats_listen }}
  secret: {{ .template_hysteria_traffic_stats_secret }}

masquerade:
  type: proxy
  proxy:
    url: https://news.ycombinator.com/
    rewriteHost: true
{{ if .template_use_acme }}
acme:
  domains:
  {{- range .template_domains }}  
    - {{ . }}
  {{- end}}
  email: {{ .template_email }}
{{ end }}`

const XrayOrV2rayConfigTemplate = `{
"routing": {
	"rules": [
		{
			"inboundTag": [
				"api"
			],
			"outboundTag": "api",
			"type": "field"
		}
	],
    "domainStrategy": "AsIs"
},
"inbounds": [
	{
		"protocol": "dokodemo-door",
		"port": {{ .template_xray_v2ray_api_port }},
		"listen": "127.0.0.1",
		"settings": {
			"address": "127.0.0.1"
		},
		"tag": "api"
	}
],
"outbounds": [
	{
		"protocol": "freedom"
	}
],
"policy": {
	"system": {
		"statsInboundUplink": true,
		"statsInboundDownlink": true,
		"statsOutboundUplink": true,
		"statsOutboundDownlink": true
	}
},
"api": {
	"tag": "api",
	"services": [
		"HandlerService",
		"LoggerService",
		"StatsService"
	]
},
"stats": {}
}`
