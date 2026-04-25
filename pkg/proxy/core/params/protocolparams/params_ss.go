package protocolparams

import (
	"fmt"
)

// parseSS fills the SS slot of a ProtocolParams from the raw request map.
//
// Contract:
//
//   - KeyPassword and KeyCipher are required — FillDefaults fills both if the
//     caller omitted them (default cipher is "2022-blake3-aes-256-gcm").
//   - KeyUDP defaults to true. Shadowsocks is almost always used with UDP;
//     callers that want TCP-only must explicitly pass udp=false.
//   - KeyPlugin is optional. When present, the individual plugin-opts keys
//     (KeyPluginMode, KeyPluginHost, KeyPluginPath, KeyPluginTLS,
//     KeyPluginPassword, KeyPluginVersion) are read and stored in
//     SSParams.PluginOpts using their canonical short names ("mode", "host",
//     "path", "tls", "password", "version").
//   - Shadowsocks has no pluggable transport or TLS layer at the ProtocolParams
//     level; pp.Transport and pp.Security are left nil.
//
// Plugin notes:
//   - obfs-local: uses "mode" (http|tls) and optionally "host".
//   - v2ray-plugin: uses "mode" (websocket|quic), "path", "host", "tls".
//   - shadow-tls: uses "host" (target), "password", "version" (2 or 3).
//     shadow-tls is a separate proxy layer; the plugin info is for subscription
//     (client-side config) only — the mihomo listener itself runs plain SS.
//   - kcp-tun is out of scope (separate task).
func parseSS(raw map[string]any, pp *ProtocolParams) error {
	password, err := requireString(raw, KeyPassword)
	if err != nil {
		return fmt.Errorf("shadowsocks: %w", err)
	}

	cipher, err := requireString(raw, KeyCipher)
	if err != nil {
		return fmt.Errorf("shadowsocks: %w", err)
	}

	udp := true
	if v, ok := raw[KeyUDP]; ok {
		switch vv := v.(type) {
		case bool:
			udp = vv
		case string:
			switch vv {
			case "false", "0":
				udp = false
			case "true", "1":
				udp = true
				// empty string or unrecognised value: leave default (true)
			}
		}
	}

	ss := &SSParams{
		Password: password,
		Cipher:   cipher,
		UDP:      udp,
	}

	if plugin := optionalString(raw, KeyPlugin); plugin != "" {
		ss.Plugin = plugin
		ss.PluginOpts = parseSSPluginOpts(raw)
	}

	pp.SS = ss
	return nil
}

// parseSSPluginOpts collects the flat plugin-opt keys from the raw map into a
// canonical map[string]string keyed by mihomo plugin-opts names.
func parseSSPluginOpts(raw map[string]any) map[string]string {
	opts := make(map[string]string)
	if mode := optionalString(raw, KeyPluginMode); mode != "" {
		opts["mode"] = mode
	}
	if host := optionalString(raw, KeyPluginHost); host != "" {
		opts["host"] = host
	}
	if path := optionalString(raw, KeyPluginPath); path != "" {
		opts["path"] = path
	}
	switch v := raw[KeyPluginTLS].(type) {
	case bool:
		if v {
			opts["tls"] = "true"
		}
	case string:
		if v == "true" || v == "1" {
			opts["tls"] = "true"
		}
	}
	if pwd := optionalString(raw, KeyPluginPassword); pwd != "" {
		opts["password"] = pwd
	}
	if ver := optionalString(raw, KeyPluginVersion); ver != "" {
		opts["version"] = ver
	}
	if len(opts) == 0 {
		return nil
	}
	return opts
}
