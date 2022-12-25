package rpc

import (
	context "context"
	"fmt"
	"math"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/proxy/manager"
	"github.com/lureiny/v2raymg/server"
	"github.com/lureiny/v2raymg/server/rpc/proto"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

var proxyManager = manager.GetProxyManager()

var RpcServerKey = []byte{}
var endNodeServer = &EndNodeServer{}

var localNode = common.GlobalLocalNode

type EndNodeServer struct {
	proto.UnimplementedEndNodeAccessServer
	clusterManager *common.EndNodeClusterManager
	userManager    *common.UserManager
	centerNode     *common.Node
	server.ServerConfig
}

const rpcServerKeyLen = 32

func GetEndNodeServer() *EndNodeServer {
	return endNodeServer
}

func (s *EndNodeServer) initRpcServerKey() {
	if len(s.clusterManager.Token) > rpcServerKeyLen {
		RpcServerKey = []byte(s.clusterManager.Token)[:32]
	} else {
		// 如果密码为空, 则同样不具有安全性, 仅仅不会被抓包直接分析
		RpcServerKey = common.PKCS7Padding([]byte(s.clusterManager.Token), rpcServerKeyLen)
	}
}

var methodPrefixLen = len("/proto.EndNodeAccess/")

// gateway模式下放行的接口列表
var onlyGatewayMethods = "HeartBeat|RegisterNode|SetGatewayModel"

func isOnlyGatewayMethod(fullMethod string) bool {
	return strings.Contains(onlyGatewayMethods, fullMethod[methodPrefixLen:])
}

var methodRspMap = map[string]interface{}{
	"GetUsers":             &proto.GetUsersRsp{},
	"AddUsers":             &proto.UserOpRsp{},
	"DeleteUsers":          &proto.UserOpRsp{},
	"UpdateUsers":          &proto.UserOpRsp{},
	"ResetUser":            &proto.UserOpRsp{},
	"GetSub":               &proto.GetSubRsp{},
	"GetBandWidthStats":    &proto.GetBandwidthStatsRsp{},
	"HeartBeat":            &proto.HeartBeatRsp{},
	"RegisterNode":         &proto.RegisterNodeRsp{},
	"AddInbound":           &proto.InboundOpRsp{},
	"DeleteInbound":        &proto.InboundOpRsp{},
	"TransferInbound":      &proto.InboundOpRsp{},
	"CopyInbound":          &proto.InboundOpRsp{},
	"CopyUser":             &proto.InboundOpRsp{},
	"GetInbound":           &proto.GetInboundRsp{},
	"GetTag":               &proto.GetTagRsp{},
	"UpdateProxy":          &proto.UpdateProxyRsp{},
	"AddAdaptiveConfig":    &proto.AdaptiveRsp{},
	"DeleteAdaptiveConfig": &proto.AdaptiveRsp{},
	"Adaptive":             &proto.AdaptiveRsp{},
	"SetGatewayModel":      &proto.SetGatewayModelRsp{},
}

func newEmptyRsp(fullMethod string) (interface{}, error) {
	return methodRspMap[fullMethod[methodPrefixLen:]], nil
}

func authRemoteNode(req interface{}, fullMethod string) (bool, interface{}, *proto.Node) {
	reqValue := reflect.ValueOf(req)
	nodeAuthInfo := reqValue.Elem().FieldByName("NodeAuthInfo").Elem().Interface().(proto.NodeAuthInfo)
	if fullMethod[methodPrefixLen:] == "RegisterNode" {
		return true, nil, nodeAuthInfo.Node
	}
	node := &common.Node{
		Node:    nodeAuthInfo.Node,
		InToken: nodeAuthInfo.Token,
	}
	if err := endNodeServer.clusterManager.AuthRemoteNode(&node); err != nil && localNode.Token != node.InToken {
		errMsg := fmt.Sprintf("auth err > %v", err)
		logger.Error(
			"Err=%s|Src=%s:%d|SrcName=%s|Api=%s",
			errMsg,
			node.Host,
			node.Port,
			node.Name,
			fullMethod[methodPrefixLen:],
		)
		rspValue := reflect.ValueOf(methodRspMap[fullMethod[methodPrefixLen:]])
		rspValue.Elem().FieldByName("Code").SetInt(400)
		rspValue.Elem().FieldByName("Msg").SetString(errMsg)
		return false, rspValue.Interface(), nodeAuthInfo.Node
	}
	return true, nil, nodeAuthInfo.Node
}

func UnaryServerInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, hander grpc.UnaryHandler) (interface{}, error) {
	// only gateway表示当前节点仅作为转发节点, 本身对外不提供代理服务
	if configManager.GetBool(common.ServerRpcOnlyGateway) &&
		!isOnlyGatewayMethod(info.FullMethod) {
		return newEmptyRsp(info.FullMethod)
	}
	authOK, rsp, node := authRemoteNode(req, info.FullMethod)
	if !authOK {
		return rsp, nil
	}
	startPoint := time.Now().UnixMilli()
	rsp, err := hander(ctx, req)
	logger.Info(
		"Src=%s:%d|SrcName=%s|Api=%s|Delay=%dms",
		node.Host,
		node.Port,
		node.Name,
		info.FullMethod[methodPrefixLen:],
		time.Now().UnixMilli()-startPoint,
	)

	return rsp, err
}

