package http

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/proxy/sub"
	"github.com/lureiny/v2raymg/server"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

var configManager = common.GetGlobalConfigManager()
var localNode = common.GlobalLocalNode

type HttpServer struct {
	RestfulServer *gin.Engine
	server.ServerConfig
	userManager    *common.UserManager
	clusterManager *common.EndNodeClusterManager
	token          string // for admin op such as user op, stat op
}

var logger = common.LoggerImp

func (s *HttpServer) SetUserManager(um *common.UserManager) {
	s.userManager = um
}

func (s *HttpServer) Init(um *common.UserManager, cm *common.EndNodeClusterManager) {
	s.userManager = um
	s.clusterManager = cm
	gin.SetMode(gin.ReleaseMode)
	s.RestfulServer = gin.Default()

	s.Host = configManager.GetString("server.listen")
	s.Port = configManager.GetInt("server.http.port")
	s.token = configManager.GetString("server.http.token")
	s.Name = configManager.GetString("server.name")

	s.RestfulServer.GET("/sub", s.sub)
	s.RestfulServer.GET("/user", s.authWithToken, s.userOperate)
	s.RestfulServer.GET("/stat", s.authWithToken, s.stat)
	s.RestfulServer.GET("/bound", s.authWithToken, s.boundOperate)
	s.RestfulServer.GET("/nodeList", s.authWithToken, s.getNodeList)
	s.RestfulServer.GET("/tag", s.authWithToken, s.getTag)
	s.RestfulServer.GET("/updateProxy", s.authWithToken, s.updateProxy)
	s.RestfulServer.GET("/adaptiveOp", s.authWithToken, s.adaptiveOp)
	s.RestfulServer.GET("/adaptive", s.authWithToken, s.adaptive)
}

func (s *HttpServer) authWithToken(c *gin.Context) {
	token := c.DefaultQuery("token", "")
	if token != s.token {
		logger.Error("Err=invalid token|HttpPath=%s", c.FullPath())
		c.String(401, "invalide token")
		c.Abort()
		return
	}
	c.Next()
}

func (s *HttpServer) SetName(name string) {
	s.Name = name
}

func (s *HttpServer) Start() {
	logger.Info(
		"Msg=http server start, listen at %s:%d",
		s.Host,
		s.Port,
	)
	s.RestfulServer.Run(fmt.Sprintf("%s:%d", s.Host, s.Port))
}

// 解析请求参数
func (s *HttpServer) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	// 同一参数的重复解析是为了增加可读性, 清晰了解不同uri需要的请求参数,
	// 对于具体接口不做区分，
	// user
	parasMap["user"] = c.Query("user")
	parasMap["pwd"] = c.Query("pwd")
	parasMap["type"] = c.DefaultQuery("type", "")
	parasMap["target"] = c.DefaultQuery("target", s.Name)
	parasMap["tags"] = c.DefaultQuery("tags", "")

	// sub  这里需要变更下token的问题
	parasMap["user"] = c.Query("user")
	parasMap["pwd"] = c.Query("pwd")
	parasMap["tags"] = c.DefaultQuery("tags", "") // 按照","分割
	parasMap["target"] = c.DefaultQuery("target", s.Name)

	// stat
	parasMap["reset"] = c.DefaultQuery("reset", "0")
	parasMap["target"] = c.DefaultQuery("target", s.Name)
	parasMap["pattern"] = c.DefaultQuery("pattern", "") // 查询字符

	// bound
	parasMap["type"] = c.DefaultQuery("type", "")
	parasMap["target"] = c.DefaultQuery("target", s.Name)
	parasMap["boundRawString"] = c.DefaultQuery("bound_raw_string", "")
	parasMap["srcTag"] = c.DefaultQuery("src_tag", "")
	parasMap["newTag"] = c.DefaultQuery("new_tag", "")
	parasMap["newPort"] = c.DefaultQuery("new_port", "")
	parasMap["isCopyUser"] = c.DefaultQuery("is_copy_user", "1") // 默认copy
	parasMap["newProtocol"] = c.DefaultQuery("new_protocol", "")

	// update proxy server
	parasMap["versionTag"] = c.DefaultQuery("version_tag", "latest")

	// transfer user in node
	parasMap["srcTarget"] = c.DefaultQuery("src_target", s.Name)
	parasMap["dstTarget"] = c.DefaultQuery("dst_target", s.Name)
	parasMap["tranfserTag"] = c.DefaultQuery("transfer_tag", "1") // 默认迁移

	// adaptive op
	parasMap["ports"] = c.DefaultQuery("ports", "")
	parasMap["tags"] = c.DefaultQuery("tags", "")
	parasMap["type"] = c.DefaultQuery("type", "")

	return parasMap
}

