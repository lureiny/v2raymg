package http

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

func getExpireTime(parmas map[string]string) (int64, error) {
	var expire uint64 = 0
	var ttl uint64 = 0
	var err error = nil

	if _, ok := parmas["expire"]; ok {
		if expire, err = strconv.ParseUint(parmas["expire"], 10, 64); err != nil {
			return 0, fmt.Errorf("invalid expire param > %v", err)
		}
	}

	if _, ok := parmas["ttl"]; ok {
		if ttl, err = strconv.ParseUint(parmas["ttl"], 10, 64); err != nil {
			return 0, fmt.Errorf("invalid ttl param > %v", err)
		}
	}

	// 优先使用ttl
	if ttl == 0 {
		return int64(expire), nil
	} else {
		return time.Now().Unix() + int64(ttl), nil
	}
}

func calcExpire(expire, ttl uint64) int64 {
	if ttl == 0 {
		return int64(expire)
	}
	return time.Now().Unix() + int64(ttl)
}

const userAddHelpText = `POST /api/user
	添加用户
	body: {"target": "", "user": "", "pwd": "", "expire": 0, "ttl": 0, "role": "", "group": "", "inbound_tags": [], "upload_bps": 0, "download_bps": 0, "max_clients": 0, "client_recycle_delay_sec": 0, "client_drain_sec": 0}
	user: 用户名, pwd: password, expire: 过期时间戳(与ttl同时存在时优先使用ttl), ttl: 存活时间(秒), inbound_tags: Layer 2 访问标签数组`

const userUpdateHelpText = `PUT /api/user
	更新用户信息
	body: {"target": "", "user": "", "pwd": "", "expire": 0, "ttl": 0, "role": "", "group": "", "inbound_tags": [], "upload_bps": 0, "download_bps": 0, "max_clients": 0, "client_recycle_delay_sec": 0, "client_drain_sec": 0}
	inbound_tags: 省略或 null 保持不变, [] 清空全部标签, 非空全量替换; 配额字段 0 保持不变, -1 取消限制`

const userDeleteHelpText = `DELETE /api/user
	删除用户
	body: {"target": "", "user": ""}`

const userResetHelpText = `POST /api/user/reset
	重置用户proxy密钥
	body: {"target": "", "user": ""}`

const userResetTrafficHelpText = `POST /api/user/reset-traffic
	重置用户累计持久化流量统计 (TrafficTotalUplink/Downlink 清零)
	节点自治: 每个目标节点各自清零本地累计值; target 为空默认作用本机。
	body: {"target": "", "user": ""}`

const userResetAuthTokenHelpText = `POST /api/user/reset-token
	重置当前用户的auth token
	Body (optional): {"target":"node1"}
	  target: 目标节点名，空则默认本机`

// UserAddReq is the request payload for POST /api/user (MCP tool: add_user).
type UserAddReq struct {
	Target                string   `json:"target,omitempty" jsonschema:"Target node name or node ID, or 'all' for all nodes (defaults to local node)"`
	User                  string   `json:"user" jsonschema:"Username to create"`
	Pwd                   string   `json:"pwd,omitempty" jsonschema:"Login password for the user"`
	Expire                uint64   `json:"expire,omitempty" jsonschema:"Expiration Unix timestamp in seconds (overridden by ttl when ttl is non-zero)"`
	TTL                   uint64   `json:"ttl,omitempty" jsonschema:"Time-to-live in seconds from now (takes precedence over expire when non-zero)"`
	Role                  string   `json:"role,omitempty" jsonschema:"User role (e.g. normal or admin)"`
	Group                 string   `json:"group,omitempty" jsonschema:"User group name for cluster visibility"`
	UploadBps             int64    `json:"upload_bps,omitempty" jsonschema:"Upload bandwidth limit in bytes per second (0 for default)"`
	DownloadBps           int64    `json:"download_bps,omitempty" jsonschema:"Download bandwidth limit in bytes per second (0 for default)"`
	MaxClients            int32    `json:"max_clients,omitempty" jsonschema:"Maximum concurrent client connections (0 for default)"`
	ClientRecycleDelaySec int32    `json:"client_recycle_delay_sec,omitempty" jsonschema:"Delay in seconds before recycling inactive client slots (0 for default)"`
	ClientDrainSec        int32    `json:"client_drain_sec,omitempty" jsonschema:"Drain grace period in seconds when closing excess clients (0 for default)"`
	InboundTags           []string `json:"inbound_tags,omitempty" jsonschema:"Layer 2 inbound access tags restricting which tagged inbounds this user can use (empty means unrestricted)"`
}

