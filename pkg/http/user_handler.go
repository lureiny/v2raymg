package http

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
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
		Target                string `json:"target"`
		User                  string `json:"user"`
		Pwd                   string `json:"pwd"`
		Expire                uint64 `json:"expire"`
		TTL                   uint64 `json:"ttl"`
		Role                  string `json:"role"`
		Group                 string `json:"group"`
		UploadBps             int64  `json:"upload_bps"`
		DownloadBps           int64  `json:"download_bps"`
		MaxClients            int32  `json:"max_clients"`
		ClientRecycleDelaySec int32  `json:"client_recycle_delay_sec"`
		ClientDrainSec        int32  `json:"client_drain_sec"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)
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
	}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.AddUsersReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
		jsonErr(c, 500, errMsg)
		return
	}
	jsonOK(c)
}

func (handler *UserAddHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserAddHandler) getRelativePath() string { return "/user" }

func (handler *UserAddHandler) help() string {
	return `POST /api/user
	添加用户
	body: {"target": "", "user": "", "pwd": "", "expire": 0, "ttl": 0}
	user: 用户名, pwd: password, expire: 过期时间戳(与ttl同时存在时优先使用ttl), ttl: 存活时间(秒)`
}

// UserUpdateHandler PUT /user — 更新用户
type UserUpdateHandler struct{ HttpHandlerImp }

func (handler *UserUpdateHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target                string `json:"target"`
		User                  string `json:"user"`
		Pwd                   string `json:"pwd"`
		Expire                uint64 `json:"expire"`
		TTL                   uint64 `json:"ttl"`
		Role                  string `json:"role"`
		UploadBps             int64  `json:"upload_bps"`
		DownloadBps           int64  `json:"download_bps"`
		MaxClients            int32  `json:"max_clients"`
		ClientRecycleDelaySec int32  `json:"client_recycle_delay_sec"`
		ClientDrainSec        int32  `json:"client_drain_sec"`
		Group                 string `json:"group"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)
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
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.UpdateUsersReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
		jsonErr(c, 500, errMsg)
		return
	}
	jsonOK(c)
}

func (handler *UserUpdateHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserUpdateHandler) getRelativePath() string { return "/user" }

func (handler *UserUpdateHandler) help() string {
	return `PUT /api/user
	更新用户信息
	body: {"target": "", "user": "", "pwd": "", "expire": 0, "ttl": 0, "role": "", "upload_bps": 0, "download_bps": 0, "max_clients": 0, "client_recycle_delay_sec": 0, "client_drain_sec": 0}`
}

// UserDeleteHandler DELETE /user — 删除用户
type UserDeleteHandler struct{ HttpHandlerImp }

func (handler *UserDeleteHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target string `json:"target"`
		User   string `json:"user"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)
	userPoint := &proto.User{Name: req.User}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.DeleteUsersReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
		jsonErr(c, 500, errMsg)
		return
	}
	jsonOK(c)
}

func (handler *UserDeleteHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserDeleteHandler) getRelativePath() string { return "/user" }

func (handler *UserDeleteHandler) help() string {
	return `DELETE /api/user
	删除用户
	body: {"target": "", "user": ""}`
}

// UserResetHandler POST /user/reset — 重置用户
type UserResetHandler struct{ HttpHandlerImp }

func (handler *UserResetHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target string `json:"target"`
		User   string `json:"user"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)
	userPoint := &proto.User{Name: req.User}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.ResetUserReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|User=%s|Target=%s", errMsg, req.User, req.Target)
		jsonErr(c, 500, errMsg)
		return
	}
	jsonOK(c)
}

func (handler *UserResetHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserResetHandler) getRelativePath() string { return "/user/reset" }

func (handler *UserResetHandler) help() string {
	return `POST /api/user/reset
	重置用户proxy密钥
	body: {"target": "", "user": ""}`
}

// UserResetAuthTokenHandler POST /user/reset-token — reset the caller's own auth token.
// Available to any authenticated user (normal or admin).
type UserResetAuthTokenHandler struct{ HttpHandlerImp }

func (handler *UserResetAuthTokenHandler) handlerFunc(c *gin.Context) {
	usernameVal, exists := c.Get(auth.ContextKeyUsername)
	if !exists {
		c.JSON(401, gin.H{"code": 401, "msg": "JWT required"})
		return
	}
	username, _ := usernameVal.(string)

	type tokenResetter interface {
		ResetAuthToken(username string) (string, error)
	}
	ul := handler.getHttpServer().userLister
	resetter, ok := ul.(tokenResetter)
	if !ok {
		c.JSON(500, gin.H{"code": 500, "msg": "server does not support token reset"})
		return
	}
	newToken, err := resetter.ResetAuthToken(username)
	if err != nil {
		log.Error("ResetAuthToken failed", "user", username, "err", err)
		c.JSON(500, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "ok", "auth_token": newToken})
}

