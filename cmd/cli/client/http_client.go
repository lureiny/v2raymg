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

func FastAddInbound(host, token, target, tag, protocol, stream, domain, container string, isXtls bool, port int) (string, error) {
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
		"target":    target,
		"tag":       tag,
		"protocol":  protocol,
		"stream":    stream,
		"domain":    domain,
		"isXtls":    isXtls,
		"port":      port,
		"container": container,
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

func AddUser(host, token, target, userName, password, tags string, expire, ttl int) (string, error) {
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
		"pwd":    password,
		"expire": expire,
		"ttl":    ttl,
		"tags":   tags,
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func UpdateUser(host, token, target, userName, password string, expire, ttl int) (string, error) {
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
		"pwd":    password,
		"expire": expire,
		"ttl":    ttl,
	}
	err := DoPutRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

func DeleteUser(host, token, target, userName, tags string) (string, error) {
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
		"tags":   tags,
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

func DeleteInBound(host, token, target, srcTag string) (string, error) {
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
		"target":  target,
		"src_tag": srcTag,
	}
	err := DoDeleteRequest(reqUrl, token, body, getCallBackFunc(cb))
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

func GetStat(host, token, target, pattern string, reset bool) (string, error) {
	result := ""
	resetVal := "0"
	if reset {
		resetVal = "1"
	}
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.Stat)
	err := DoGetRequest(reqUrl, token, map[string]interface{}{
		"target":  target,
		"pattern": pattern,
		"reset":   resetVal,
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

// RotatePort resets all forward ports for the authenticated user (JWT auth via AuthMiddleware).
// username is optional: empty = operate on the token's own user; non-empty = target user (admin only).
func RotatePort(host, token, username string) (string, error) {
	result := ""
	cb := func(resp *http.Response) error {
		d, err := readBody(resp)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}
	reqUrl := fmt.Sprintf("%s/%s", host, common.RotatePort)
	body := map[string]interface{}{}
	if username != "" {
		body["username"] = username
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// RotateInboundPort resets the forward port for a specific container+inbound (JWT auth via AuthMiddleware).
// username is optional: empty = token's own user; non-empty = target user (admin only).
// port=0 means auto-allocate; port>0 requests a specific port.
func RotateInboundPort(host, token, username, container, inbound string, port int) (string, error) {
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
	if username != "" {
		body["username"] = username
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// RotateAllPorts resets all forward ports for the authenticated user using make-before-break (JWT auth).
// username is optional: empty = token's own user; non-empty = target user (admin only).
func RotateAllPorts(host, token, username string) (string, error) {
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
	if username != "" {
		body["username"] = username
	}
	err := DoPostRequest(reqUrl, token, body, getCallBackFunc(cb))
	return result, err
}

// SetUserRole sets the frontend login role ("admin" or "normal") for the given user.
// Requires admin auth (X-Token or admin JWT).
func SetUserRole(host, token, username, role string) (string, error) {
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

// Profile returns the current user's profile from GET /profile.
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
	reqUrl := fmt.Sprintf("%s/%s", host, common.Profile)
	err := DoGetRequest(reqUrl, token, map[string]interface{}{}, getCallBackFunc(cb))
	return result, err
}