// 根据target查找路由的节点
func (s *HttpServer) getTargetNodes(target string) *[]*common.Node {
	if target == "all" {
		filter := func(n *common.Node) bool {
			return n.IsValid()
		}
		nodes := s.clusterManager.RemoteNode.GetNodesWithFilter(filter)
		*nodes = append(*nodes, &common.Node{
			InToken:  localNode.Token,
			OutToken: localNode.Token,
			Node: &proto.Node{
				Name: s.Name,
				Host: "127.0.0.1",
				Port: int32(configManager.GetInt("server.rpc.port")),
			},
			ReportHeartBeatTime: time.Now().Unix(),
		})
		return nodes
	} else if target == s.Name {
		// 本地节点
		return &[]*common.Node{
			{
				InToken:  localNode.Token,
				OutToken: localNode.Token,
				Node: &proto.Node{
					Name: s.Name,
					Host: "127.0.0.1",
					Port: int32(configManager.GetInt("server.rpc.port")),
				},
				ReportHeartBeatTime: time.Now().Unix(),
			},
		}
	} else {
		filter := func(n *common.Node) bool {
			return n.IsValid() && n.Name == target
		}
		return s.clusterManager.RemoteNode.GetNodesWithFilter(filter)
	}
}

var userOpMap = map[string]string{
	// ListUser暂时不支持转发
	"1": "AddUsers", "2": "UpdateUsers", "3": "DeleteUsers", "4": "ResetUser",
}

