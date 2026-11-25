package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const rotateAllPortsHelp = `/api/rotateAllPorts
	用户自助重置自身所有 inbound 的前置转发端口（JWT 鉴权，用户只能操作自己的 inbound）
	POST /api/rotateAllPorts
	Header: Authorization: Bearer <jwt>  OR  X-Token: <admin-token>
	Body (optional): {"target":"node1","username": "target-user"}
	  target: 目标节点名，空则默认本机
	  X-Token: body 必须提供 username 字段
	  admin JWT: 可选 username 字段；不填默认操作自身
	  普通用户 JWT: 不允许指定他人 username（→ 403）
	Response: {"code":0,"ports":{"xray:vless-tcp":23456,"xray:trojan-tls":34567},"msg":"ok"}
	部分成功时 code=301，ports 包含已成功轮换的 inbound，msg 包含失败原因
	全部失败时 code=300，ports 为空`

// RotateAllPortsReq is the request payload for POST /rotateAllPorts (MCP tool: rotate_all_ports).
type RotateAllPortsReq struct {
	Target   string `json:"target,omitempty"   form:"target"   jsonschema:"Target cluster node name or node_id (defaults to local node)."`
	Username string `json:"username,omitempty" form:"username" jsonschema:"Target username (required for X-Token callers; optional for admin JWT; normal JWT may only operate on self)."`
}

// RotateAllPortsResp is the response payload for POST /rotateAllPorts.
type RotateAllPortsResp struct {
	Code  int               `json:"code"`
	Ports map[string]uint32 `json:"ports"`
	Msg   string            `json:"msg"`
}

func (s *HttpServer) rotateAllPortsEndpoint() Endpoint[RotateAllPortsReq, RotateAllPortsResp] {
	return Endpoint[RotateAllPortsReq, RotateAllPortsResp]{
		Method:         http.MethodPost,
		Path:           "/rotateAllPorts",
		Scope:          ScopeUser,
		Help:           rotateAllPortsHelp,
		OptionalBody:   true,
		MCPName:        "rotate_all_ports",
		MCPDescription: "Rotate all frontend forward ports across every inbound for a user on the target node. This disconnects or replaces existing port mappings for all of the user's inbounds.",
		ReadOnly:       false,
		Destructive:    true,
		Execute: func(ctx context.Context, caller Caller, req RotateAllPortsReq) (RotateAllPortsResp, error) {
			var targetUser string
			if caller.Username == "" {
				// X-Token path: no user identity in context, username required in body.
				if req.Username == "" {
					return RotateAllPortsResp{}, NewEndpointErr(http.StatusBadRequest, "username required for X-Token requests")
				}
				targetUser = req.Username
			} else {
				// JWT path: default to caller's own username.
				targetUser = caller.Username
				if req.Username != "" && req.Username != caller.Username {
					if !caller.IsAdmin() {
						return RotateAllPortsResp{}, NewEndpointErr(http.StatusForbidden, "only admin can rotate another user's ports")
					}
					targetUser = req.Username
				}
			}

			targetNode := resolveTarget(req.Target, s.Name)
			nodes := s.GetTargetNodes(targetNode)
			if len(nodes) == 0 {
				return RotateAllPortsResp{}, NewEndpointErr(http.StatusBadGateway, "no available node")
			}

			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return RotateAllPortsResp{}, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
				ctx,
				client.RotateAllPortsReqType,
				&proto.RotateAllPortsReq{Username: targetUser},
				s.GetClusterToken(),
			)
			// RPC transport failure — node unreachable / connection refused etc.
			if len(failedList) != 0 && len(succList) == 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|User=%s|Target=%s", errMsg, targetUser, targetNode)
				return RotateAllPortsResp{}, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}

			// Extract structured response from the target node.
			// code=0: all succeeded; code=301: partial success (ports has succeeded inbounds);
			// code=300: full failure.
			for _, v := range succList {
				if rsp, ok := v.(*proto.RotateAllPortsRsp); ok && rsp != nil {
					return RotateAllPortsResp{
						Code:  int(rsp.GetCode()),
						Ports: rsp.GetPorts(),
						Msg:   rsp.GetMsg(),
					}, nil
				}
			}

			return RotateAllPortsResp{}, NewEndpointErr(http.StatusInternalServerError, "no response from target node")
		},
	}
}

// RotateAllPortsHandler POST /rotateAllPorts — 用户自助重置自身所有 inbound 的前置转发端口。
// 认证由 AuthMiddleware 在路由层完成（userGroup），无需 handler 内部再做密码校验。
// 对每个 inbound 独立执行 make-before-break，返回所有新端口映射。
//
// 认证规则：
//   - X-Token（break-glass admin）：必须在 body 中显式提供 username。
//   - admin JWT：可选 body username；未指定时操作 token 对应用户。
//   - 普通用户 JWT：只能操作自己的所有 inbound；body 中指定他人 username → 403。
type RotateAllPortsHandler struct{ HttpHandlerImp }

func (handler *RotateAllPortsHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().rotateAllPortsEndpoint())(c)
}

func (handler *RotateAllPortsHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *RotateAllPortsHandler) getRelativePath() string { return "/rotateAllPorts" }

func (handler *RotateAllPortsHandler) help() string {
	return rotateAllPortsHelp
}
