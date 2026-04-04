package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// ClusterUserListHandler GET /cluster-users
type ClusterUserListHandler struct{ HttpHandlerImp }

func (handler *ClusterUserListHandler) handlerFunc(c *gin.Context) {
	target := c.DefaultQuery("target", handler.getHttpServer().Name)
	group := c.Query("group")
	nodes := handler.getHttpServer().GetTargetNodes(target)
	if len(nodes) == 0 {
		c.String(502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.ListClusterUsersReqType, &proto.ListClusterUsersReq{Group: group}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Target=%s", errMsg, target)
		c.String(500, errMsg)
		return
	}
	var users []*proto.ClusterUserInfo
	for _, v := range succList {
		if us, ok := v.([]*proto.ClusterUserInfo); ok {
			users = append(users, us...)
		}
	}
	if users == nil {
		users = []*proto.ClusterUserInfo{}
	}
	c.JSON(200, gin.H{"users": users})
}

func (handler *ClusterUserListHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ClusterUserListHandler) getRelativePath() string { return "/cluster-users" }

func (handler *ClusterUserListHandler) help() string {
	return `GET /cluster-users
	获取 cluster user 列表
	query: target（默认本节点）, group（可选过滤）`
}

// ClusterUserAddHandler POST /cluster-users
type ClusterUserAddHandler struct{ HttpHandlerImp }

func (handler *ClusterUserAddHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target      string `json:"target"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		Expire      int64  `json:"expire"`
		Role        string `json:"role"`
		TargetGroup string `json:"target_group"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(400, "invalid request body: %v", err)
		return
	}
	if req.Username == "" {
		c.String(400, "username is required")
		return
	}
	if req.Password == "" {
		c.String(400, "password is required")
		return
	}
	if req.Target == "" {
		req.Target = handler.getHttpServer().Name
	}
	userInfo := &proto.ClusterUserInfo{
		Username:    req.Username,
		Password:    req.Password,
		Expire:      req.Expire,
		Role:        req.Role,
		TargetGroup: req.TargetGroup,
	}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.String(502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.UpsertClusterUsersReqType, &proto.UpsertClusterUsersReq{Users: []*proto.ClusterUserInfo{userInfo}, FromAdmin: true}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Username=%s|Target=%s", errMsg, req.Username, req.Target)
		c.String(500, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *ClusterUserAddHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ClusterUserAddHandler) getRelativePath() string { return "/cluster-users" }

func (handler *ClusterUserAddHandler) help() string {
	return `POST /cluster-users
	新增 cluster user
	body: {"target": "", "username": "", "password": "", "expire": 0, "role": "normal", "target_group": "default"}`
}

// ClusterUserUpdateHandler PUT /cluster-users/:name
type ClusterUserUpdateHandler struct{ HttpHandlerImp }

func (handler *ClusterUserUpdateHandler) handlerFunc(c *gin.Context) {
	username := c.Param("name")
	var req struct {
		Target      string `json:"target"`
		Password    string `json:"password"`
		Expire      int64  `json:"expire"`
		Role        string `json:"role"`
		TargetGroup string `json:"target_group"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(400, "invalid request body: %v", err)
		return
	}
	if req.Password == "" && req.Expire == 0 && req.Role == "" && req.TargetGroup == "" {
		c.String(400, "at least one field (password, expire, role, target_group) must be provided")
		return
	}
	if req.Target == "" {
		req.Target = handler.getHttpServer().Name
	}
	userInfo := &proto.ClusterUserInfo{
		Username:    username,
		Password:    req.Password,
		Expire:      req.Expire,
		Role:        req.Role,
		TargetGroup: req.TargetGroup,
	}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.String(502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.UpsertClusterUsersReqType, &proto.UpsertClusterUsersReq{Users: []*proto.ClusterUserInfo{userInfo}, FromAdmin: true}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Username=%s|Target=%s", errMsg, username, req.Target)
		c.String(500, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *ClusterUserUpdateHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ClusterUserUpdateHandler) getRelativePath() string { return "/cluster-users/:name" }

func (handler *ClusterUserUpdateHandler) help() string {
	return `PUT /cluster-users/:name
	更新 cluster user（username 从 path 获取）
	body: {"target": "", "password": "", "expire": 0, "role": "normal", "target_group": "default"}`
}

// ClusterUserDeleteHandler DELETE /cluster-users/:name
type ClusterUserDeleteHandler struct{ HttpHandlerImp }

func (handler *ClusterUserDeleteHandler) handlerFunc(c *gin.Context) {
	username := c.Param("name")
	target := c.DefaultQuery("target", handler.getHttpServer().Name)
	nodes := handler.getHttpServer().GetTargetNodes(target)
	if len(nodes) == 0 {
		c.String(502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.DeleteClusterUsersReqType, &proto.DeleteClusterUsersReq{Usernames: []string{username}}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Username=%s|Target=%s", errMsg, username, target)
		c.String(500, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *ClusterUserDeleteHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ClusterUserDeleteHandler) getRelativePath() string { return "/cluster-users/:name" }

func (handler *ClusterUserDeleteHandler) help() string {
	return `DELETE /cluster-users/:name
	删除 cluster user（tombstone 语义）
	path: name — 用户名
	query: target（默认本节点）`
}