func (s *EndNodeServer) Init(um *common.UserManager, cm *common.EndNodeClusterManager) {
	s.clusterManager = cm
	s.userManager = um
	s.Host = configManager.GetString(common.ServerListen)
	s.Port = configManager.GetInt(common.ServerRpcPort)
	s.Type = "End"
	s.clusterManager.Name = configManager.GetString(common.ClusterName)
	serverName := configManager.GetString(common.ServerName)
	accessHost := configManager.GetString(common.ProxyHost)
	if serverName == "" {
		serverName = fmt.Sprintf("%s:%d", accessHost, s.Port)
	}
	s.Name = serverName
	localNode.Token = uuid.New().String()
	localNode.Node = proto.Node{
		Host:        configManager.GetString(common.ProxyHost),
		Port:        int32(s.Port),
		ClusterName: s.clusterManager.Name,
		Name:        s.Name,
	}
	// 添加本地节点
	cm.Add(&common.Node{
		InToken:             localNode.Token,
		OutToken:            localNode.Token,
		Node:                &localNode.Node,
		ReportHeartBeatTime: math.MaxInt64 - common.NodeTimeOut,
		GetHeartBeatTime:    math.MaxInt64 - common.NodeTimeOut,
		CreateTime:          time.Now().Unix(),
	})
	logger.Init()
	logger.SetLogLevel(0)
	logger.SetNodeType("End")
	logger.SetServerName(serverName)

	s.clusterManager.Token = configManager.GetString(common.ClusterToken)
	err := s.clusterManager.LoadStaticNode()
	s.initRpcServerKey()
	if err != nil {
		logger.Error("Err=Load Static Node > %v", err)
	}

	// init center node
	s.centerNode = &common.Node{
		Node: &proto.Node{
			Host: configManager.GetString(common.CenterNodeHost),
			Port: int32(configManager.GetInt(common.CenterNodePort)),
		},
	}

	err = proxyManager.StartProxyServer()
	if err != nil {
		logger.Error("Err=Start proxy server err > %v", err)
	}
}

func (s *EndNodeServer) RegisterNode(ctx context.Context, registerNodeReq *proto.RegisterNodeReq) (*proto.RegisterNodeRsp, error) {
	registerNodeRsp := &proto.RegisterNodeRsp{}
	clusterToken := registerNodeReq.GetNodeAuthInfo().GetToken()
	node := registerNodeReq.GetNodeAuthInfo().GetNode()
	// 这里没有做探活
	if node.Host == "" || node.Port <= 0 {
		errMsg := "empty host or invalid port"
		logger.Error(
			"Err=%s|Src=%s:%d",
			errMsg,
			node.Host,
			node.Port,
		)
		registerNodeRsp.Code = 100
		registerNodeRsp.Msg = errMsg
		return registerNodeRsp, nil
	}

	if err := s.clusterManager.IsSameCluster(node.GetClusterName(), clusterToken); err != nil {
		errMsg := err.Error()
		logger.Info(
			"Err=%s|Src=%s:%d|LocalCuster=%s|RegisteredCluster=%s|Token=%s",
			errMsg,
			node.Host,
			node.Port,
			localNode.ClusterName,
			node.GetClusterName(),
			clusterToken,
		)
		// 记录注册到本地失败的node列表
		s.clusterManager.AddToWrongNodeList(&common.Node{
			Node:       node,
			CreateTime: time.Now().Unix(),
		})
		registerNodeRsp.Msg = errMsg
		registerNodeRsp.Code = 101
		return registerNodeRsp, nil
	}

	nodeName := node.Name
	token := ""
	// 重新带有正确token验证后应该从wrong node list中清除
	s.clusterManager.DeleteFromWrongTokenNodeList(nodeName)

	if n, ok := s.clusterManager.NodeManager.HaveNode(nodeName); ok {
		// 已经感知到的节点
		if n.RegisteredLocal() {
			errMsg := "repeated register"
			logger.Error(
				"Err=%s|SrcName=%s",
				errMsg,
				nodeName,
			)
			// 102代表重复注册但是不代表失败, 当对端上报心跳失败导致重复注册时会用到
			registerNodeRsp.Code = 102
			registerNodeRsp.Msg = errMsg
			token = n.InToken
		} else {
			// 更新已经感知到但是第一次注册的节点
			token = uuid.New().String()
			n.InToken = token
			logger.Info(
				"Src=%s:%d|SrcName=%s|Cluster=%s|RegisterType=%s|Token=%s",
				node.Host,
				node.Port,
				nodeName,
				node.ClusterName,
				"Update",
				token,
			)
		}
		n.GetHeartBeatTime = time.Now().Unix()
	} else {
		// 新注册的节点
		token = uuid.New().String()
		newNode := &common.Node{
			Node:                node,
			InToken:             token,
			OutToken:            "",
			GetHeartBeatTime:    time.Now().Unix(),
			CreateTime:          time.Now().Unix(),
			ReportHeartBeatTime: 0,
		}
		s.clusterManager.Add(newNode)
		logger.Info(
			"Src=%s:%d|SrcName=%s|Cluster=%s|RegisterType=%s|Token=%s",
			node.Host,
			node.Port,
			nodeName,
			node.ClusterName,
			"New Register",
			token,
		)
	}
	registerNodeRsp.Data = []byte(token)
	return registerNodeRsp, nil
}

