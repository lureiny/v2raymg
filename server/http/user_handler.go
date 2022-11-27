package http

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type UserHandler struct{ HttpHandlerImp }

var userOpMap = map[string]string{
	// ListUser暂时不支持转发
	"1": "AddUsers", "2": "UpdateUsers", "3": "DeleteUsers", "4": "ResetUser",
}

func (handler *UserHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	parasMap["user"] = c.Query("user")
	parasMap["pwd"] = c.Query("pwd")
	parasMap["type"] = c.DefaultQuery("type", "")
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["tags"] = c.DefaultQuery("tags", "")
	return parasMap
}

func (handler *UserHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)
	var err error = nil
	expire, err := strconv.ParseInt(c.DefaultQuery("expire", "0"), 10, 64)

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
		c.String(200, "illegal expire time")
		return
	}

	tagList := common.StringList{}
	tagList = strings.Split(parasMap["tags"], ",")
	userPoint := &proto.User{
		Name:       parasMap["user"],
		Passwd:     parasMap["pwd"],
		ExpireTime: expire,
		Tags:       tagList.Filter(func(t string) bool { return len(t) > 0 }),
	}

	nodes := handler.getHttpServer().getTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	// 根据target做路由
	if opName, ok := userOpMap[parasMap["type"]]; ok {
		// 此种场景结果仅作记录, 返回给用户的结果以本地添加的为主
		rpcClient := client.NewEndNodeClient(nodes, localNode)
		err := rpcClient.UserOp(userPoint, opName)
		if err != nil {
			c.String(200, err.Error())
		}
		return
	} else if parasMap["type"] == "5" {
		rpcClient := client.NewEndNodeClient(nodes, localNode)
		usersMap, _ := rpcClient.GetUsers()
		users := map[string][]string{}
		for targetUsers, userList := range usersMap {
			users[targetUsers] = []string{}
			for _, u := range userList {
				users[targetUsers] = append(users[targetUsers], u.Name)
			}
		}
		c.JSON(200, users)
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
	}
	c.String(200, "Succ")
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
	/user?type=1&user={user}&pwd={pwd}&expire={expire}&target={target}&token={token}
	user: 用户名
	pwd: password
	expire: 过期时间, 过期时间的时间戳, 例如2022-11-27 12:00:00过期, 则expire=1669521600
	2. 更新用户信息
	/user?type=2&user={user}&pwd={pwd}&expire={expire}&target={target}&token={token}
	user: 用户名
	pwd: password
	expire: 过期时间, 过期时间的时间戳, 例如2022-11-27 12:00:00过期, 则expire=1669521600
	3. 删除用户
	/user?type=3&target={target}&user={user}&token={token}
	user: 用户名
	4. 重置用户
	/user?target={target}&type=4&user={user}&token={token}
	user: 用户名
	5. 获取用户列表
	/user?type=5&target={target}&token={token}
	`
	return usage
}