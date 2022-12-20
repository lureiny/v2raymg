package http

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
)

type BoundHandler struct{ HttpHandlerImp }

func (handler *BoundHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}

	parasMap["type"] = c.DefaultQuery("type", "")
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["boundRawString"] = c.DefaultQuery("bound_raw_string", "")
	parasMap["srcTag"] = c.DefaultQuery("src_tag", "")
	parasMap["dstTag"] = c.DefaultQuery("dst_tag", "")
	parasMap["newPort"] = c.DefaultQuery("new_port", "")
	parasMap["isCopyUser"] = c.DefaultQuery("is_copy_user", "1") // 默认copy
	parasMap["dstProtocol"] = c.DefaultQuery("dst_protocol", "")

	return parasMap
}

func (handler *BoundHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	nodes := handler.getHttpServer().getTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)

	var err error = nil
	switch parasMap["type"] {
	case "addInbound":
		err = rpcClient.AddInbound(parasMap["boundRawString"])
	case "deleteInbound":
		err = rpcClient.DeleteInbound(parasMap["srcTag"])
	case "transferInbound":
		err = rpcClient.TransferInbound(parasMap["srcTag"], parasMap["newPort"])
	case "copyInbound":
		err = rpcClient.CopyInbound(
			parasMap["srcTag"],
			parasMap["dstTag"],
			parasMap["newPort"],
			parasMap["dstProtocol"],
			parasMap["isCopyUser"] == "1")
	case "copyUser":
		err = rpcClient.CopyUser(parasMap["srcTag"], parasMap["dstTag"])
	case "getInbound":
		inbounds := []string{}
		inbounds, err = rpcClient.GetInbound(parasMap["srcTag"])
		c.String(200, strings.Join(inbounds, "\n"))
	default:
		err = fmt.Errorf("unsupport operation type %s", parasMap["type"])
	}
	if err != nil {
		logger.Error(
			"Err=%s|OpType=%s|Target=%s",
			err.Error(),
			parasMap["type"],
			parasMap["target"],
		)
		c.String(200, err.Error())
		return
	}
}

func (handler *BoundHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *BoundHandler) help() string {
	usage := `/bound
	inbound操作接口, 支持添加, 删除, 迁移, 复制inbound, inbound间复制用户, 获取inbound
	通用参数列表:
	target: 目标node的名称
	token: 用于验证操作权限
	type: 操作类型, 可选值有addInbound, deleteInbound, transferInbound, copyInbound, copyUser, getInbound
	各个接口参数说明: 
	1. 添加inbound
	/bound?type=addInbound&boundRawString={boundRawString}&token={token}
	boundRawString, json中inbound配置base64编码后的字符串
	2. 删除inbound
	/bound?type=deleteInbound&src_tag={src_tag}&token={token}
	src_tag, 要删除inbound的tag
	3. 迁移inbound
	迁移inbound仅切换端口
	/bound?type=transferInbound&src_tag={src_tag}&new_port={new_port}&token={token}
	src_tag, 要迁移inbound的tag
	new_port, 新的端口
	4. 复制inbound
	/bound?type=copyInbound&src_tag={src_tag}&new_port={new_port}&dstTag={dst_tag}&dst_protocol={dst_protocol}&is_copy_user={is_copy_user}&token={token}
	src_tag, 被复制inbound的tag
	new_port, 新的端口
	dst_tag, 新inbound的tag
	dst_protocol, 新的协议类型, 仅支持vmess, vless, trojan
	is_copy_user, 是否同时复制用户, "is_copy_user == 1"时为复制, 默认复制
	5. inbound间复制用户
	/bound?type=copyUser&src_tag={src_tag}&dst_tag={dst_tag}&token={token}
	src_tag, 被复制inbound的tag
	dst_tag, 新的tag
	6. 获取inbound详细配置
	/bound?type=getInbound&src_tag={src_tag}&token={token}
	src_tag, 想要获取inbound的tag
	`
	return usage
}
