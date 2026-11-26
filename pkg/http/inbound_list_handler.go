package http

import (
	"context"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const inboundListHelp = `GET /api/inbounds
	列出所有节点的所有 inbound（跨 xray/snell/hysteria/mihomo container）
	query: target（默认当前节点）
	返回: [{"container":"xray","name":"tag1","inbound_type":"direct|chain","access_tags":[...],"remark":"...","chain_protocol":"...","chain_target":"..."}, ...]`

// InboundListReq is the request payload for GET /api/inbounds and MCP tool list_inbounds.
type InboundListReq struct {
	Target string `json:"target,omitempty" query:"target" default:"all" jsonschema:"Target node name, node_id, or 'all' to query every cluster node (defaults to 'all')"`
}

func (s *HttpServer) inboundListEndpoint() Endpoint[InboundListReq, any] {
	return Endpoint[InboundListReq, any]{
		Method:         "GET",
		Path:           "/inbounds",
		Scope:          ScopeAdmin,
		Help:           inboundListHelp,
		MCPName:        "list_inbounds",
		MCPDescription: "List all proxy inbounds across all containers (xray, snell, hysteria, mihomo) on the target cluster node(s). Target defaults to 'all' (every cluster node) or accepts a specific node name or node_id.",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req InboundListReq) (any, error) {
			target := req.Target
			if target == "" {
				target = "all"
			}
			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
				ctx,
				client.ListInboundReqType,
				&proto.ListInboundReq{},
				s.GetClusterToken(),
			)
			if len(succList) > 0 {
				return succList, nil
			}
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|OpType=listInbound|Target=%s", errMsg, target)
				return nil, NewEndpointErr(500, errMsg)
			}
			return []interface{}{}, nil
		},
	}
}

// InboundListHandler GET /inbounds — 列出所有 inbound（跨所有 container）
type InboundListHandler struct{ HttpHandlerImp }

func (handler *InboundListHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().inboundListEndpoint())(c)
}

func (handler *InboundListHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *InboundListHandler) getRelativePath() string { return "/inbounds" }

func (handler *InboundListHandler) help() string {
	return inboundListHelp
}

const inboundDeleteByNameHelp = `DELETE /api/inbounds
	按 container 类型 + name 删除 inbound
	body: {"target": "", "container": "xray|snell|hysteria|mihomo", "name": "<inbound tag>"}`

// InboundDeleteByNameReq is the request payload for DELETE /api/inbounds and MCP tool delete_inbound.
type InboundDeleteByNameReq struct {
	Target    string `json:"target,omitempty" jsonschema:"Target node name, node_id, or 'all' (defaults to the local node when omitted)"`
	Container string `json:"container" jsonschema:"Container type owning the inbound: 'xray', 'snell', 'hysteria', or 'mihomo' (required)"`
	Name      string `json:"name" jsonschema:"Inbound tag/name to delete (required)"`
}