// UserUpdateReq is the request payload for PUT /api/user (MCP tool: update_user).
type UserUpdateReq struct {
	Target                string    `json:"target,omitempty" jsonschema:"Target node name or node ID, or 'all' for all nodes (defaults to local node)"`
	User                  string    `json:"user" jsonschema:"Username to update"`
	Pwd                   string    `json:"pwd,omitempty" jsonschema:"New login password (empty keeps unchanged)"`
	Expire                uint64    `json:"expire,omitempty" jsonschema:"New expiration Unix timestamp in seconds (overridden by ttl when ttl is non-zero)"`
	TTL                   uint64    `json:"ttl,omitempty" jsonschema:"New time-to-live in seconds from now (takes precedence over expire when non-zero)"`
	Role                  string    `json:"role,omitempty" jsonschema:"New user role (empty keeps unchanged)"`
	UploadBps             int64     `json:"upload_bps,omitempty" jsonschema:"Upload bandwidth limit in bytes per second (0 keeps unchanged, -1 removes limit)"`
	DownloadBps           int64     `json:"download_bps,omitempty" jsonschema:"Download bandwidth limit in bytes per second (0 keeps unchanged, -1 removes limit)"`
	MaxClients            int32     `json:"max_clients,omitempty" jsonschema:"Maximum concurrent client connections (0 keeps unchanged, -1 removes limit)"`
	ClientRecycleDelaySec int32     `json:"client_recycle_delay_sec,omitempty" jsonschema:"Delay in seconds before recycling inactive client slots (0 keeps unchanged, -1 removes limit)"`
	ClientDrainSec        int32     `json:"client_drain_sec,omitempty" jsonschema:"Drain grace period in seconds when closing excess clients (0 keeps unchanged, -1 removes limit)"`
	Group                 string    `json:"group,omitempty" jsonschema:"User group name for cluster visibility (empty keeps unchanged)"`
	InboundTags           *[]string `json:"inbound_tags,omitempty" jsonschema:"Layer 2 inbound access tags (omit/null keeps unchanged; empty array [] clears all tags)"`
}

// UserDeleteReq is the request payload for DELETE /api/user (MCP tool: delete_user).
type UserDeleteReq struct {
	Target string `json:"target,omitempty" jsonschema:"Target node name or node ID, or 'all' for all nodes (defaults to local node)"`
	User   string `json:"user" jsonschema:"Username to delete"`
}

// UserResetReq is the request payload for POST /api/user/reset (MCP tool: reset_user).
type UserResetReq struct {
	Target string `json:"target,omitempty" jsonschema:"Target node name or node ID, or 'all' for all nodes (defaults to local node)"`
	User   string `json:"user" jsonschema:"Username whose proxy credentials will be reset"`
}

// UserResetTrafficReq is the request payload for POST /api/user/reset-traffic (MCP tool: reset_user_traffic).
type UserResetTrafficReq struct {
	Target string `json:"target,omitempty" jsonschema:"Target node name or node ID, or 'all' for all nodes (defaults to local node)"`
	User   string `json:"user" jsonschema:"Username whose cumulative traffic counters will be reset"`
}

// UserResetAuthTokenReq is the request payload for POST /api/user/reset-token (MCP tool: reset_auth_token).
type UserResetAuthTokenReq struct {
	Target string `json:"target,omitempty" jsonschema:"Target node name or node ID, or 'all' for all nodes (defaults to local node)"`
}

func cloneAnyValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		if typed == nil {
			return map[string]any(nil)
		}
		cp := make(map[string]any, len(typed))
		for k, val := range typed {
			cp[k] = cloneAnyValue(val)
		}
		return cp
	case gin.H:
		if typed == nil {
			return gin.H(nil)
		}
		cp := make(gin.H, len(typed))
		for k, val := range typed {
			cp[k] = cloneAnyValue(val)
		}
		return cp
	case []any:
		if typed == nil {
			return []any(nil)
		}
		cp := make([]any, len(typed))
		for i, item := range typed {
			cp[i] = cloneAnyValue(item)
		}
		return cp
	case []map[string]any:
		if typed == nil {
			return []map[string]any(nil)
		}
		cp := make([]map[string]any, len(typed))
		for i, item := range typed {
			if item == nil {
				cp[i] = nil
				continue
			}
			cp[i] = cloneAnyValue(item).(map[string]any)
		}
		return cp
	case []gin.H:
		if typed == nil {
			return []gin.H(nil)
		}
		cp := make([]gin.H, len(typed))
		for i, item := range typed {
			if item == nil {
				cp[i] = nil
				continue
			}
			cp[i] = cloneAnyValue(item).(gin.H)
		}
		return cp
	case []string:
		if typed == nil {
			return []string(nil)
		}
		cp := make([]string, len(typed))
		copy(cp, typed)
		return cp
	default:
		return v
	}
}

// sanitizeResetAuthTokenForMCP deep-copies a reset-token response map for MCP
// callers and redacts non-empty "auth_token" values without mutating resp.
func sanitizeResetAuthTokenForMCP(resp map[string]any) any {
	if resp == nil {
		return nil
	}
	out := make(map[string]any, len(resp))
	for k, v := range resp {
		if k == "auth_token" {
			if tok, ok := v.(string); ok && tok != "" {
				out[k] = "[REDACTED]"
				continue
			}
		}
		out[k] = cloneAnyValue(v)
	}
	return out
}

