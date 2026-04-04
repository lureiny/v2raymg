package http

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
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

// UserAddHandler POST /user — 添加用户
type UserAddHandler struct{ HttpHandlerImp }

func (handler *UserAddHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target string `json:"target"`
		User   string `json:"user"`
		Pwd    string `json:"pwd"`
		Tags   string `json:"tags"`
		Expire uint64 `json:"expire"`
		TTL    uint64 `json:"ttl"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(400, "invalid request body: %v", err)
		return
	}
	if req.Target == "" {
		req.Target = handler.getHttpServer().Name
	}
	expire := calcExpire(req.Expire, req.TTL)
	tagList := splitAndFilter(req.Tags)
	userPoint := &proto.User{Name: req.User, Passwd: req.Pwd, ExpireTime: expire, Tags: tagList}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.AddUsersReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *UserAddHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserAddHandler) getRelativePath() string { return "/user" }

func (handler *UserAddHandler) help() string {
	return `POST /user
	添加用户
	body: {"target": "", "user": "", "pwd": "", "tags": "", "expire": 0, "ttl": 0}
	user: 用户名, pwd: password, expire: 过期时间戳(与ttl同时存在时优先使用ttl), ttl: 存活时间(秒), tags: inbound tag列表(逗号分隔)`
}

// UserUpdateHandler PUT /user — 更新用户
type UserUpdateHandler struct{ HttpHandlerImp }

func (handler *UserUpdateHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target string `json:"target"`
		User   string `json:"user"`
		Pwd    string `json:"pwd"`
		Expire uint64 `json:"expire"`
		TTL    uint64 `json:"ttl"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(400, "invalid request body: %v", err)
		return
	}
	if req.Target == "" {
		req.Target = handler.getHttpServer().Name
	}
	expire := calcExpire(req.Expire, req.TTL)
	userPoint := &proto.User{Name: req.User, Passwd: req.Pwd, ExpireTime: expire}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.UpdateUsersReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *UserUpdateHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserUpdateHandler) getRelativePath() string { return "/user" }

func (handler *UserUpdateHandler) help() string {
	return `PUT /user
	更新用户信息
	body: {"target": "", "user": "", "pwd": "", "expire": 0, "ttl": 0}`
}

// UserDeleteHandler DELETE /user — 删除用户
type UserDeleteHandler struct{ HttpHandlerImp }

func (handler *UserDeleteHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target string `json:"target"`
		User   string `json:"user"`
		Tags   string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(400, "invalid request body: %v", err)
		return
	}
	if req.Target == "" {
		req.Target = handler.getHttpServer().Name
	}
	tagList := splitAndFilter(req.Tags)
	userPoint := &proto.User{Name: req.User, Tags: tagList}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.DeleteUsersReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *UserDeleteHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserDeleteHandler) getRelativePath() string { return "/user" }

func (handler *UserDeleteHandler) help() string {
	return `DELETE /user
	删除用户
	body: {"target": "", "user": "", "tags": ""}`
}

// UserResetHandler POST /user/reset — 重置用户
type UserResetHandler struct{ HttpHandlerImp }

func (handler *UserResetHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target string `json:"target"`
		User   string `json:"user"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(400, "invalid request body: %v", err)
		return
	}
	if req.Target == "" {
		req.Target = handler.getHttpServer().Name
	}
	userPoint := &proto.User{Name: req.User}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.ResetUserReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *UserResetHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserResetHandler) getRelativePath() string { return "/user/reset" }

func (handler *UserResetHandler) help() string {
	return `POST /user/reset
	重置用户proxy密钥
	body: {"target": "", "user": ""}`
}

// UserListHandler GET /user — 获取用户列表
type UserListHandler struct{ HttpHandlerImp }

func (handler *UserListHandler) handlerFunc(c *gin.Context) {
	target := c.DefaultQuery("target", handler.getHttpServer().Name)
	nodes := handler.getHttpServer().GetTargetNodes(target)
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, _, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.GetUsersReqType, &proto.GetUsersReq{}, handler.getHttpServer().GetClusterToken())
	c.JSON(200, succList)
}

func (handler *UserListHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserListHandler) getRelativePath() string { return "/user" }

func (handler *UserListHandler) help() string {
	return `GET /user
	获取用户列表
	query: target`
}
