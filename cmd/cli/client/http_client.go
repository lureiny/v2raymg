package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lureiny/v2raymg/cmd/cli/common"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// Login calls POST /login and returns the JWT token string and its Unix expiry timestamp.
func Login(host, username, password string) (token string, expire int64, err error) {
	var loginResp struct {
		Token  string `json:"token"`
		Expire int64  `json:"expire"`
	}
	cb := func(resp *http.Response) error {
		if resp.StatusCode != 200 {
			return fmt.Errorf("login failed: HTTP %d", resp.StatusCode)
		}
		d, e := readBody(resp)
		if e != nil {
			return e
		}
		return json.Unmarshal(d, &loginResp)
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.Login)
	body := map[string]interface{}{
		"username": username,
		"password": password,
	}
	err = DoPostRequest(reqUrl, "", body, getCallBackFunc(cb))
	return loginResp.Token, loginResp.Expire, err
}

func getCallBackFunc(fn func(resp *http.Response) error) HttpCallback {
	return func(r *http.Response, err error) error {
		if err != nil {
			return err
		}
		// Reject non-2xx responses here, at the one entry point every wrapper
		// shares, so a 4xx/5xx error body is never mistaken for a successful
		// result. Business errors are carried in a 200 body (code!=0) and are
		// left untouched.
		if r.StatusCode < 200 || r.StatusCode >= 300 {
			body, _ := readBody(r) // consumes+closes the body; only on the error path
			return unexpectedHTTPStatus(r, body)
		}
		return fn(r)
	}
}

func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func unexpectedHTTPStatus(resp *http.Response, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
}

// ListNode returns nodes keyed by name. Server returns a slice, so we convert.
func ListNode(host, token string) (map[string]*cluster.Node, error) {
	nodeList := []*cluster.Node{}
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return unexpectedHTTPStatus(resp, d)
		}
		return json.Unmarshal(d, &nodeList)
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.ListNodeURI)
	err := DoGetRequest(reqUrl, token, nil, getCallBackFunc(cb))
	if err != nil {
		return nil, err
	}
	result := make(map[string]*cluster.Node, len(nodeList))
	for _, n := range nodeList {
		result[n.GetName()] = n
	}
	return result, nil
}

func ListCert(host, token, target string) (map[string][]*proto.Cert, error) {
	certList := map[string][]*proto.Cert{}
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return unexpectedHTTPStatus(resp, d)
		}
		return json.Unmarshal(d, &certList)
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.ListCert)
	err := DoGetRequest(reqUrl, token, map[string]interface{}{
		"target": target,
	}, getCallBackFunc(cb))
	return certList, err
}