func (s *EndNodeServer) HeartBeat(ctx context.Context, heartBeatReq *proto.HeartBeatReq) (*proto.HeartBeatRsp, error) {
	heartBeatRsp := &proto.HeartBeatRsp{}
	heartBeatRsp.NodesMap = s.clusterManager.GetNodes(
		func(node *common.Node) bool {
			return node.Name != s.Name && node.IsValid()
		},
	)
	return heartBeatRsp, nil
}

func (s *EndNodeServer) GetUsers(ctx context.Context, getUsersReq *proto.GetUsersReq) (*proto.GetUsersRsp, error) {
	getUsersRsp := &proto.GetUsersRsp{
		Code: 0,
	}
	usersMap := s.userManager.GetUserList()
	for _, u := range usersMap {
		getUsersRsp.Users = append(getUsersRsp.Users, u)
	}
	return getUsersRsp, nil
}

func (s *EndNodeServer) AddUsers(ctx context.Context, addUsersReq *proto.UserOpReq) (*proto.UserOpRsp, error) {
	addUsersRsp := &proto.UserOpRsp{
		Code: 0,
	}
	userList := ""
	for _, user := range addUsersReq.GetUsers() {
		userList = userList + ";" + user.Name
		err := s.userManager.Add(user)
		if err != nil {
			logger.Error(
				"Err=%s|User=%s",
				err.Error(),
				user.Name,
			)
			addUsersRsp.Msg += fmt.Sprintf("user: %s add failed: %s|", user.Name, err.Error())
		}
	}
	if len(addUsersRsp.Msg) > 0 {
		addUsersRsp.Code = 200
	}

	return addUsersRsp, nil
}

func (s *EndNodeServer) DeleteUsers(ctx context.Context, deleteUsersReq *proto.UserOpReq) (*proto.UserOpRsp, error) {
	deleteUsersRsp := &proto.UserOpRsp{
		Code: 0,
	}

	userList := ""
	for _, user := range deleteUsersReq.GetUsers() {
		userList = userList + ";" + user.Name
		err := s.userManager.Delete(user)
		if err != nil {
			logger.Error(
				"Err=%s|User=%s",
				err.Error(),
				user.Name,
			)
			deleteUsersRsp.Msg += fmt.Sprintf("user: %s delete failed, %s\n", user.Name, err.Error())
		}
	}
	if len(deleteUsersRsp.Msg) > 0 {
		deleteUsersRsp.Code = 201
	}

	return deleteUsersRsp, nil
}

func (s *EndNodeServer) UpdateUsers(ctx context.Context, updateUsersReq *proto.UserOpReq) (*proto.UserOpRsp, error) {
	updateUsersRsp := &proto.UserOpRsp{
		Code: 0,
	}

	var err error = nil
	userList := ""
	for _, user := range updateUsersReq.GetUsers() {
		userList = userList + ";" + user.Name
		err = s.userManager.Update(user)
	}
	if err != nil {
		updateUsersRsp.Msg = err.Error()
	}

	return updateUsersRsp, nil
}

func (s *EndNodeServer) ResetUser(ctx context.Context, resetUserReq *proto.UserOpReq) (*proto.UserOpRsp, error) {
	resetUserRsp := &proto.UserOpRsp{
		Code: 0,
	}

	userList := ""
	for _, user := range resetUserReq.GetUsers() {
		userList = userList + ";" + user.Name
		err := s.userManager.Reset(user)
		if err != nil {
			logger.Error(
				"Err=%s|User=%s",
				err.Error(),
				user.Name,
			)
			resetUserRsp.Msg += fmt.Sprintf("user: %s reset failed, %s\n", user.Name, err.Error())
		}
	}
	if len(resetUserRsp.Msg) > 0 {
		resetUserRsp.Code = 202
	}

	return resetUserRsp, nil
}

