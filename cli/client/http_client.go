package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/lureiny/v2raymg/cli/common"
	"github.com/lureiny/v2raymg/cluster"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

func getCallBackFunc(fn func(resp *http.Response) error) HttpCallback {
	return func(r *http.Response, err error) error {
		if err != nil {
			return err
		}
		return fn(r)
	}
}

func ListNode(host, token string) (map[string]*cluster.Node, error) {
	nodeList := map[string]*cluster.Node{}
	cb := func(resp *http.Response) error {
		defer resp.Body.Close()
		d, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return json.Unmarshal(d, &nodeList)
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.ListNodeURI)
	err := DoGetRequest(reqUrl, map[string]interface{}{
		"token": token,
	}, nil, getCallBackFunc(cb))
	return nodeList, err
}

func ListCert(host, token, target string) (map[string][]*proto.Cert, error) {
	certList := map[string][]*proto.Cert{}
	cb := func(resp *http.Response) error {
		defer resp.Body.Close()
		d, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return json.Unmarshal(d, &certList)
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.ListCert)
	err := DoGetRequest(reqUrl, map[string]interface{}{
		"token":  token,
		"target": target,
	}, nil, getCallBackFunc(cb))
	return certList, err
}

func SetGatewayModel(host, token, target string, enableGatewayModel bool) (string, error) {
	result := ""
	headers := map[string]interface{}{
		"token":  token,
		"target": target,
	}
	if enableGatewayModel {
		headers["enable_gateway_model"] = "1"
	} else {
		headers["enable_gateway_model"] = "0"
	}
	cb := func(resp *http.Response) error {
		defer resp.Body.Close()
		d, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.Gateway)
	err := DoGetRequest(reqUrl, headers, nil, getCallBackFunc(cb))
	return result, err
}

func ApplyCert(host, token, target, domain string) (string, error) {
	result := ""
	headers := map[string]interface{}{
		"token":  token,
		"target": target,
		"domain": domain,
	}
	cb := func(resp *http.Response) error {
		defer resp.Body.Close()
		d, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.ApplyCert)
	err := DoGetRequest(reqUrl, headers, nil, getCallBackFunc(cb))
	return result, err
}

func FastAddInbound(host, token, target, tag, protocol, stream, domain string, isXtls bool, port int) (string, error) {
	result := ""
	headers := map[string]interface{}{
		"token":    token,
		"target":   target,
		"tag":      tag,
		"protocol": protocol,
		"stream":   stream,
		"domain":   domain,
		"isXtls":   isXtls,
		"port":     port,
	}
	cb := func(resp *http.Response) error {
		defer resp.Body.Close()
		d, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.FastAddInbound)
	err := DoGetRequest(reqUrl, headers, nil, getCallBackFunc(cb))
	return result, err
}

func CopyUserBetweenNodes(host, token, srcNode, dstNode string) (string, error) {
	result := ""
	headers := map[string]interface{}{
		"token":    token,
		"src_node": srcNode,
		"dst_node": dstNode,
	}
	cb := func(resp *http.Response) error {
		defer resp.Body.Close()
		d, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.CopyUserBetweenNodes)
	err := DoGetRequest(reqUrl, headers, nil, getCallBackFunc(cb))
	return result, err
}

func userOp(host, token, target string, opType common.UserOpType, params map[string]interface{}) ([]byte, error) {
	result := []byte{}
	headers := map[string]interface{}{
		"token":  token,
		"target": target,
		"type":   opType,
	}
	for k, v := range params {
		headers[k] = v
	}

	cb := func(resp *http.Response) error {
		defer resp.Body.Close()
		d, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		result = d
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.User)
	err := DoGetRequest(reqUrl, headers, nil, getCallBackFunc(cb))
	return result, err
}

func AddUser(host, token, target, userName, password, tags string, expire, ttl int) (string, error) {
	params := map[string]interface{}{
		"user":   userName,
		"pwd":    password,
		"expire": expire,
		"ttl":    ttl,
		"tags":   tags,
	}
	result, err := userOp(host, token, target, common.AddUser, params)
	return string(result), err
}

func UpdateUser(host, token, target, userName, password string, expire, ttl int) (string, error) {
	params := map[string]interface{}{
		"user":   userName,
		"pwd":    password,
		"expire": expire,
		"ttl":    ttl,
	}
	result, err := userOp(host, token, target, common.UpdateUser, params)
	return string(result), err
}

func DeleteUser(host, token, target, userName, tags string) (string, error) {
	params := map[string]interface{}{
		"user": userName,
		"tags": tags,
	}
	result, err := userOp(host, token, target, common.DeleteUser, params)
	return string(result), err
}

func ResetUser(host, token, target, userName string) (string, error) {
	params := map[string]interface{}{
		"user": userName,
	}
	result, err := userOp(host, token, target, common.ResetUser, params)
	return string(result), err
}

func ListUser(host, token, target string) (map[string][]*proto.User, error) {
	data, err := userOp(host, token, target, common.ListUser, nil)
	if err != nil {
		return nil, err
	}
	users := map[string][]*proto.User{}
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func ClearUser(host, token, target, users string) (string, error) {
	result := ""
	headers := map[string]interface{}{
		"token":  token,
		"target": target,
		"users":  users,
	}
	cb := func(resp *http.Response) error {
		defer resp.Body.Close()
		d, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.ClearUsers)
	err := DoGetRequest(reqUrl, headers, nil, getCallBackFunc(cb))
	return result, err
}

func CopyUserInBound(host, token, target, srcTag, dstTag string) (string, error) {
	result := ""
	headers := map[string]interface{}{
		"token":   token,
		"src_tag": srcTag,
		"dst_tag": dstTag,
		"target":  target,
		"type":    "copyUser",
	}
	cb := func(resp *http.Response) error {
		defer resp.Body.Close()
		d, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.Bound)
	err := DoGetRequest(reqUrl, headers, nil, getCallBackFunc(cb))
	return result, err
}

func DeleteInBound(host, token, target, srcTag string) (string, error) {
	result := ""
	headers := map[string]interface{}{
		"token":   token,
		"src_tag": srcTag,
		"target":  target,
		"type":    "deleteInbound",
	}
	cb := func(resp *http.Response) error {
		defer resp.Body.Close()
		d, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		result = string(d)
		return nil
	}

	reqUrl := fmt.Sprintf("%s/%s", host, common.Bound)
	err := DoGetRequest(reqUrl, headers, nil, getCallBackFunc(cb))
	return result, err
}
