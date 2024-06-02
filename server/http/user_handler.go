package http

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	client "github.com/lureiny/v2raymg/client/rpc"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/common/util"
	globalCluster "github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type UserHandler struct{ HttpHandlerImp }

var userOpMap = map[string]string{
	"1": "AddUsers", "2": "UpdateUsers", "3": "DeleteUsers", "4": "ResetUser",
}

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

func (handler *UserHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	parasMap["user"] = c.Query("user")
	parasMap["pwd"] = c.Query("pwd")
	parasMap["type"] = c.DefaultQuery("type", "")
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["tags"] = c.DefaultQuery("tags", "")
	parasMap["ttl"] = c.DefaultQuery("ttl", "0")
	parasMap["expire"] = c.DefaultQuery("expire", "0")
	return parasMap
}

func (handler *UserHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)
	var err error = nil
	expire, err := getExpireTime(parasMap)

	if err != nil {
		errMsg := fmt.Sprintf("illegal expire time > %v", err)
		logger.Error(
			"Err=%s|User=%s|Passwd=%s|OpType=%s|Target=%s",
			errMsg,
			parasMap["user"],
			parasMap["pwd"],
			parasMap["type"],
			parasMap["target"],
		)
		c.String(200, errMsg)
		return
	}

	tagList := util.StringList{}
	tagList = strings.Split(parasMap["tags"], ",")
	userPoint := &proto.User{
		Name:       parasMap["user"],
		Passwd:     parasMap["pwd"],
		ExpireTime: expire,
		Tags:       tagList.Filter(func(t string) bool { return len(t) > 0 }),
	}

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, nil)

	if opName, ok := userOpMap[parasMap["type"]]; ok {
		req := &proto.UserOpReq{
			Users: []*proto.User{userPoint},
		}
		var reqType client.ReqToEndNodeType = -1
		switch opName {
		case "AddUsers":
			reqType = client.AddUsersReqType
		case "UpdateUsers":
			reqType = client.UpdateUsersReqType
		case "DeleteUsers":
			reqType = client.DeleteUsersReqType
		case "ResetUser":
			reqType = client.ResetUserReqType
		}
		_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), reqType, req, globalCluster.GetClusterToken())
		if len(failedList) != 0 {
			errMsg := joinFailedList(failedList)
			logger.Error(
				"Err=%s|User=%s|Passwd=%s|OpType=%s|Target=%s",
				errMsg,
				parasMap["user"],
				parasMap["pwd"],
				parasMap["type"],
				parasMap["target"],
			)
			c.String(200, errMsg)
			return
		}
	} else if parasMap["type"] == "5" {
		// GetUsers
		succList, _, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
			client.GetUsersReqType,
			&proto.GetUsersReq{},
			globalCluster.GetClusterToken(),
		)
		c.JSON(200, succList)
		return
	} else {
		err = fmt.Errorf("unsupport operation type %s", parasMap["type"])
		logger.Error(
			"Err=%s|User=%s|Passwd=%s|OpType=%s|Target=%s",
			err.Error(),
			parasMap["user"],
			parasMap["pwd"],
			parasMap["type"],
			parasMap["target"],
		)
		c.String(200, err.Error())
		return
	}
	c.String(200, "Succ")
}

func (handler *UserHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *UserHandler) getRelativePath() string {
	return "/user"
}

func (handler *UserHandler) help() string {
	usage := `/user
	user操作接口, 支持添加, 删除, 更新user信息, 重置用户proxy的密钥信息, 获取用户列表
	通用参数列表:
	target: 目标node的名称
	tags: 操作的inbound的tag, 使用","分隔
	type: 操作类型
	token: 用于验证操作权限
	各个接口参数说明:
	1. 添加用户
	/user?type=1&user={user}&pwd={pwd}&expire={expire}&target={target}&token={token}&ttl={ttl}&tags={tags}
	user: 用户名
	pwd: password
	expire: 过期时间, 过期时间的时间戳, 例如2022-11-27 12:00:00过期, 则expire=1669521600, 与下述ttl参数同时存在时, 优先使用ttl设置过期时间
	ttl: 存活时间, 从添加时开始的有效存活时间, 单位为秒, 例如1个小时内有效, ttl=3600
	tags: 添加inbound的tag列表, 以逗号分隔
	2. 更新用户信息
	/user?type=2&user={user}&pwd={pwd}&expire={expire}&target={target}&token={token}&ttl={ttl}
	user: 用户名
	pwd: password
	expire: 过期时间, 过期时间的时间戳, 例如2022-11-27 12:00:00过期, 则expire=1669521600, 与下述ttl参数同时存在时, 优先使用ttl设置过期时间
	ttl: 存活时间, 从添加时开始的有效存活时间, 单位为秒, 例如1个小时内有效, ttl=3600
	3. 删除用户
	/user?type=3&target={target}&user={user}&token={token}&tags={tags}
	user: 用户名
	4. 重置用户
	/user?target={target}&type=4&user={user}&token={token}
	user: 用户名
	5. 获取用户列表
	/user?type=5&target={target}&token={token}
	`
	return usage
}