func (s *EndNodeServer) GetSub(ctx context.Context, getSubReq *proto.GetSubReq) (*proto.GetSubRsp, error) {
	getSubRsp := &proto.GetSubRsp{
		Code: 0,
	}

	user := getSubReq.GetUser()
	// 判断用户是否存在/合法
	uris, err := s.userManager.GetUserSub(user)
	if err != nil || len(uris) == 0 {
		errMsg := fmt.Sprintf("get sub err > %v", err)
		logger.Error(
			"Err=%s|User=%s|Passwd=%s|URI=%s|Tags=%v",
			errMsg,
			user.Name,
			user.Passwd,
			user.Tags,
		)
		getSubRsp.Msg = errMsg
		getSubRsp.Code = 300
		return getSubRsp, nil
	}
	getSubRsp.Uris = uris

	return getSubRsp, nil
}

func (s *EndNodeServer) GetBandWidthStats(ctx context.Context, getBandwidthStatsReq *proto.GetBandwidthStatsReq) (*proto.GetBandwidthStatsRsp, error) {
	getBandWidthStatsRsp := &proto.GetBandwidthStatsRsp{
		Code: 0,
	}

	pattern := getBandwidthStatsReq.GetPattern()
	reset := getBandwidthStatsReq.GetReset_()

	stats, err := proxyManager.QueryStats(pattern, reset)

	if err != nil {
		errMsg := fmt.Sprintf("Get bandWidth stats err > %v", err)
		logger.Error(
			"Err=%s|Pattern=%s|Reset=%v",
			errMsg,
			pattern,
			reset,
		)
		getBandWidthStatsRsp.Msg = errMsg
		getBandWidthStatsRsp.Code = 500
		return getBandWidthStatsRsp, nil
	}
	for _, s := range *stats {
		getBandWidthStatsRsp.Stats = append(getBandWidthStatsRsp.Stats, s)
	}
	return getBandWidthStatsRsp, nil
}

func (s *EndNodeServer) AddInbound(ctx context.Context, inboundOpReq *proto.InboundOpReq) (*proto.InboundOpRsp, error) {
	inboundOpRsp := &proto.InboundOpRsp{
		Code: 0,
	}
	newInbound := manager.Inbound{}
	err := newInbound.Init(inboundOpReq.GetInboundInfo())
	if err != nil {
		errMsg := fmt.Sprintf("unmarshal inbound err > %v", err)
		logger.Error(
			"Err=%s|InboundInfo=%s",
			errMsg,
			inboundOpReq.GetInboundInfo(),
		)
		inboundOpRsp.Code = 600
		inboundOpRsp.Msg = errMsg
		return inboundOpRsp, nil
	}

	err = proxyManager.AddInbound(&newInbound)
	if err != nil {
		errMsg := fmt.Sprintf("add inbound err > %v", err)
		logger.Error(
			"Err=%s|InboundInfo=%s",
			errMsg,
			inboundOpReq.GetInboundInfo(),
		)
		inboundOpRsp.Msg = errMsg
		inboundOpRsp.Code = 601
		return inboundOpRsp, nil
	}
	// 如果本身不存在的user会被过滤掉
	for _, u := range newInbound.GetUsers() {
		if user := s.userManager.Get(u); user != nil {
			user.Tags = append(user.Tags, newInbound.Tag)
		}
	}
	return inboundOpRsp, nil
}

func (s *EndNodeServer) DeleteInbound(ctx context.Context, inboundOpReq *proto.InboundOpReq) (*proto.InboundOpRsp, error) {
	inboundOpRsp := &proto.InboundOpRsp{
		Code: 0,
	}
	tag := inboundOpReq.GetInboundInfo()

	dstInbound := proxyManager.GetInbound(tag)
	if dstInbound != nil {
		users := dstInbound.GetUsers()
		for _, user := range users {
			s.userManager.Delete(&proto.User{
				Name: user,
				Tags: []string{tag},
			})
		}
	}

	err := proxyManager.DeleteInbound(tag)
	if err != nil {
		errMsg := fmt.Sprintf("delete inbound err > %v", err)
		logger.Error(
			"Err=%s|InboundTag=%s",
			errMsg,
			inboundOpReq.GetInboundInfo(),
		)
		inboundOpRsp.Msg = errMsg
		inboundOpRsp.Code = 601
		return inboundOpRsp, nil
	}
	return inboundOpRsp, nil
}

func (s *EndNodeServer) TransferInbound(ctx context.Context, transferInboundReq *proto.TransferInboundReq) (*proto.InboundOpRsp, error) {
	inboundOpRsp := &proto.InboundOpRsp{
		Code: 0,
	}

	err := proxyManager.TransferInbound(transferInboundReq.GetTag(), uint32(transferInboundReq.GetNewPort()))
	if err != nil {
		errMsg := fmt.Sprintf("transfer inbound err > %v", err)
		logger.Error(
			"Err=%s|Tag=%s|NewPort=%d",
			errMsg,
			transferInboundReq.GetTag(),
			transferInboundReq.GetNewPort(),
		)
		inboundOpRsp.Msg = errMsg
		inboundOpRsp.Code = 700
		return inboundOpRsp, nil
	}
	return inboundOpRsp, nil
}