func (s *HttpServer) userAddEndpoint() Endpoint[UserAddReq, map[string]any] {
	return Endpoint[UserAddReq, map[string]any]{
		Method:         http.MethodPost,
		Path:           "/user",
		Scope:          ScopeAdmin,
		Help:           userAddHelpText,
		MCPName:        "add_user",
		MCPDescription: "Add a user to target cluster nodes (defaults to the local node when target is omitted).",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req UserAddReq) (map[string]any, error) {
			tags, err := contracts.NormalizeAccessTags(req.InboundTags)
			if err != nil {
				return nil, NewEndpointErr(400, fmt.Sprintf("invalid inbound_tags: %v", err))
			}
			req.Target = resolveTarget(req.Target, s.Name)
			expire := calcExpire(req.Expire, req.TTL)
			userPoint := &proto.User{
				Name:                  req.User,
				Passwd:                req.Pwd,
				ExpireTime:            expire,
				Role:                  req.Role,
				Group:                 req.Group,
				UploadBps:             req.UploadBps,
				DownloadBps:           req.DownloadBps,
				MaxClients:            req.MaxClients,
				ClientRecycleDelaySec: req.ClientRecycleDelaySec,
				ClientDrainSec:        req.ClientDrainSec,
				InboundTags:           tags,
			}
			nodes := s.GetTargetNodes(req.Target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(500, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.AddUsersReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
				return nil, NewEndpointErr(500, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

func (s *HttpServer) userUpdateEndpoint() Endpoint[UserUpdateReq, map[string]any] {
	return Endpoint[UserUpdateReq, map[string]any]{
		Method:         http.MethodPut,
		Path:           "/user",
		Scope:          ScopeAdmin,
		Help:           userUpdateHelpText,
		MCPName:        "update_user",
		MCPDescription: "Update an existing user's settings on target cluster nodes (defaults to the local node when target is omitted).",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req UserUpdateReq) (map[string]any, error) {
			req.Target = resolveTarget(req.Target, s.Name)
			expire := calcExpire(req.Expire, req.TTL)
			userPoint := &proto.User{
				Name:                  req.User,
				Passwd:                req.Pwd,
				ExpireTime:            expire,
				Role:                  req.Role,
				UploadBps:             req.UploadBps,
				DownloadBps:           req.DownloadBps,
				MaxClients:            req.MaxClients,
				ClientRecycleDelaySec: req.ClientRecycleDelaySec,
				ClientDrainSec:        req.ClientDrainSec,
				Group:                 req.Group,
			}
			if req.InboundTags != nil {
				tags, err := contracts.NormalizeAccessTags(*req.InboundTags)
				if err != nil {
					return nil, NewEndpointErr(400, fmt.Sprintf("invalid inbound_tags: %v", err))
				}
				userPoint.InboundTags = tags
				userPoint.UpdateInboundTags = true
			}
			nodes := s.GetTargetNodes(req.Target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(500, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.UpdateUsersReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
				return nil, NewEndpointErr(500, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

func (s *HttpServer) userDeleteEndpoint() Endpoint[UserDeleteReq, map[string]any] {
	return Endpoint[UserDeleteReq, map[string]any]{
		Method:         http.MethodDelete,
		Path:           "/user",
		Scope:          ScopeAdmin,
		Help:           userDeleteHelpText,
		MCPName:        "delete_user",
		MCPDescription: "Delete a user from target cluster nodes (defaults to the local node when target is omitted).",
		ReadOnly:       false,
		Destructive:    true,
		Execute: func(ctx context.Context, caller Caller, req UserDeleteReq) (map[string]any, error) {
			req.Target = resolveTarget(req.Target, s.Name)
			userPoint := &proto.User{Name: req.User}
			nodes := s.GetTargetNodes(req.Target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(500, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.DeleteUsersReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
				return nil, NewEndpointErr(500, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

func (s *HttpServer) userResetEndpoint() Endpoint[UserResetReq, map[string]any] {
	return Endpoint[UserResetReq, map[string]any]{
		Method:         http.MethodPost,
		Path:           "/user/reset",
		Scope:          ScopeAdmin,
		Help:           userResetHelpText,
		MCPName:        "reset_user",
		MCPDescription: "Reset a user's proxy credentials on target cluster nodes (defaults to the local node when target is omitted).",
		ReadOnly:       false,
		Destructive:    true,
		Execute: func(ctx context.Context, caller Caller, req UserResetReq) (map[string]any, error) {
			if req.User == "" {
				return nil, NewEndpointErr(400, "user is required")
			}
			req.Target = resolveTarget(req.Target, s.Name)
			userPoint := &proto.User{Name: req.User}
			nodes := s.GetTargetNodes(req.Target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(500, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.ResetUserReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
				return nil, NewEndpointErr(500, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

func (s *HttpServer) userResetTrafficEndpoint() Endpoint[UserResetTrafficReq, map[string]any] {
	return Endpoint[UserResetTrafficReq, map[string]any]{
		Method:         http.MethodPost,
		Path:           "/user/reset-traffic",
		Scope:          ScopeAdmin,
		Help:           userResetTrafficHelpText,
		MCPName:        "reset_user_traffic",
		MCPDescription: "Reset a user's persisted cumulative uplink and downlink traffic counters to zero on target cluster nodes (defaults to the local node when target is omitted).",
		ReadOnly:       false,
		Destructive:    true,
		Execute: func(ctx context.Context, caller Caller, req UserResetTrafficReq) (map[string]any, error) {
			if req.User == "" {
				return nil, NewEndpointErr(400, "user is required")
			}
			req.Target = resolveTarget(req.Target, s.Name)
			userPoint := &proto.User{Name: req.User}
			nodes := s.GetTargetNodes(req.Target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(500, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.ResetUserTrafficReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
				return nil, NewEndpointErr(500, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

func (s *HttpServer) userResetAuthTokenEndpoint() Endpoint[UserResetAuthTokenReq, map[string]any] {
	return Endpoint[UserResetAuthTokenReq, map[string]any]{
		Method:         http.MethodPost,
		Path:           "/user/reset-token",
		Scope:          ScopeUser,
		Help:           userResetAuthTokenHelpText,
		OptionalBody:   true,
		MCPName:        "reset_auth_token",
		MCPDescription: "Reset the authenticated caller's own proxy auth token on target cluster nodes (requires JWT authentication; defaults to the local node when target is omitted; token value is redacted in MCP responses).",
		ReadOnly:       false,
		Destructive:    true,
		MCPSanitize:    sanitizeResetAuthTokenForMCP,
		Execute: func(ctx context.Context, caller Caller, req UserResetAuthTokenReq) (map[string]any, error) {
			if caller.Username == "" {
				return nil, NewEndpointErr(401, "JWT required")
			}

			newToken := uuid.NewString()

			target := resolveTarget(req.Target, s.Name)
			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return nil, NewBusinessErr(200, 300, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewBusinessErr(200, 300, "local node not initialized")
			}
			succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
				ctx,
				client.ResetAuthTokenReqType,
				&proto.ResetAuthTokenReq{Username: caller.Username, NewToken: newToken},
				s.GetClusterToken(),
			)
			if len(failedList) != 0 && len(succList) == 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("[ResetAuthToken] failed|User=%s|Err=%s", caller.Username, errMsg)
				return nil, NewBusinessErr(200, 300, errMsg)
			}
			msg := "ok"
			if len(failedList) != 0 {
				msg = joinFailedList(failedList)
			}
			return map[string]any{"code": 0, "msg": msg, "auth_token": newToken}, nil
		},
	}
}

// UserAddHandler POST /user — 添加用户
type UserAddHandler struct{ HttpHandlerImp }

func (handler *UserAddHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().userAddEndpoint())(c)
}

func (handler *UserAddHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserAddHandler) getRelativePath() string { return "/user" }

func (handler *UserAddHandler) help() string { return userAddHelpText }

// UserUpdateHandler PUT /user — 更新用户
type UserUpdateHandler struct{ HttpHandlerImp }

func (handler *UserUpdateHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().userUpdateEndpoint())(c)
}

func (handler *UserUpdateHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserUpdateHandler) getRelativePath() string { return "/user" }

func (handler *UserUpdateHandler) help() string { return userUpdateHelpText }

// UserDeleteHandler DELETE /user — 删除用户
type UserDeleteHandler struct{ HttpHandlerImp }

func (handler *UserDeleteHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().userDeleteEndpoint())(c)
}

func (handler *UserDeleteHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserDeleteHandler) getRelativePath() string { return "/user" }

func (handler *UserDeleteHandler) help() string { return userDeleteHelpText }

// UserResetHandler POST /user/reset — 重置用户
type UserResetHandler struct{ HttpHandlerImp }

func (handler *UserResetHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().userResetEndpoint())(c)
}

func (handler *UserResetHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserResetHandler) getRelativePath() string { return "/user/reset" }

func (handler *UserResetHandler) help() string { return userResetHelpText }

// UserResetTrafficHandler POST /user/reset-traffic — 重置用户累计持久化流量统计
type UserResetTrafficHandler struct{ HttpHandlerImp }

func (handler *UserResetTrafficHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().userResetTrafficEndpoint())(c)
}

func (handler *UserResetTrafficHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserResetTrafficHandler) getRelativePath() string { return "/user/reset-traffic" }

func (handler *UserResetTrafficHandler) help() string { return userResetTrafficHelpText }

// UserResetAuthTokenHandler POST /user/reset-token — reset the caller's own auth token.
// Available to any authenticated user (normal or admin).
type UserResetAuthTokenHandler struct{ HttpHandlerImp }

func (handler *UserResetAuthTokenHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().userResetAuthTokenEndpoint())(c)
}

func (handler *UserResetAuthTokenHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserResetAuthTokenHandler) getRelativePath() string { return "/user/reset-token" }

func (handler *UserResetAuthTokenHandler) help() string { return userResetAuthTokenHelpText }

const userListHelpText = `GET /api/user
	获取用户信息。管理员返回全部用户，普通用户返回自己的数据。
	query: target`

const profileHelpText = `GET /api/profile
	获取当前登录用户的个人信息，不区分角色，始终返回完整 profile。
	query: target`

// UserListReq is the request payload for GET /api/user (MCP tool: list_users).
type UserListReq struct {
	Target string `json:"target,omitempty" query:"target" default:"all" jsonschema:"Target node name or node ID (defaults to all nodes)"`
}

// ProfileReq is the request payload for GET /api/profile (MCP tool: get_profile).
type ProfileReq struct {
	Target string `json:"target,omitempty" query:"target" default:"all" jsonschema:"Target node name or node ID (defaults to all nodes)"`
}

func cloneGinHValue(v any) any {
	switch typed := v.(type) {
	case gin.H:
		if typed == nil {
			return gin.H(nil)
		}
		cp := make(gin.H, len(typed))
		for k, val := range typed {
			cp[k] = cloneGinHValue(val)
		}
		return cp
	case []gin.H:
		if typed == nil {
			return []gin.H(nil)
		}
		cp := make([]gin.H, len(typed))
		for i, item := range typed {
			if item == nil {
				cp[i] = nil
				continue
			}
			cp[i] = cloneGinHValue(item).(gin.H)
		}
		return cp
	case []string:
		if typed == nil {
			return []string(nil)
		}
		cp := make([]string, len(typed))
		copy(cp, typed)
		return cp
	default:
		return v
	}
}

// sanitizeUserEntriesForMCP deep-copies a node -> user/profile entry map for
// MCP callers and redacts non-empty "auth_token" values without mutating resp.
func sanitizeUserEntriesForMCP(resp map[string][]gin.H) any {
	if resp == nil {
		return nil
	}
	out := make(map[string][]gin.H, len(resp))
	for node, entries := range resp {
		if entries == nil {
			out[node] = nil
			continue
		}
		copiedEntries := make([]gin.H, len(entries))
		for i, entry := range entries {
			if entry == nil {
				copiedEntries[i] = nil
				continue
			}
			copiedEntry := make(gin.H, len(entry))
			for k, v := range entry {
				if k == "auth_token" {
					if tok, ok := v.(string); ok && tok != "" {
						copiedEntry[k] = "[REDACTED]"
						continue
					}
				}
				copiedEntry[k] = cloneGinHValue(v)
			}
			copiedEntries[i] = copiedEntry
		}
		out[node] = copiedEntries
	}
	return out
}

func (s *HttpServer) userListEndpoint() Endpoint[UserListReq, map[string][]gin.H] {
	return Endpoint[UserListReq, map[string][]gin.H]{
		Method:         http.MethodGet,
		Path:           "/user",
		Scope:          ScopeUser,
		Help:           userListHelpText,
		MCPName:        "list_users",
		MCPDescription: "List users across cluster nodes. Admins receive all users; normal users receive only their own profile.",
		ReadOnly:       true,
		MCPSanitize:    sanitizeUserEntriesForMCP,
		Execute: func(ctx context.Context, caller Caller, req UserListReq) (map[string][]gin.H, error) {
			if !caller.IsAdmin() {
				return s.execUserProfile(ctx, caller, req.Target)
			}
			target := req.Target
			if target == "" {
				target = "all"
			}
			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			succList, _, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.GetUsersReqType, &proto.GetUsersReq{IncludeAll: true}, s.GetClusterToken())

			result := map[string][]gin.H{}
			for node, v := range succList {
				users, ok := v.([]*proto.User)
				if !ok {
					continue
				}
				list := make([]gin.H, 0, len(users))
				for _, u := range users {
					inboundTags := make([]string, len(u.GetInboundTags()))
					copy(inboundTags, u.GetInboundTags())
					list = append(list, gin.H{
						"name":                     u.GetName(),
						"auth_token":               u.GetAuthToken(),
						"expire_time":              u.GetExpireTime(),
						"downlink":                 u.GetDownlink(),
						"uplink":                   u.GetUplink(),
						"role":                     u.GetRole(),
						"upload_bps":               u.GetUploadBps(),
						"download_bps":             u.GetDownloadBps(),
						"max_clients":              u.GetMaxClients(),
						"client_recycle_delay_sec": u.GetClientRecycleDelaySec(),
						"client_drain_sec":         u.GetClientDrainSec(),
						"group":                    u.GetGroup(),
						"inbound_tags":             inboundTags,
					})
				}
				result[node] = list
			}
			return result, nil
		},
	}
}

// execUserProfile is the shared logic for returning a user's own profile.
// Used by both userListEndpoint (GET /user for normal users)
// and profileEndpoint (GET /profile for any role).
func (s *HttpServer) execUserProfile(ctx context.Context, caller Caller, target string) (map[string][]gin.H, error) {
	if caller.Username == "" {
		return nil, NewEndpointErr(401, "JWT required")
	}
	if target == "" {
		target = "all"
	}
	nodes := s.GetTargetNodes(target)
	if len(nodes) == 0 {
		return nil, NewEndpointErr(404, "no available node")
	}

	rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
	succList, _, err := rpcClient.ReqToMultiEndNodeServer(
		ctx,
		client.GetProfileReqType,
		&proto.GetProfileReq{Username: caller.Username},
		s.GetClusterToken(),
	)
	if err != nil {
		return nil, NewEndpointErr(502, "RPC failed: "+err.Error())
	}

	result := map[string][]gin.H{}
	for nodeName, v := range succList {
		rsp, ok := v.(*proto.GetProfileRsp)
		if !ok || rsp.GetUsername() == "" {
			continue
		}
		var inbounds []gin.H
		for _, ib := range rsp.GetInbounds() {
			inbounds = append(inbounds, gin.H{
				"node":      nodeName,
				"tag":       ib.GetTag(),
				"container": ib.GetContainer(),
				"port":      ib.GetPort(),
			})
		}
		entry := gin.H{
			"name":                  rsp.GetUsername(),
			"expire_time":           rsp.GetExpireTime(),
			"downlink":              rsp.GetDownlink(),
			"uplink":                rsp.GetUplink(),
			"role":                  rsp.GetRole(),
			"auth_token":            rsp.GetProxyPassword(),
			"traffic_limit":         rsp.GetTrafficLimit(),
			"traffic_used_uplink":   rsp.GetUplink(),
			"traffic_used_downlink": rsp.GetDownlink(),
			"inbounds":              inbounds,
		}
		result[nodeName] = []gin.H{entry}
	}
	return result, nil
}

func (s *HttpServer) profileEndpoint() Endpoint[ProfileReq, map[string][]gin.H] {
	return Endpoint[ProfileReq, map[string][]gin.H]{
		Method:         http.MethodGet,
		Path:           "/profile",
		Scope:          ScopeUser,
		Help:           profileHelpText,
		MCPName:        "get_profile",
		MCPDescription: "Get the authenticated caller's own profile, traffic usage, and assigned inbounds (requires JWT authentication).",
		ReadOnly:       true,
		MCPSanitize:    sanitizeUserEntriesForMCP,
		Execute: func(ctx context.Context, caller Caller, req ProfileReq) (map[string][]gin.H, error) {
			return s.execUserProfile(ctx, caller, req.Target)
		},
	}
}

// UserListHandler GET /user — 获取用户信息
// Admin: returns all users across nodes (include_all).
// Normal user: returns only the caller's own data with auth_token and inbounds.
type UserListHandler struct{ HttpHandlerImp }

func (handler *UserListHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().userListEndpoint())(c)
}

func (handler *UserListHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserListHandler) getRelativePath() string { return "/user" }

func (handler *UserListHandler) help() string { return userListHelpText }

// ProfileHandler GET /profile — 获取当前登录用户的个人信息（不区分角色）
// 管理员和普通用户均走 GetProfile 逻辑，返回含 inbounds、traffic_limit 的完整 profile。
type ProfileHandler struct{ HttpHandlerImp }

func (handler *ProfileHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().profileEndpoint())(c)
}

func (handler *ProfileHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ProfileHandler) getRelativePath() string { return "/profile" }

func (handler *ProfileHandler) help() string { return profileHelpText }
