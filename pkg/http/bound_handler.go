package http

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const inboundAddHelp = `POST /api/inbound
	添加inbound（仅支持 xray container）
	body: {"target": "", "bound_raw_string": "", "container": "xray"}
	bound_raw_string: json中inbound配置base64编码后的字符串
	container: 可选，目前仅支持 "xray"（默认）；其他container请使用 POST /api/inbound/fast`

// InboundAddReq is the request payload for POST /api/inbound and MCP tool add_inbound.
type InboundAddReq struct {
	Target         string `json:"target,omitempty" jsonschema:"Target node name, node_id, or 'all' (defaults to the local node when omitted)"`
	BoundRawString string `json:"bound_raw_string" jsonschema:"Base64-encoded xray native JSON inbound configuration"`
	Container      string `json:"container,omitempty" jsonschema:"Container type; optional and only 'xray' is supported by add_inbound (defaults to 'xray'). Use fast_add_inbound for other containers."`
}

func (s *HttpServer) inboundAddEndpoint() Endpoint[InboundAddReq, map[string]any] {
	return Endpoint[InboundAddReq, map[string]any]{
		Method:         "POST",
		Path:           "/inbound",
		Scope:          ScopeAdmin,
		Help:           inboundAddHelp,
		MCPName:        "add_inbound",
		MCPDescription: "Add a raw xray inbound configuration (base64-encoded xray inbound JSON) on the target cluster node(s). Only container 'xray' is supported; use fast_add_inbound for other containers or simplified inbound creation. Target defaults to the local node when omitted.",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req InboundAddReq) (map[string]any, error) {
			// Only xray is supported via this raw-JSON path; other containers must use POST /inbound/fast
			if req.Container != "" && req.Container != "xray" {
				return nil, NewEndpointErr(400, fmt.Sprintf("container %q not supported via POST /inbound; use POST /inbound/fast instead", req.Container))
			}
			req.Target = resolveTarget(req.Target, s.Name)
			nodes := s.GetTargetNodes(req.Target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.AddInboundReqType, &proto.InboundOpReq{InboundInfo: req.BoundRawString}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|OpType=addInbound|Target=%s", errMsg, req.Target)
				return nil, NewEndpointErr(500, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

// InboundAddHandler POST /inbound — 添加inbound
type InboundAddHandler struct{ HttpHandlerImp }

func (handler *InboundAddHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().inboundAddEndpoint())(c)
}

func (handler *InboundAddHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *InboundAddHandler) getRelativePath() string { return "/inbound" }

func (handler *InboundAddHandler) help() string {
	return inboundAddHelp
}

const inboundGetHelp = `GET /api/inbound
	获取inbound详细配置（仅支持 xray container）
	query: target, src_tag, container(默认xray)`

// InboundGetReq is the request payload for GET /api/inbound and MCP tool get_inbound.
type InboundGetReq struct {
	Target    string `json:"target,omitempty" query:"target" default:"all" jsonschema:"Target node name, node_id, or 'all' to query every cluster node (defaults to 'all')"`
	SrcTag    string `json:"src_tag,omitempty" query:"src_tag" jsonschema:"Optional inbound tag to filter by; omit or leave empty to fetch all xray inbounds"`
	Container string `json:"container,omitempty" query:"container" default:"xray" jsonschema:"Container type; only 'xray' is supported by get_inbound (defaults to 'xray')"`
}

func (s *HttpServer) inboundGetEndpoint() Endpoint[InboundGetReq, any] {
	return Endpoint[InboundGetReq, any]{
		Method:         "GET",
		Path:           "/inbound",
		Scope:          ScopeAdmin,
		Help:           inboundGetHelp,
		MCPName:        "get_inbound",
		MCPDescription: "Get detailed inbound configuration for the 'xray' container on the target cluster node(s). Only container 'xray' is supported (use list_inbounds to list inbounds across all containers); target defaults to 'all' and container defaults to 'xray'.",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req InboundGetReq) (any, error) {
			if req.Target == "" {
				req.Target = "all"
			}
			if req.Container == "" {
				req.Container = "xray"
			}
			// Only xray is supported; other containers not yet implemented
			if req.Container != "xray" {
				return nil, NewEndpointErr(400, fmt.Sprintf("container %q not supported via GET /inbound", req.Container))
			}
			nodes := s.GetTargetNodes(req.Target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.GetInboundReqType, &proto.GetInboundReq{Tag: req.SrcTag}, s.GetClusterToken())
			if len(succList) > 0 {
				return succList, nil
			}
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|OpType=getInbound|Target=%s", errMsg, req.Target)
				return nil, NewEndpointErr(500, errMsg)
			}
			return gin.H{"code": 0, "msg": "ok"}, nil
		},
	}
}

// InboundGetHandler GET /inbound — 获取inbound
type InboundGetHandler struct{ HttpHandlerImp }

func (handler *InboundGetHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().inboundGetEndpoint())(c)
}

func (handler *InboundGetHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *InboundGetHandler) getRelativePath() string { return "/inbound" }

func (handler *InboundGetHandler) help() string {
	return inboundGetHelp
}
