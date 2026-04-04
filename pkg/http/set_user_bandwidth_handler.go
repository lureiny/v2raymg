package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// SetUserBandwidthHandler PUT /user/:name/bandwidth — sets bandwidth limits.
// Body: {"upload_bps": 1000000, "download_bps": 500000, "target": ""}
// A value of 0 means remove the limit (unlimited). Omitting a field means no change.
type SetUserBandwidthHandler struct{ HttpHandlerImp }

func (handler *SetUserBandwidthHandler) handlerFunc(c *gin.Context) {
	username := c.Param("name")
	if username == "" {
		c.JSON(400, gin.H{"code": 400, "msg": "username required"})
		return
	}

	var req struct {
		UploadBps   *int64 `json:"upload_bps"`
		DownloadBps *int64 `json:"download_bps"`
		Target      string `json:"target"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid request body"})
		return
	}
	if req.UploadBps == nil && req.DownloadBps == nil {
		c.JSON(400, gin.H{"code": 400, "msg": "at least one of upload_bps or download_bps required"})
		return
	}
	if req.UploadBps != nil && *req.UploadBps < 0 {
		c.JSON(400, gin.H{"code": 400, "msg": "upload_bps must be >= 0"})
		return
	}
	if req.DownloadBps != nil && *req.DownloadBps < 0 {
		c.JSON(400, gin.H{"code": 400, "msg": "download_bps must be >= 0"})
		return
	}
	if req.Target == "" {
		req.Target = handler.getHttpServer().Name
	}

	userPoint := &proto.User{Name: username}
	// Map JSON values to proto sentinel convention:
	// JSON null/absent → proto 0 (no change)
	// JSON 0 → proto -1 (remove limit)
	// JSON >0 → proto value (set limit)
	if req.UploadBps != nil {
		v := *req.UploadBps
		if v == 0 {
			v = -1
		}
		userPoint.UploadBps = v
	}
	if req.DownloadBps != nil {
		v := *req.DownloadBps
		if v == 0 {
			v = -1
		}
		userPoint.DownloadBps = v
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
		log.Errorf("[SetUserBandwidth] Err=%s|User=%s", errMsg, username)
		c.JSON(500, gin.H{"code": 500, "msg": errMsg})
		return
	}

	log.Info("[SetUserBandwidth] updated", "username", username)
	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

func (handler *SetUserBandwidthHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *SetUserBandwidthHandler) getRelativePath() string {
	return "/user/:name/bandwidth"
}

func (handler *SetUserBandwidthHandler) help() string {
	return `PUT /api/user/:name/bandwidth
	Header: X-Token or Authorization: Bearer <admin-jwt>
	Body: {"upload_bps": 1000000, "download_bps": 500000, "target": ""}
	Sets bandwidth limits (bytes/sec). 0 removes the limit (unlimited).`
}