func (s *EndNodeServer) CopyInbound(ctx context.Context, copyInboundReq *proto.CopyInboundReq) (*proto.InboundOpRsp, error) {
	inboundOpRsp := &proto.InboundOpRsp{
		Code: 0,
	}

	err := proxyManager.CopyInbound(
		copyInboundReq.GetSrcTag(),
		copyInboundReq.GetNewTag(),
		copyInboundReq.GetNewProtocol(),
		int(copyInboundReq.GetNewPort()),
	)
	if err != nil {
		errMsg := fmt.Sprintf("copy inbound err > %v", err)
		logger.Error(
			"Err=%s|SrcTag=%s|NewTag=%s|NewPort=%d|IsCopyUser=%v",
			errMsg,
			copyInboundReq.GetSrcTag(),
			copyInboundReq.GetNewTag(),
			copyInboundReq.GetNewPort(),
			copyInboundReq.GetIsCopyUser(),
		)
		inboundOpRsp.Msg = errMsg
		inboundOpRsp.Code = 800
		return inboundOpRsp, nil
	}
	if copyInboundReq.GetIsCopyUser() {
		err := s.copyUser(copyInboundReq.GetSrcTag(), copyInboundReq.GetNewTag())
		if err != nil {
			errMsg := err.Error()
			logger.Error(
				"Err=%s|SrcTag=%s|NewTag=%s|NewPort=%d|IsCopyUser=%v",
				errMsg,
				copyInboundReq.GetSrcTag(),
				copyInboundReq.GetNewTag(),
				copyInboundReq.GetNewPort(),
				copyInboundReq.GetIsCopyUser(),
			)
			inboundOpRsp.Code = 801
			inboundOpRsp.Msg = errMsg
		}
	}
	return inboundOpRsp, nil
}

func (s *EndNodeServer) CopyUser(ctx context.Context, copyUserReq *proto.CopyUserReq) (*proto.InboundOpRsp, error) {
	inboundOpRsp := &proto.InboundOpRsp{
		Code: 0,
	}

	err := s.copyUser(copyUserReq.GetSrcTag(), copyUserReq.GetDstTag())
	if err != nil {
		errMsg := err.Error()
		logger.Error(
			"Err=%s|SrcTag=%s|DstTag=%s",
			errMsg,
			copyUserReq.GetSrcTag(),
			copyUserReq.GetDstTag(),
		)
		inboundOpRsp.Code = 801
		inboundOpRsp.Msg = errMsg
	}
	return inboundOpRsp, nil
}

func (s *EndNodeServer) copyUser(srcTag, dstTag string) error {
	if srcTag == dstTag {
		return nil
	}
	srcInbound := proxyManager.GetInbound(srcTag)
	if srcInbound == nil {
		return fmt.Errorf("inbound with src tag(%s) is not exist", srcTag)
	}

	dstInbound := proxyManager.GetInbound(dstTag)
	if dstInbound == nil {
		return fmt.Errorf("inbound with dst tag(%s) is not exist", dstTag)
	}
	users := srcInbound.GetUsers()
	succUser := []string{}
	failedUser := []string{}
	for _, user := range users {
		err := s.userManager.Add(&proto.User{
			Name: user,
			Tags: []string{dstTag},
		})
		if err != nil {
			logger.Error(
				"Err=%s",
				err.Error(),
			)
			failedUser = append(failedUser, user)
		} else {
			succUser = append(succUser, user)
		}
	}
	if len(failedUser) != 0 {
		return fmt.Errorf("succ list: [%s], failed list: [%s]", strings.Join(succUser, "|"), strings.Join(failedUser, "|"))
	}
	return nil
}

func (s *EndNodeServer) GetInbound(ctx context.Context, getInboundReq *proto.GetInboundReq) (*proto.GetInboundRsp, error) {
	getInboundRsp := &proto.GetInboundRsp{
		Code: 0,
	}

	inbound := proxyManager.GetInbound(getInboundReq.GetTag())
	if inbound == nil {
		errMsg := fmt.Sprintf("inbound with tag(%s) is not exist", getInboundReq.GetTag())
		logger.Error(
			"Err=%s|Tag=%s",
			errMsg,
			getInboundReq.GetTag(),
		)
		getInboundRsp.Code = 901
		getInboundRsp.Msg = errMsg
		return getInboundRsp, nil
	}
	inbound.RWMutex.RLock()
	defer inbound.RWMutex.RUnlock()
	data, err := inbound.Encode()
	if err != nil {
		errMsg := err.Error()
		logger.Error(
			"Err=%s|Tag=%s",
			errMsg,
			getInboundReq.GetTag(),
		)
		getInboundRsp.Code = 902
		getInboundRsp.Msg = errMsg
		return getInboundRsp, nil
	}
	getInboundRsp.Data = string(data)
	return getInboundRsp, nil
}