func (s *HttpServer) inboundDeleteByNameEndpoint() Endpoint[InboundDeleteByNameReq, map[string]any] {
	return Endpoint[InboundDeleteByNameReq, map[string]any]{
		Method:         "DELETE",
		Path:           "/inbounds",
		Scope:          ScopeAdmin,
		Help:           inboundDeleteByNameHelp,
		MCPName:        "delete_inbound",
		MCPDescription: "Delete an inbound by container type and inbound tag name on the target cluster node(s). This removes the inbound listener and disconnects active proxy sessions using it. Target defaults to the local node when omitted.",
		ReadOnly:       false,
		Destructive:    true,
		Execute: func(ctx context.Context, caller Caller, req InboundDeleteByNameReq) (map[string]any, error) {
			if req.Container == "" {
				return nil, NewEndpointErr(400, "container is required")
			}
			if req.Name == "" {
				return nil, NewEndpointErr(400, "name is required")
			}
			req.Target = resolveTarget(req.Target, s.Name)
			nodes := s.GetTargetNodes(req.Target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
				ctx,
				client.DeleteInboundByNameReqType,
				&proto.DeleteInboundByNameReq{Container: req.Container, Name: req.Name},
				s.GetClusterToken(),
			)
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|OpType=deleteInboundByName|Target=%s|Container=%s|Name=%s", errMsg, req.Target, req.Container, req.Name)
				return nil, NewEndpointErr(500, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

// InboundDeleteByNameHandler DELETE /inbounds — 按 container + name 删除 inbound
type InboundDeleteByNameHandler struct{ HttpHandlerImp }

func (handler *InboundDeleteByNameHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().inboundDeleteByNameEndpoint())(c)
}

func (handler *InboundDeleteByNameHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *InboundDeleteByNameHandler) getRelativePath() string { return "/inbounds" }

func (handler *InboundDeleteByNameHandler) help() string {
	return inboundDeleteByNameHelp
}

const inboundSetTagsHelp = `PUT /api/inbounds/tags
	更新指定 inbound 的 Layer 2 访问控制标签 (access_tags)
	body: {"target": "", "container": "xray|snell|hysteria|mihomo", "name": "<inbound tag>", "access_tags": ["tag1", "tag2"]}`

// SetInboundTagsReq is the request payload for PUT /api/inbounds/tags and MCP tool set_inbound_tags.
type SetInboundTagsReq struct {
	Target     string   `json:"target,omitempty" jsonschema:"Target node name, node_id, or 'all' (defaults to the local node when omitted)"`
	Container  string   `json:"container" jsonschema:"Container type owning the inbound: 'xray', 'snell', 'hysteria', or 'mihomo' (required)"`
	Name       string   `json:"name" jsonschema:"Inbound tag/name to update (required)"`
	AccessTags []string `json:"access_tags,omitempty" jsonschema:"Layer 2 access tags controlling which users can access this inbound (empty clears tags so inbound is unrestricted)"`
}

func (s *HttpServer) inboundSetTagsEndpoint() Endpoint[SetInboundTagsReq, map[string]any] {
	return Endpoint[SetInboundTagsReq, map[string]any]{
		Method:         "PUT",
		Path:           "/inbounds/tags",
		Scope:          ScopeAdmin,
		Help:           inboundSetTagsHelp,
		MCPName:        "set_inbound_tags",
		MCPDescription: "Replace Layer 2 access_tags on an existing inbound across target cluster node(s). Pass an empty access_tags array to clear restrictions so all visible users can access the inbound. Target defaults to the local node when omitted.",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req SetInboundTagsReq) (map[string]any, error) {
			containerType := strings.TrimSpace(req.Container)
			if containerType == "" {
				return nil, NewEndpointErr(400, "container is required")
			}
			name := strings.TrimSpace(req.Name)
			if name == "" {
				return nil, NewEndpointErr(400, "name is required")
			}
			tags, err := contracts.NormalizeAccessTags(req.AccessTags)
			if err != nil {
				return nil, NewEndpointErr(400, fmt.Sprintf("invalid access_tags: %v", err))
			}

			req.Target = resolveTarget(req.Target, s.Name)
			nodes := s.GetTargetNodes(req.Target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
				ctx,
				client.SetInboundAccessTagsReqType,
				&proto.SetInboundAccessTagsReq{
					Container:  containerType,
					Name:       name,
					AccessTags: tags,
				},
				s.GetClusterToken(),
			)
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|OpType=setInboundAccessTags|Target=%s|Container=%s|Name=%s", errMsg, req.Target, containerType, name)
				return nil, NewEndpointErr(500, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

// InboundSetTagsHandler PUT /inbounds/tags — 更新 inbound 的 access_tags
type InboundSetTagsHandler struct{ HttpHandlerImp }

func (handler *InboundSetTagsHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().inboundSetTagsEndpoint())(c)
}

func (handler *InboundSetTagsHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *InboundSetTagsHandler) getRelativePath() string { return "/inbounds/tags" }

func (handler *InboundSetTagsHandler) help() string {
	return inboundSetTagsHelp
}