func SetGatewayModel(host, token, target string, enableGatewayModel bool) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.Gateway)
	body := map[string]interface{}{
		"target":               target,
		"enable_gateway_model": enableGatewayModel,
	}
	err := DoPutRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func ApplyCert(host, token, target, domain string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.ApplyCert)
	body := map[string]interface{}{
		"target": target,
		"domain": domain,
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// FastAddInboundRequest holds all parameters for the FastAddInbound API call.
type FastAddInboundRequest struct {
	Target             string
	Tag                string
	Protocol           string
	Stream             string
	Domain             string
	IsXtls             bool
	Port               int
	Container          string
	Security           string
	RealityTarget      string
	RealityServerNames []string
	RealityShortIDs    []string
	// Generic
	SkipCertVerify  bool
	SelfSigned      bool
	SniffingEnabled bool
	// Shadowsocks plugin
	Plugin         string
	PluginMode     string
	PluginHost     string
	PluginPath     string
	PluginPassword string
	PluginVersion  string
	PluginTLS      bool
	// Hysteria2
	Obfs                  string
	ObfsPassword          string
	Up                    string
	Down                  string
	Masquerade            string
	IgnoreClientBandwidth bool
	// TUIC
	CongestionController string
	UDPRelayMode         string
	ZeroRTTHandshake     bool
	HeartbeatInterval    string
	DisableSNI           bool
	// AnyTLS
	PaddingScheme                   string
	IdleSessionCheckIntervalSeconds int
	IdleSessionTimeoutSeconds       int
	MinIdleSession                  int
}

func FastAddInbound(host, token string, req *FastAddInboundRequest) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.FastAddInbound)
	body := map[string]interface{}{
		"target":               req.Target,
		"tag":                  req.Tag,
		"protocol":             req.Protocol,
		"stream":               req.Stream,
		"domain":               req.Domain,
		"is_xtls":              req.IsXtls,
		"port":                 req.Port,
		"container":            req.Container,
		"security":             req.Security,
		"reality_target":       req.RealityTarget,
		"reality_server_names": req.RealityServerNames,
		"reality_short_ids":    req.RealityShortIDs,
		"skip_cert_verify":     req.SkipCertVerify,
		"self_signed":          req.SelfSigned,
		"sniffing_enabled":     req.SniffingEnabled,
		"plugin_tls":           req.PluginTLS,
		"ignore_client_bandwidth": req.IgnoreClientBandwidth,
		"zero_rtt_handshake":   req.ZeroRTTHandshake,
		"disable_sni":          req.DisableSNI,
		"idle_session_check_interval_seconds": req.IdleSessionCheckIntervalSeconds,
		"idle_session_timeout_seconds":        req.IdleSessionTimeoutSeconds,
		"min_idle_session":                    req.MinIdleSession,
	}
	// Only include string/int fields when non-empty/non-zero to keep request clean.
	if req.Plugin != "" {
		body["plugin"] = req.Plugin
	}
	if req.PluginMode != "" {
		body["plugin_mode"] = req.PluginMode
	}
	if req.PluginHost != "" {
		body["plugin_host"] = req.PluginHost
	}
	if req.PluginPath != "" {
		body["plugin_path"] = req.PluginPath
	}
	if req.PluginPassword != "" {
		body["plugin_password"] = req.PluginPassword
	}
	if req.PluginVersion != "" {
		body["plugin_version"] = req.PluginVersion
	}
	if req.Obfs != "" {
		body["obfs"] = req.Obfs
	}
	if req.ObfsPassword != "" {
		body["obfs_password"] = req.ObfsPassword
	}
	if req.Up != "" {
		body["up"] = req.Up
	}
	if req.Down != "" {
		body["down"] = req.Down
	}
	if req.Masquerade != "" {
		body["masquerade"] = req.Masquerade
	}
	if req.CongestionController != "" {
		body["congestion_controller"] = req.CongestionController
	}
	if req.UDPRelayMode != "" {
		body["udp_relay_mode"] = req.UDPRelayMode
	}
	if req.HeartbeatInterval != "" {
		body["heartbeat_interval"] = req.HeartbeatInterval
	}
	if req.PaddingScheme != "" {
		body["padding_scheme"] = req.PaddingScheme
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func CopyUserBetweenNodes(host, token, srcNode, dstNode string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.CopyUserBetweenNodes)
	body := map[string]interface{}{
		"src_node": srcNode,
		"dst_node": dstNode,
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func AddUser(host, token, target, userName, password string, expire, ttl int,
	role, group string, uploadBps, downloadBps int64, maxClients, recycleDelaySec, drainSec int) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.User)
	body := map[string]interface{}{
		"target":                   target,
		"user":                     userName,
		"pwd":                      password,
		"expire":                   expire,
		"ttl":                      ttl,
		"role":                     role,
		"group":                    group,
		"upload_bps":               uploadBps,
		"download_bps":             downloadBps,
		"max_clients":              maxClients,
		"client_recycle_delay_sec": recycleDelaySec,
		"client_drain_sec":         drainSec,
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func UpdateUser(host, token, target, userName, password string, expire, ttl int,
	role string, uploadBps, downloadBps int64, maxClients, recycleDelaySec, drainSec int, group string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.User)
	body := map[string]interface{}{
		"target":                  target,
		"user":                    userName,
		"pwd":                     password,
		"expire":                  expire,
		"ttl":                     ttl,
		"role":                    role,
		"upload_bps":              uploadBps,
		"download_bps":            downloadBps,
		"max_clients":             maxClients,
		"client_recycle_delay_sec": recycleDelaySec,
		"client_drain_sec":        drainSec,
		"group":                   group,
	}
	err := DoPutRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func DeleteUser(host, token, target, userName string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.User)
	body := map[string]interface{}{
		"target": target,
		"user":   userName,
	}
	err := DoDeleteRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func ResetUser(host, token, target, userName string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.UserReset)
	body := map[string]interface{}{
		"target": target,
		"user":   userName,
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func ListUser(host, token, target string) (map[string][]*proto.User, error) {
	users := map[string][]*proto.User{}
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		return json.Unmarshal(d, &users)
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.User)
	err := DoGetRequest(reqUrl, token, map[string]interface{}{
		"target": target,
	}, getCallBackFunc(cb))
	if err != nil {
		return nil, err
	}
	return users, nil
}

func ListAllUsers(host, token, target string) (map[string][]*proto.User, error) {
	users := map[string][]*proto.User{}
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		return json.Unmarshal(d, &users)
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.User)
	err := DoGetRequest(reqUrl, token, map[string]interface{}{
		"target": target,
	}, getCallBackFunc(cb))
	if err != nil {
		return nil, err
	}
	return users, nil
}

func AddInBound(host, token, target, boundRawString string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.Inbound)
	body := map[string]interface{}{
		"target":           target,
		"bound_raw_string": boundRawString,
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func GetInBound(host, token, target, srcTag string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.Inbound)
	err := DoGetRequest(reqUrl, token, map[string]interface{}{
		"target":  target,
		"src_tag": srcTag,
	}, getCallBackFunc(cb))
	return result, err
}

func UpdateProxy(host, token, target, versionTag string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.Update)
	body := map[string]interface{}{
		"target":      target,
		"version_tag": versionTag,
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func SetPingCheck(host, token, target string, enablePingCheck bool) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.PingCheck)
	body := map[string]interface{}{
		"target":            target,
		"enable_ping_check": enablePingCheck,
	}
	err := DoPutRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func ListInbounds(host, token, target string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.Inbounds)
	err := DoGetRequest(reqUrl, token, map[string]interface{}{
		"target": target,
	}, getCallBackFunc(cb))
	return result, err
}