func (s *EndNodeServer) GetTag(ctx context.Context, getTagReq *proto.GetTagReq) (*proto.GetTagRsp, error) {
	getTagRsp := &proto.GetTagRsp{
		Code: 0,
	}
	getTagRsp.Tags = proxyManager.GetTags()
	return getTagRsp, nil
}

func (s *EndNodeServer) UpdateProxy(ctx context.Context, updateProxyReq *proto.UpdateProxyReq) (*proto.UpdateProxyRsp, error) {
	updateProxyRsp := &proto.UpdateProxyRsp{
		Code: 0,
	}
	tag := updateProxyReq.GetTag()
	if tag == proxyManager.GetProxyServerVersion() ||
		"v"+tag == proxyManager.GetProxyServerVersion() {
		updateProxyRsp.Msg = "current version is same with dst version"
		return updateProxyRsp, nil
	}
	// 下载对应版本
	err := proxyManager.UpdateProxyServer(updateProxyReq.GetTag())
	if err != nil {
		errMsg := err.Error()
		logger.Error(
			"Err=%s|Tag=%s",
			errMsg,
			updateProxyReq.GetTag(),
		)
		updateProxyRsp.Code = 1002
		updateProxyRsp.Msg = errMsg
		return updateProxyRsp, nil
	}
	newVersion := proxyManager.GetProxyServerVersion()
	logger.Info("CurrentProxyVersion=%s", newVersion)
	return updateProxyRsp, nil
}

func (s *EndNodeServer) AddAdaptiveConfig(ctx context.Context, adaptiveOpReq *proto.AdaptiveOpReq) (*proto.AdaptiveRsp, error) {
	adaptiveRsp := &proto.AdaptiveRsp{
		Code: 0,
	}
	errs := []string{}
	ports := adaptiveOpReq.GetPorts()
	tags := adaptiveOpReq.GetTags()
	for _, tag := range tags {
		if err := proxyManager.AddAdaptiveTag(tag); err != nil {
			errs = append(errs, fmt.Sprintf("add tag err > %v", err))
		}
	}
	for _, port := range ports {
		if err := proxyManager.AddAdaptivePort(port); err != nil {
			errs = append(errs, fmt.Sprintf("add tag err > %v", err))
		}
	}

	rawAdaptiveConfig := proxyManager.GetRawAdaptive()
	configManager.Set("proxy.adaptive", rawAdaptiveConfig)

	logger.Info(
		"Err=%s|Ports=%s|Tags=%s",
		strings.Join(errs, ","),
		strings.Join(ports, ","),
		strings.Join(tags, ","),
	)
	if len(errs) > 0 {
		adaptiveRsp.Code = 1010
		adaptiveRsp.Msg = strings.Join(errs, "\n")
	}
	return adaptiveRsp, nil
}

func (s *EndNodeServer) DeleteAdaptiveConfig(ctx context.Context, adaptiveOpReq *proto.AdaptiveOpReq) (*proto.AdaptiveRsp, error) {
	adaptiveRsp := &proto.AdaptiveRsp{
		Code: 0,
	}
	errs := []string{}
	ports := adaptiveOpReq.GetPorts()
	tags := adaptiveOpReq.GetTags()
	for _, tag := range tags {
		proxyManager.DeleteAdaptiveTag(tag)
	}
	for _, port := range ports {
		if p, err := strconv.ParseInt(port, 10, 64); err != nil {
			errs = append(errs, fmt.Sprintf("invalid port: %s", port))
			continue
		} else {
			proxyManager.DeleteAdaptivePort(p)
		}
	}
	rawAdaptiveConfig := proxyManager.GetRawAdaptive()
	configManager.Set("proxy.adaptive", rawAdaptiveConfig)

	logger.Info(
		"Err=%s|Ports=%s|Tags=%s",
		strings.Join(errs, ","),
		strings.Join(ports, ","),
		strings.Join(tags, ","),
	)
	if len(errs) > 0 {
		adaptiveRsp.Code = 1011
		adaptiveRsp.Msg = strings.Join(errs, "\n")
	}
	return adaptiveRsp, nil
}