func (handler *UserResetAuthTokenHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserResetAuthTokenHandler) getRelativePath() string { return "/user/reset-token" }

func (handler *UserResetAuthTokenHandler) help() string {
	return `POST /api/user/reset-token
	重置当前用户的auth token`
}

// UserListHandler GET /user — 获取用户信息
// Admin: returns all users across nodes (include_all).
// Normal user: returns only the caller's own data with auth_token and inbounds.
type UserListHandler struct{ HttpHandlerImp }

func (handler *UserListHandler) handlerFunc(c *gin.Context) {
	role, _ := c.Get(auth.ContextKeyRole)
	if role == "admin" {
		handler.serveAdmin(c)
	} else {
		handler.serveNormal(c)
	}
}

// serveAdmin returns all users via RPC GetUsers (admin view).
func (handler *UserListHandler) serveAdmin(c *gin.Context) {
	target := getTargetFromQuery(c)
	nodes := handler.getHttpServer().GetTargetNodes(target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, _, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.GetUsersReqType, &proto.GetUsersReq{IncludeAll: true}, handler.getHttpServer().GetClusterToken())

	result := map[string][]gin.H{}
	for node, v := range succList {
		users, ok := v.([]*proto.User)
		if !ok {
			continue
		}
		list := make([]gin.H, 0, len(users))
		for _, u := range users {
			list = append(list, gin.H{
				"name":                      u.GetName(),
				"auth_token":                u.GetAuthToken(),
				"expire_time":               u.GetExpireTime(),
				"downlink":                  u.GetDownlink(),
				"uplink":                    u.GetUplink(),
				"role":                      u.GetRole(),
				"upload_bps":                u.GetUploadBps(),
				"download_bps":              u.GetDownloadBps(),
				"max_clients":               u.GetMaxClients(),
				"client_recycle_delay_sec":  u.GetClientRecycleDelaySec(),
				"client_drain_sec":          u.GetClientDrainSec(),
				"group":                     u.GetGroup(),
			})
		}
		result[node] = list
	}
	c.JSON(200, result)
}

// serveNormal returns the caller's own profile via RPC GetProfile.
func (handler *UserListHandler) serveNormal(c *gin.Context) {
	serveUserProfile(c, handler.getHttpServer())
}

func (handler *UserListHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UserListHandler) getRelativePath() string { return "/user" }

func (handler *UserListHandler) help() string {
	return `GET /api/user
	获取用户信息。管理员返回全部用户，普通用户返回自己的数据。
	query: target`
}

// serveUserProfile is the shared logic for returning a user's own profile.
// Used by both UserListHandler.serveNormal (GET /user for normal users)
// and ProfileHandler (GET /profile for any role).
func serveUserProfile(c *gin.Context, s *HttpServer) {
	usernameVal, exists := c.Get(auth.ContextKeyUsername)
	if !exists {
		c.JSON(401, gin.H{"code": 401, "msg": "JWT required"})
		return
	}
	username, _ := usernameVal.(string)
	target := getTargetFromQuery(c)
	nodes := s.GetTargetNodes(target)
	if len(nodes) == 0 {
		c.JSON(404, gin.H{"code": 404, "msg": "no available node"})
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
	succList, _, err := rpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(),
		client.GetProfileReqType,
		&proto.GetProfileReq{Username: username},
		s.GetClusterToken(),
	)
	if err != nil {
		c.JSON(502, gin.H{"code": 502, "msg": "RPC failed: " + err.Error()})
		return
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
	c.JSON(200, result)
}

// ProfileHandler GET /profile — 获取当前登录用户的个人信息（不区分角色）
// 管理员和普通用户均走 GetProfile 逻辑，返回含 inbounds、traffic_limit 的完整 profile。
type ProfileHandler struct{ HttpHandlerImp }

func (handler *ProfileHandler) handlerFunc(c *gin.Context) {
	serveUserProfile(c, handler.getHttpServer())
}

func (handler *ProfileHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ProfileHandler) getRelativePath() string { return "/profile" }

func (handler *ProfileHandler) help() string {
	return `GET /api/profile
	获取当前登录用户的个人信息，不区分角色，始终返回完整 profile。
	query: target`
}