func (s *HttpServer) userOperate(c *gin.Context) {
	parasMap := s.parseParam(c)
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

	nodes := s.getTargetNodes(parasMap["target"])
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

func (s *HttpServer) stat(c *gin.Context) {
	parasMap := s.parseParam(c)

	nodes := s.getTargetNodes(parasMap["target"])
	if nodes == nil {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	statsMap, err := rpcClient.GetBandWidthStats(parasMap["pattern"], parasMap["reset"] == "1")

	if err != nil {
		logger.Info(
			"Err=%s",
			err.Error(),
		)
		c.String(200, err.Error())
		return
	}
	var jsonDatas = gin.H{}
	for key, s := range *statsMap {
		jsonDatas[key] = s
	}
	c.JSON(200, jsonDatas)
}

func (s *HttpServer) sub(c *gin.Context) {
	parasMap := s.parseParam(c)
	userAgent := c.GetHeader("User-Agent")

	tagList := common.StringList{}
	tagList = strings.Split(parasMap["tags"], ",")
	// 需要根据target做路由
	userPoint := &proto.User{
		Name:   parasMap["user"],
		Passwd: parasMap["pwd"],
		Tags:   tagList.Filter(func(t string) bool { return len(t) > 0 }),
	}

	if !common.IsUserComplete(userPoint, true) {
		logger.Error(
			"Err=%s|User=%s|Passwd=%s|Target=%s",
			"invalid user",
			parasMap["user"],
			parasMap["pwd"],
			parasMap["target"],
		)
		c.String(200, "invalid user")
		return
	}

	nodes := s.getTargetNodes(parasMap["target"])
	if nodes == nil {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	uris, err := rpcClient.GetUsersSub(userPoint)

	if err != nil {
		logger.Error(
			"Err=%s|User=%s|Passwd=%s|Target=%s",
			err.Error(),
			parasMap["user"],
			parasMap["pwd"],
			parasMap["target"],
		)
	}

	uri := sub.TransferSubUri(uris, userAgent)
	c.String(200, uri)
}

func (s *HttpServer) boundOperate(c *gin.Context) {
	parasMap := s.parseParam(c)

	nodes := s.getTargetNodes(parasMap["target"])
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
			parasMap["newTag"],
			parasMap["newPort"],
			parasMap["newProtocol"],
			parasMap["isCopyUser"] == "1")
	case "copyUser":
		err = rpcClient.CopyUser(parasMap["srcTag"], parasMap["newTag"])
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
	c.String(200, "Succ")
}

func (s *HttpServer) getNodeList(c *gin.Context) {
	nodeList := s.clusterManager.GetNodeNameList()
	nodeList = append(nodeList, localNode.Name)
	c.JSON(200, nodeList)
}

func (s *HttpServer) getTag(c *gin.Context) {
	parasMap := s.parseParam(c)

	nodes := s.getTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	remoteTags, err := rpcClient.GetTag()
	if err != nil {
		logger.Error("Err=%s|Target=%s", err.Error(), parasMap["target"])
	}
	c.JSON(200, remoteTags)
}

func (s *HttpServer) updateProxy(c *gin.Context) {
	parasMap := s.parseParam(c)

	nodes := s.getTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)

	if err := rpcClient.UpdateProxy(parasMap["versionTag"]); err != nil {
		logger.Error(
			"Err=%s|Target=%s|Tag=%s",
			err.Error(),
			parasMap["target"],
			parasMap["versionTag"],
		)
		c.String(200, err.Error())
		return
	}
	c.String(200, "Succ")
}

func (s *HttpServer) transferUserInNode(c *gin.Context) {
	parasMap := s.parseParam(c)

	if parasMap["srcTarget"] == parasMap["dstTarget"] {
		c.String(200, fmt.Sprintf("src target(%s) is same with dst target(%s)", parasMap["srcTarget"], parasMap["dstTarget"]))
		return
	}

	srcNodes := s.getTargetNodes(parasMap["srcTarget"])
	if len(*srcNodes) == 0 {
		c.String(200, "no avaliable src node")
		return
	}

	dstNodes := s.getTargetNodes(parasMap["dstTarget"])
	if len(*dstNodes) == 0 {
		c.String(200, "no avaliable dst node")
		return
	}

	srcNodeRpcClient := client.NewEndNodeClient(srcNodes, localNode)
	dstNodeRpcClient := client.NewEndNodeClient(dstNodes, localNode)

	usersMap, err := srcNodeRpcClient.GetUsers()
	if err != nil {
		errMsg := fmt.Sprintf("get src target user list err > %v", err)
		logger.Error(
			"Err=get %s|Target=%s|Tag=%s",
			errMsg,
			parasMap["target"],
			parasMap["versionTag"],
		)
		c.String(200, errMsg)
		return
	}

	users := usersMap[parasMap["srcTarget"]]
	errMsgs := []string{}
	for _, u := range users {
		if parasMap["tranfserTag"] != "1" {
			// 不同步迁移tag, 迁移到目标节点默认的inbound上
			u.Tags = []string{}
		}
		err := dstNodeRpcClient.UserOp(u, "AddUsers")
		if err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("user: %s transfer err > %v", u.Name, err))
		}
	}

	if len(errMsgs) > 0 {
		c.String(200, strings.Join(errMsgs, "|"))
		return
	}
	c.String(200, "Succ")
}

func (s *HttpServer) adaptiveOp(c *gin.Context) {
	parasMap := s.parseParam(c)

	nodes := s.getTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	tagList := common.StringList{}
	tagList = strings.Split(parasMap["tags"], ",")
	portList := common.StringList{}
	portList = strings.Split(parasMap["ports"], ",")

	opType := ""
	switch strings.ToLower(parasMap["type"]) {
	case "del":
		opType = client.DeleteAdaptiveOpType
	default:
		opType = client.AddAdaptiveOpType
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	err := rpcClient.AdaptiveOp(
		portList.Filter(func(p string) bool { return len(p) > 0 }),
		tagList.Filter(func(t string) bool { return len(t) > 0 }),
		opType,
	)
	if err != nil {
		logger.Error(
			"Err=%s|Tags=%s|Ports=%s|Type=%s",
			err.Error(),
			strings.Join(tagList, ","),
			strings.Join(portList, ","),
			parasMap["type"],
		)
		c.String(200, err.Error())
		return
	}
	c.String(200, "Succ")
}

func (s *HttpServer) adaptive(c *gin.Context) {
	parasMap := s.parseParam(c)

	nodes := s.getTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	tagList := common.StringList{}
	tagList = strings.Split(parasMap["tags"], ",")

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	err := rpcClient.Adaptive(tagList.Filter(func(t string) bool { return len(t) > 0 }))
	if err != nil {
		logger.Error(
			"Err=%s|Tags=%s",
			err.Error(),
			strings.Join(tagList, ","),
		)
		c.String(200, err.Error())
		return
	}
	c.String(200, "Succ")
}