func (s *EndNodeServer) Adaptive(ctx context.Context, adaptiveReq *proto.AdaptiveReq) (*proto.AdaptiveRsp, error) {
	adaptiveRsp := &proto.AdaptiveRsp{
		Code: 0,
	}
	errs := []string{}
	tags := adaptiveReq.GetTags()
	for _, tag := range tags {
		if oldPort, newPort, err := proxyManager.AdaptiveOneInbound(tag); err != nil {
			errs = append(errs, fmt.Sprintf("adaptive inbound with tag(%s) err > %v", tag, err))
			logger.Error(
				"Err=%s|Tag=%s|OldPort=%d|NewPort=%d",
				err.Error(),
				tag,
				oldPort,
				newPort,
			)
		} else {
			logger.Info(
				"Msg=adaptive inbound succ|Tag=%s|OldPort=%d|NewPort=%d",
				tag,
				oldPort,
				newPort,
			)
		}
	}
	if len(errs) > 0 {
		adaptiveRsp.Code = 1012
		adaptiveRsp.Msg = strings.Join(errs, "\n")
	}
	return adaptiveRsp, nil
}

func (s *EndNodeServer) SetGatewayModel(ctx context.Context, setGatewayModelReq *proto.SetGatewayModelReq) (*proto.SetGatewayModelRsp, error) {
	setGatewayModelRsp := &proto.SetGatewayModelRsp{
		Code: 0,
	}
	configManager.Set(common.ServerRpcOnlyGateway, setGatewayModelReq.GetEnableGatewayModel())
	return setGatewayModelRsp, nil
}

func (s *EndNodeServer) registerToEndNode(node *common.Node, wg *sync.WaitGroup, ch chan struct{}) {
	defer func() {
		wg.Done()
		<-ch
	}()
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		errMsg := fmt.Sprintf("did not connect > %v", err)
		logger.Error(
			"Err=%s|Dst=%s:%d|DstName=%s",
			errMsg,
			node.Host,
			node.Port,
			node.Name,
		)
		return
	}

	c := proto.NewEndNodeAccessClient(conn)
	registerNodeReq := &proto.RegisterNodeReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: s.clusterManager.Token,
			Node:  &localNode.Node,
		},
	}
	rsp, err := c.RegisterNode(context.Background(), registerNodeReq, grpc.ForceCodec(&EncryptMessageCodec{}))
	errMsg := ""
	if err != nil {
		errMsg = fmt.Sprintf("register to end node failed > %v", err)
	} else if rsp.GetCode() != 0 {
		errMsg = rsp.GetMsg()
	}
	if errMsg != "" {
		// 从处理的节点中清除, 添加到wrong token node list
		// TODO:  本地节点不会从遍历列表清除, 后面需要对node持久化时可以将此作为落盘依据之一
		// 102代表是已经注册过, 已经注册过的可以重新获取到token, 此时不需要删除, Error log仅做记录用
		if !node.IsLocal() && rsp.GetCode() != 102 {
			s.clusterManager.Delete(node.GetName())
			s.clusterManager.AddToWrongNodeList(node)
		}
		logger.Error(
			"Err=%s|Dst=%s:%d|DstName=%s",
			errMsg,
			node.Host,
			node.Port,
			node.Name,
		)
	}
	if len(rsp.GetData()) != 0 {
		token := string(rsp.GetData())
		node.OutToken = token
		node.ReportHeartBeatTime = time.Now().Unix()
	}
}

func (s *EndNodeServer) heartbeatToEndNode(node *common.Node, wg *sync.WaitGroup, ch chan struct{}) {
	defer func() {
		wg.Done()
		<-ch
	}()
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		errMsg := fmt.Sprintf("did not connect > %v", err)
		logger.Error(
			"Err=%s|Dst=%s:%d|DstName=%s",
			errMsg,
			node.Host,
			node.Port,
			node.Name,
		)
		return
	}

	c := proto.NewEndNodeAccessClient(conn)
	heartBeatReq := &proto.HeartBeatReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  &localNode.Node,
		},
	}
	rsp, err := c.HeartBeat(context.Background(), heartBeatReq, grpc.ForceCodec(&EncryptMessageCodec{}))
	if err != nil || rsp.GetCode() != 0 {
		errMsg := fmt.Sprintf("heartbeat to end node failed > %v", err)
		if rsp.GetCode() != 0 {
			errMsg = rsp.GetMsg()
			// 非网络问题应该更新上报时间, 延长节点有效时间
			node.ReportHeartBeatTime = time.Now().Unix()
		}
		// 将该节点置为未注册, 下次周期重新注册
		node.OutToken = ""
		logger.Error(
			"Err=%s|Dst=%s:%d|DstName=%s",
			errMsg,
			node.Host,
			node.Port,
			node.Name,
		)
	} else {
		node.ReportHeartBeatTime = time.Now().Unix()
		addRemoteNode(rsp, s, "End")
	}
}