// ListInboundsStructured returns inbounds grouped by node name.
// The server returns map[nodeName][]InboundInfo (same shape as ListUser).
func ListInboundsStructured(host, token, target string) (map[string][]*proto.InboundInfo, error) {
	result := map[string][]*proto.InboundInfo{}
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		return json.Unmarshal(d, &result)
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.Inbounds)
	err := DoGetRequest(reqUrl, token, map[string]interface{}{
		"target": target,
	}, getCallBackFunc(cb))
	if err != nil {
		return nil, err
	}
	return result, nil
}

func DeleteInboundByName(host, token, target, container, name string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.Inbounds)
	body := map[string]interface{}{
		"target":    target,
		"container": container,
		"name":      name,
	}
	err := DoDeleteRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// RotateInboundPort resets the forward port for a specific container+inbound (JWT auth via AuthMiddleware).
// target is optional: empty = local node; non-empty = target node name.
// username is optional: empty = token's own user; non-empty = target user (admin only).
// port=0 means auto-allocate; port>0 requests a specific port.
func RotateInboundPort(host, token, target, username, container, inbound string, port int) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.RotateInboundPort)
	body := map[string]interface{}{
		"container": container,
		"inbound":   inbound,
		"port":      port,
	}
	if target != "" {
		body["target"] = target
	}
	if username != "" {
		body["username"] = username
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// RotateAllPorts resets all forward ports for the authenticated user using make-before-break (JWT auth).
// target is optional: empty = local node; non-empty = target node name.
// username is optional: empty = token's own user; non-empty = target user (admin only).
func RotateAllPorts(host, token, target, username string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.RotateAllPorts)
	body := map[string]interface{}{}
	if target != "" {
		body["target"] = target
	}
	if username != "" {
		body["username"] = username
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// SetUserRole sets the frontend login role ("admin" or "normal") for the given user.
// Requires admin auth (X-Token or admin JWT).
func SetUserRole(host, token, target, username, role string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s/%s/role", host, common.UserRole, username)
	body := map[string]interface{}{
		"role": role,
	}
	if target != "" {
		body["target"] = target
	}
	err := DoPutRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// SetUserBandwidth sets bandwidth limits for a user.
// uploadBps/downloadBps: nil = no change, non-nil = set value (0 = unlimited).
func SetUserBandwidth(host, token, target, username string, uploadBps, downloadBps *int64) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s/%s/bandwidth", host, common.UserBandwidth, username)
	body := map[string]interface{}{}
	if target != "" {
		body["target"] = target
	}
	if uploadBps != nil {
		body["upload_bps"] = *uploadBps
	}
	if downloadBps != nil {
		body["download_bps"] = *downloadBps
	}
	err := DoPutRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// SetUserClientLimit sets client connection limits for a user.
// maxClients=0 means remove the limit (unlimited).
// recycleDelaySec/drainSec: nil = no change, non-nil = set value.
func SetUserClientLimit(host, token, target, username string, maxClients int, recycleDelaySec, drainSec *int) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s/%s/client-limit", host, common.UserClientLimit, username)
	body := map[string]interface{}{
		"max_clients": maxClients,
	}
	if target != "" {
		body["target"] = target
	}
	if recycleDelaySec != nil {
		body["recycle_delay_sec"] = *recycleDelaySec
	}
	if drainSec != nil {
		body["drain_sec"] = *drainSec
	}
	err := DoPutRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// Logout revokes the current JWT by calling POST /logout.
// The token must be a Bearer JWT ("Bearer <jwt>"); X-Token is rejected by the server.
func Logout(host, token string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.Logout)
	err := DoPostRequest(reqUrl, token, map[string]interface{}{}, getCallBackFunc(cb))
	return result, err
}

// Profile returns the current user's profile from GET /api/user.
// The token must be a Bearer JWT; X-Token is rejected by the server.
func Profile(host, token string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.User)
	err := DoGetRequest(reqUrl, token, map[string]interface{}{}, getCallBackFunc(cb))
	return result, err
}

// ChangePassword changes the authenticated user's own password via PUT /api/profile/password.
// Requires JWT auth.
func ChangePassword(host, token, oldPassword, newPassword string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.ChangePassword)
	body := map[string]interface{}{
		"old_password": oldPassword,
		"new_password": newPassword,
	}
	err := DoPutRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// TransferCert transfers a certificate to target nodes via POST /api/cert/transfer.
func TransferCert(host, token, target, domain string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.TransferCert)
	body := map[string]interface{}{
		"target": target,
		"domain": domain,
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// GetStatus returns node status from GET /api/status.
func GetStatus(host, token, target string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	params := map[string]interface{}{}
	if target != "" {
		params["target"] = target
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.Status)
	err := DoGetRequest(reqUrl, token, params, getCallBackFunc(cb))
	return result, err
}
