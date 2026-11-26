package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const rotateInboundPortHelp = `/api/rotateInboundPort
	用户自助更换指定 container+inbound 的前置转发端口（JWT 鉴权，用户只能操作自己的 inbound）
	POST /api/rotateInboundPort
	Header: Authorization: Bearer <jwt>  OR  X-Token: <admin-token>
	Body: {"target":"node1","container":"xray","inbound":"<tag>","port":0}
	  target: 目标节点名，空则默认本机
	  X-Token: body 必须提供 username 字段
	  admin JWT: 可选 username 字段；不填默认操作自身
	  普通用户 JWT: 不允许指定他人 username（→ 403）
	port=0 随机分配，port>0 指定新端口
	Response: {"code":0,"port":12345,"msg":"ok"}`

// RotateInboundPortReq is the request payload for POST /rotateInboundPort (MCP tool: rotate_inbound_port).
type RotateInboundPortReq struct {
	Target    string `json:"target,omitempty"    form:"target"    jsonschema:"Target cluster node name or node_id (defaults to local node)."`
	Username  string `json:"username,omitempty"  form:"username"  jsonschema:"Target username (required for X-Token callers; optional for admin JWT; normal JWT may only operate on self)."`
	Container string `json:"container"           form:"container" jsonschema:"Proxy container name, such as 'xray', 'hysteria', 'snell', or 'mihomo'."`
	Inbound   string `json:"inbound"             form:"inbound"   jsonschema:"Inbound tag within the container whose forward port will be rotated."`
	Port      uint32 `json:"port,omitempty"      form:"port"      jsonschema:"Preferred new port (0 for random allocation, >0 for a specific port)."`
}

// RotateInboundPortResp is the response payload for POST /rotateInboundPort.
type RotateInboundPortResp struct {
	Code int    `json:"code"`
	Port uint32 `json:"port"`
	Msg  string `json:"msg"`
}

func (s *HttpServer) rotateInboundPortEndpoint() Endpoint[RotateInboundPortReq, RotateInboundPortResp] {
	return Endpoint[RotateInboundPortReq, RotateInboundPortResp]{
		Method:         http.MethodPost,
		Path:           "/rotateInboundPort",
		Scope:          ScopeUser,
		Help:           rotateInboundPortHelp,
		MCPName:        "rotate_inbound_port",
		MCPDescription: "Rotate the frontend forward port for a specific container and inbound belonging to a user. Normal users may only rotate their own inbound; admins may specify another username.",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req RotateInboundPortReq) (RotateInboundPortResp, error) {
			if req.Container == "" || req.Inbound == "" {
				return RotateInboundPortResp{}, NewEndpointErr(http.StatusBadRequest, "container and inbound are required")
			}

			var targetUser string
			if caller.Username == "" {
				// X-Token path: no user identity in context, username required in body.
				if req.Username == "" {
					return RotateInboundPortResp{}, NewEndpointErr(http.StatusBadRequest, "username required for X-Token requests")
				}
				targetUser = req.Username
			} else {
				// JWT path: default to caller's own username.
				targetUser = caller.Username
				if req.Username != "" && req.Username != caller.Username {
					if !caller.IsAdmin() {
						return RotateInboundPortResp{}, NewEndpointErr(http.StatusForbidden, "only admin can rotate another user's inbound port")
					}
					targetUser = req.Username
				}
			}

			targetNode := resolveTarget(req.Target, s.Name)
			nodes := s.GetTargetNodes(targetNode)
			if len(nodes) == 0 {
				return RotateInboundPortResp{}, NewEndpointErr(http.StatusBadGateway, "no available node")
			}

			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return RotateInboundPortResp{}, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
				ctx,
				client.RotateInboundPortReqType,
				&proto.RotateInboundPortReq{
					Username:  targetUser,
					Container: req.Container,
					Inbound:   req.Inbound,
					Port:      req.Port,
				},
				s.GetClusterToken(),
			)
			// RPC transport failure — node unreachable / connection refused etc.
			if len(failedList) != 0 && len(succList) == 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|User=%s|Target=%s", errMsg, targetUser, targetNode)
				return RotateInboundPortResp{}, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}

			// Extract structured response from the target node.
			for _, v := range succList {
				if rsp, ok := v.(*proto.RotateInboundPortRsp); ok && rsp != nil {
					return RotateInboundPortResp{
						Code: int(rsp.GetCode()),
						Port: rsp.GetPort(),
						Msg:  rsp.GetMsg(),
					}, nil
				}
			}

			return RotateInboundPortResp{}, NewEndpointErr(http.StatusInternalServerError, "no response from target node")
		},
	}
}

// RotateInboundPortHandler POST /rotateInboundPort — 用户自助更换指定 inbound 的前置转发端口。
// 认证由 AuthMiddleware 在路由层完成（userGroup），无需 handler 内部再做密码校验。
//
// 认证规则：
//   - X-Token（break-glass admin）：必须在 body 中显式提供 username。
//   - admin JWT：可选 body username；未指定时操作 token 对应用户。
//   - 普通用户 JWT：只能操作自己的 inbound；body 中指定他人 username → 403。
type RotateInboundPortHandler struct{ HttpHandlerImp }

func (handler *RotateInboundPortHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().rotateInboundPortEndpoint())(c)
}

func (handler *RotateInboundPortHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *RotateInboundPortHandler) getRelativePath() string { return "/rotateInboundPort" }

func (handler *RotateInboundPortHandler) help() string {
	return rotateInboundPortHelp
}
