package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// SetUserClientLimitHandler PUT /user/:name/client-limit — sets client limits.
// Body: {"max_clients": 3, "recycle_delay_sec": 60, "drain_sec": 2, "target": ""}
// All three fields (max_clients, recycle_delay_sec, drain_sec) must be specified together.
// max_clients=0 means remove the limit (unlimited).
type SetUserClientLimitHandler struct{ HttpHandlerImp }

func (handler *SetUserClientLimitHandler) handlerFunc(c *gin.Context) {
	username := c.Param("name")
	if username == "" {
		c.JSON(400, gin.H{"code": 400, "msg": "username required"})
		return
	}

	var req struct {
		MaxClients      *int   `json:"max_clients"`
		RecycleDelaySec *int   `json:"recycle_delay_sec"`
		DrainSec        *int   `json:"drain_sec"`
		Target          string `json:"target"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid request body"})
		return
	}
	if req.MaxClients == nil {
		c.JSON(400, gin.H{"code": 400, "msg": "max_clients required"})
		return
	}
	if *req.MaxClients < 0 {
		c.JSON(400, gin.H{"code": 400, "msg": "max_clients must be >= 0"})
		return
	}
	if req.RecycleDelaySec != nil && *req.RecycleDelaySec < 0 {
		c.JSON(400, gin.H{"code": 400, "msg": "recycle_delay_sec must be >= 0"})
		return
	}
	if req.DrainSec != nil && *req.DrainSec < 0 {
		c.JSON(400, gin.H{"code": 400, "msg": "drain_sec must be >= 0"})
		return
	}
	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)

	userPoint := &proto.User{Name: username}
	// Map JSON max_clients to proto sentinel:
	// JSON 0 → proto -1 (remove limit)
	// JSON >0 → proto value (set limit)
	mc := int32(*req.MaxClients)
	if mc == 0 {
		mc = -1
	}
	userPoint.MaxClients = mc
	if req.RecycleDelaySec != nil {
		userPoint.ClientRecycleDelaySec = int32(*req.RecycleDelaySec)
	}
	if req.DrainSec != nil {
		userPoint.ClientDrainSec = int32(*req.DrainSec)
	}

	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.JSON(200, gin.H{"code": 404, "msg": "no available node"})
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(), client.UpdateUsersReqType,
		&proto.UserOpReq{Users: []*proto.User{userPoint}},
		handler.getHttpServer().GetClusterToken(),
	)
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("[SetUserClientLimit] Err=%s|User=%s", errMsg, username)
		c.JSON(500, gin.H{"code": 500, "msg": errMsg})
		return
	}

	log.Info("[SetUserClientLimit] updated", "username", username)
	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

func (handler *SetUserClientLimitHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *SetUserClientLimitHandler) getRelativePath() string {
	return "/user/:name/client-limit"
}

func (handler *SetUserClientLimitHandler) help() string {
	return `PUT /api/user/:name/client-limit
	Header: X-Token or Authorization: Bearer <admin-jwt>
	Body: {"max_clients": 3, "recycle_delay_sec": 60, "drain_sec": 2, "target": ""}
	Sets client connection limits. max_clients is required; 0 removes the limit.
	recycle_delay_sec and drain_sec are optional (unchanged if omitted).`
}