// 向远端注册或者上报心跳
func (s *EndNodeServer) registerOrHeartBeatToEndNode() {
	// 并发数 10
	ch := make(chan struct{}, 10)
	wg := sync.WaitGroup{}
	for _, node := range s.clusterManager.NodeManager.GetNodes() {
		if node.Name == s.Name {
			continue
		}
		//网络波动导致节点间断连的场景下, 若本地节点不继续探测则会导致网络恢复后无法重现建立链接
		// 因此对于本地节点, 无论成功与否都更新上报时间, 确保节点始终有效, 从而会始终尝试探测与心跳上报
		if node.IsLocal() {
			node.ReportHeartBeatTime = time.Now().Unix()
		}
		// 无效节点, 等待被清理, 本地节点永远不会被清理
		if !node.IsValid() {
			msg := "Skip heartbeat to invalid node"
			logger.Info(
				"Msg=%s|Dst=%s:%d|DstName=%s",
				msg,
				node.Host,
				node.Port,
				node.Name,
			)
			return
		}

		ch <- struct{}{}
		wg.Add(1)
		if !node.RegisteredRemote() {
			go s.registerToEndNode(node, &wg, ch)
		} else {
			go s.heartbeatToEndNode(node, &wg, ch)
		}
	}
	wg.Wait()
}

func (s *EndNodeServer) heartbeatToCenterNode() {
	// 发送心跳到center node
	if s.centerNode.Host == "" || s.centerNode.Port <= 1000 {
		return
	}
	conn, err := s.centerNode.GetGrpcClientConn()
	if err != nil {
		errMsg := fmt.Sprintf("did not connect > %v", err)
		logger.Error(
			"Err=%s|Center=%s",
			errMsg,
			s.centerNode.Host,
		)
		return
	}
	c := proto.NewCenterNodeAccessClient(conn)
	heartBeatReq := &proto.HeartBeatReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: "",
			Node:  &localNode.Node,
		},
	}
	rsp, err := c.HeartBeat(context.Background(), heartBeatReq)
	if err != nil {
		errMsg := fmt.Sprintf("heartbeat failed > %v", err)
		logger.Error(
			"Err=%s|Center=%s:%d",
			errMsg,
			s.centerNode.Host,
		)
	} else {
		addRemoteNode(rsp, s, "Center")
	}
}

func addRemoteNode(rsp *proto.HeartBeatRsp, s *EndNodeServer, remoteServerType string) {
	// 添加本地不存在的node
	for key, remoteNode := range rsp.NodesMap {
		remoteNodeName := remoteNode.GetName()
		if node := s.clusterManager.Get(key); node == nil && remoteNode.Name != localNode.Name {
			if wrongNode := s.clusterManager.GetNodeFromWrongNodeList(remoteNodeName); wrongNode != nil {
				continue
			}
			logger.Info(
				"Msg=Add Node From %s Node|Node=%s:%d|NodeName=%s",
				remoteServerType,
				remoteNode.GetHost(),
				remoteNode.GetPort(),
				remoteNode.GetName(),
			)
			s.clusterManager.Add(
				&common.Node{
					Node:                remoteNode,
					InToken:             "",
					OutToken:            "",
					GetHeartBeatTime:    0,
					ReportHeartBeatTime: 0,
					CreateTime:          time.Now().Unix(),
				},
			)
		}
	}
}

func (s *EndNodeServer) heartBeatAndRegisterToNodeOrCenterNode() {
	// 5s上报一次
	ticker := time.NewTicker(time.Second * 5)
	for {
		s.heartbeatToCenterNode()
		s.registerOrHeartBeatToEndNode()
		<-ticker.C
	}
}

// 过滤无效节点和无效用户
func (s *EndNodeServer) filter() {
	// 10s 过滤一次
	clearCycle := 10 * time.Second
	timeTicker := time.NewTicker(clearCycle)
	for {
		<-timeTicker.C
		logger.Info("Msg=fliter invalid node and expire user")
		// 过滤掉无效节点, 保留本地节点
		s.clusterManager.NodeManager.Filter(func(n *common.Node) bool {
			return n.IsValid() || n.IsLocal()
		})
		// 过滤无效用户
		s.userManager.ClearInvalideUser()
	}
}

func isAddrValid(host string, port int) bool {
	return host != "" && port >= 1000
}

func (s *EndNodeServer) Start() {
	if !isAddrValid(s.Host, s.Port) {
		return
	}
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.Host, s.Port))
	if err != nil {
		errMsg := fmt.Sprintf("failed to listen > %v", err)
		logger.Error(
			"Err=%s|Addr=%s:%d",
			errMsg,
			s.Host,
			s.Port,
		)
		return
	}
	encoding.RegisterCodec(&EncryptMessageCodec{})
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(UnaryServerInterceptor))
	proto.RegisterEndNodeAccessServer(grpcServer, s)
	go s.heartBeatAndRegisterToNodeOrCenterNode()
	go s.filter()
	logger.Info("Msg=server listening at %v", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		errMsg := fmt.Sprintf("failed to serve > %v", err)
		logger.Error(
			"Err=%s|Addr=%d:%s",
			errMsg,
			s.Host,
			s.Port,
		)
		return
	}
}
