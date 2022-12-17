package client

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/server/rpc"
	"github.com/lureiny/v2raymg/server/rpc/proto"
	"google.golang.org/grpc"
)

var logger = common.LoggerImp

const MaxConcurrencyClientNum = 32

var ops = map[string]bool{
	"AddUsers": true, "DeleteUsers": true, "UpdateUsers": true, "ResetUser": true,
}

// 控制全局订阅拉取并发数量
var ch = make(chan struct{}, MaxConcurrencyClientNum)

type EndNodeClient struct {
	nodes     *[]*common.Node
	localNode *common.LocalNode
}

func NewEndNodeClient(nodes *[]*common.Node, localNode *common.LocalNode) *EndNodeClient {
	if nodes == nil || localNode == nil {
		return nil
	}
	endNodeClient := &EndNodeClient{}
	endNodeClient.nodes = nodes
	endNodeClient.localNode = localNode
	return endNodeClient
}

func processReqResult(rsp *proto.UserOpRsp, err error) error {
	if rsp.GetCode() != 0 {
		return fmt.Errorf(rsp.GetMsg())
	}
	return err
}

func reqUserOpToOneNode(node *common.Node, user *proto.User, localNode *proto.Node, opType string) error {
	if !node.RegisteredRemote() {
		// 没有注册过的节点不算作失败
		return nil
	}
	if _, ok := ops[opType]; !ok {
		return fmt.Errorf("unsupport Optype=%s", opType)
	}

	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return err
	}
	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.UserOpReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		Users: []*proto.User{
			user,
		},
	}

	value := reflect.ValueOf(endNodeAccessClient)
	f := value.MethodByName(opType)
	paramList := []reflect.Value{reflect.ValueOf(context.Background()), reflect.ValueOf(req), reflect.ValueOf(grpc.ForceCodec(&rpc.EncryptMessageCodec{}))}
	results := f.Call(paramList)
	var rsp *proto.UserOpRsp = (*proto.UserOpRsp)(results[0].UnsafePointer())
	if results[1].IsNil() {
		err = nil
	} else {
		err = results[1].Interface().(error)
	}
	return processReqResult(rsp, err)
}

func (c *EndNodeClient) UserOp(user *proto.User, opType string) error {
	failedNodes := []*common.Node{}
	globalErrMsg := ""
	lock := sync.Mutex{}
	wg := &sync.WaitGroup{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			err := reqUserOpToOneNode(n, user, &c.localNode.Node, opType)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				globalErrMsg = fmt.Sprintf("%s|%s", globalErrMsg, err.Error())
				lock.Unlock()
				failedNodes = append(failedNodes, n)
			}
			wg.Done()
		}(node)
	}
	wg.Wait()
	if len(failedNodes) == 0 {
		return nil
	}
	return fmt.Errorf(globalErrMsg)
}

func reqGetUserSub(node *common.Node, user *proto.User, localNode *proto.Node) ([]string, error) {
	if !node.RegisteredRemote() {
		return nil, nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return nil, err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.GetSubReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		User: user,
	}

	rsp, err := endNodeAccessClient.GetSub(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if err != nil {
		return nil, err
	}
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return rsp.GetUris(), nil
}

func (c *EndNodeClient) GetUsersSub(user *proto.User) ([]string, error) {
	wg := &sync.WaitGroup{}
	globalErrMsg := ""
	lock := sync.Mutex{}
	allUris := []string{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			uris, err := reqGetUserSub(n, user, &c.localNode.Node)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				globalErrMsg = fmt.Sprintf("%s|%s", globalErrMsg, err.Error())
				lock.Unlock()
			} else if len(uris) != 0 {
				lock.Lock()
				allUris = append(allUris, uris...)
				lock.Unlock()
			}
		}(node)
	}
	wg.Wait()
	if len(allUris) == 0 {
		return nil, fmt.Errorf(globalErrMsg)
	}
	return allUris, nil
}

func reqGetBandwidthStats(node *common.Node, localNode *proto.Node, pattern string, reset bool) ([]*proto.Stats, error) {
	if !node.RegisteredRemote() {
		return []*proto.Stats{}, nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return []*proto.Stats{}, err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.GetBandwidthStatsReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		Pattern: pattern,
		Reset_:  reset,
	}

	rsp, err := endNodeAccessClient.GetBandWidthStats(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if err != nil {
		return []*proto.Stats{}, err
	}
	if rsp.GetCode() != 0 {
		return []*proto.Stats{}, fmt.Errorf(rsp.GetMsg())
	}
	return rsp.GetStats(), nil
}

func (c *EndNodeClient) GetBandWidthStats(pattern string, reset bool) (*map[string][]*proto.Stats, error) {
	wg := &sync.WaitGroup{}
	globalErrMsg := ""
	lock := sync.Mutex{}
	allNodeStats := map[string][]*proto.Stats{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			remoteNodeStats, err := reqGetBandwidthStats(n, &c.localNode.Node, pattern, reset)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				globalErrMsg = fmt.Sprintf("%s|%s", globalErrMsg, err.Error())
				lock.Unlock()
				return
			}
			lock.Lock()
			allNodeStats[n.Name] = remoteNodeStats
			lock.Unlock()

		}(node)
	}
	wg.Wait()
	var err error = nil
	if globalErrMsg != "" {
		err = fmt.Errorf(globalErrMsg)
	}
	return &allNodeStats, err
}

func reqAddInbound(node *common.Node, localNode *proto.Node, inboundRawString string) error {
	if !node.RegisteredRemote() {
		return nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.InboundOpReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		InboundInfo: inboundRawString,
	}

	rsp, err := endNodeAccessClient.AddInbound(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return fmt.Errorf(rsp.GetMsg())
	}
	return err
}

func (c *EndNodeClient) AddInbound(inboundRawString string) error {
	wg := &sync.WaitGroup{}
	globalErrMsg := ""
	lock := sync.Mutex{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			err := reqAddInbound(n, &c.localNode.Node, inboundRawString)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				globalErrMsg = fmt.Sprintf("%s|%s", globalErrMsg, err.Error())
				lock.Unlock()
			}
		}(node)
	}
	wg.Wait()
	var err error = nil
	if globalErrMsg != "" {
		err = fmt.Errorf(globalErrMsg)
	}
	return err
}

func reqDeleteInbound(node *common.Node, localNode *proto.Node, tag string) error {
	if !node.RegisteredRemote() {
		return nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.InboundOpReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		InboundInfo: tag,
	}

	rsp, err := endNodeAccessClient.DeleteInbound(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return fmt.Errorf(rsp.GetMsg())
	}
	return err
}

func (c *EndNodeClient) DeleteInbound(tag string) error {
	wg := &sync.WaitGroup{}
	globalErrMsg := ""
	lock := sync.Mutex{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			err := reqDeleteInbound(n, &c.localNode.Node, tag)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				globalErrMsg = fmt.Sprintf("%s|%s", globalErrMsg, err.Error())
				lock.Unlock()
			}
		}(node)
	}
	wg.Wait()
	var err error = nil
	if globalErrMsg != "" {
		err = fmt.Errorf(globalErrMsg)
	}
	return err
}

func reqTransferInbound(node *common.Node, localNode *proto.Node, tag string, newPort int32) error {
	if !node.RegisteredRemote() {
		return nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.TransferInboundReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		Tag:     tag,
		NewPort: newPort,
	}

	rsp, err := endNodeAccessClient.TransferInbound(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return fmt.Errorf(rsp.GetMsg())
	}
	return err
}

func (c *EndNodeClient) TransferInbound(tag, newPort string) error {
	port, err := strconv.ParseInt(newPort, 10, 64)
	if err != nil {
		return err
	}
	wg := &sync.WaitGroup{}
	globalErrMsg := ""
	lock := sync.Mutex{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			err := reqTransferInbound(n, &c.localNode.Node, tag, int32(port))
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				globalErrMsg = fmt.Sprintf("%s|%s", globalErrMsg, err.Error())
				lock.Unlock()
			}
		}(node)
	}
	wg.Wait()
	if globalErrMsg != "" {
		err = fmt.Errorf(globalErrMsg)
	}
	return err
}

func reqCopyInbound(node *common.Node, localNode *proto.Node, srcTag, newTag, newProtocol string, newPort int32, isCopyUser bool) error {
	if !node.RegisteredRemote() {
		return nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.CopyInboundReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		SrcTag:      srcTag,
		NewTag:      newTag,
		NewPort:     newPort,
		IsCopyUser:  isCopyUser,
		NewProtocol: newProtocol,
	}

	rsp, err := endNodeAccessClient.CopyInbound(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return fmt.Errorf(rsp.GetMsg())
	}
	return err
}

func (c *EndNodeClient) CopyInbound(srcTag, newTag, newPort, newProtocol string, isCopyUser bool) error {
	port, err := strconv.ParseInt(newPort, 10, 64)
	if err != nil {
		return err
	}
	wg := &sync.WaitGroup{}
	globalErrMsg := ""
	lock := sync.Mutex{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			err := reqCopyInbound(n, &c.localNode.Node, srcTag, newTag, newProtocol, int32(port), isCopyUser)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				globalErrMsg = fmt.Sprintf("%s|%s", globalErrMsg, err.Error())
				lock.Unlock()
			}
		}(node)
	}
	wg.Wait()
	if globalErrMsg != "" {
		err = fmt.Errorf(globalErrMsg)
	}
	return err
}

func reqCopyUser(node *common.Node, localNode *proto.Node, srcTag, newTag string) error {
	if !node.RegisteredRemote() {
		return nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.CopyUserReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		SrcTag: srcTag,
		NewTag: newTag,
	}

	rsp, err := endNodeAccessClient.CopyUser(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return fmt.Errorf(rsp.GetMsg())
	}
	return err
}

func (c *EndNodeClient) CopyUser(srcTag, newTag string) error {
	wg := &sync.WaitGroup{}
	globalErrMsg := ""
	lock := sync.Mutex{}
	var err error = nil
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			err := reqCopyUser(n, &c.localNode.Node, srcTag, newTag)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				globalErrMsg = fmt.Sprintf("%s|%s", globalErrMsg, err.Error())
				lock.Unlock()
			}
		}(node)
	}
	wg.Wait()
	if globalErrMsg != "" {
		err = fmt.Errorf(globalErrMsg)
	}
	return err
}

func reqGetUsers(node *common.Node, localNode *proto.Node) ([]*proto.User, error) {
	if !node.RegisteredRemote() {
		return []*proto.User{}, nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return nil, err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.GetUsersReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
	}

	rsp, err := endNodeAccessClient.GetUsers(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if err != nil {
		return nil, err
	}
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	users := []*proto.User{}
	for _, u := range rsp.GetUsers() {
		users = append(users, u)
	}
	return users, nil
}

func (c *EndNodeClient) GetUsers() (map[string][]*proto.User, error) {
	wg := &sync.WaitGroup{}
	globalErrMsg := ""
	lock := sync.Mutex{}
	usersMap := map[string][]*proto.User{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			remoteUsers, err := reqGetUsers(n, &c.localNode.Node)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				globalErrMsg = fmt.Sprintf("%s|%s", globalErrMsg, err.Error())
				lock.Unlock()
				return
			}
			lock.Lock()
			usersMap[n.Name] = remoteUsers
			lock.Unlock()

		}(node)
	}
	wg.Wait()
	var err error = nil
	if globalErrMsg != "" {
		err = fmt.Errorf(globalErrMsg)
	}
	return usersMap, err
}

func reqGetInbound(node *common.Node, localNode *proto.Node, tag string) (string, error) {
	if !node.RegisteredRemote() {
		return "", nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return "", err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.GetInboundReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		Tag: tag,
	}

	rsp, err := endNodeAccessClient.GetInbound(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if err != nil {
		return "", err
	}
	if rsp.GetCode() != 0 {
		return "", fmt.Errorf(rsp.GetMsg())
	}
	return rsp.GetData(), nil
}

func (c *EndNodeClient) GetInbound(tag string) ([]string, error) {
	wg := &sync.WaitGroup{}
	globalErrMsg := ""
	lock := sync.Mutex{}
	inbounds := []string{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			inboundData, err := reqGetInbound(n, &c.localNode.Node, tag)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				globalErrMsg = fmt.Sprintf("%s|%s", globalErrMsg, err.Error())
				lock.Unlock()
				return
			}
			lock.Lock()
			inbounds = append(inbounds, inboundData)
			lock.Unlock()

		}(node)
	}
	wg.Wait()
	var err error = nil
	if globalErrMsg != "" {
		err = fmt.Errorf(globalErrMsg)
	}
	return inbounds, err
}

func reqGetTag(node *common.Node, localNode *proto.Node) ([]string, error) {
	if !node.RegisteredRemote() {
		return nil, nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return nil, err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.GetTagReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
	}

	rsp, err := endNodeAccessClient.GetTag(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if err != nil {
		return nil, err
	}
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return rsp.GetTags(), nil
}

func (c *EndNodeClient) GetTag() (map[string][]string, error) {
	wg := &sync.WaitGroup{}
	globalErrMsg := ""
	lock := sync.Mutex{}
	tags := map[string][]string{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			remoteTags, err := reqGetTag(n, &c.localNode.Node)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				globalErrMsg = fmt.Sprintf("%s|%s", globalErrMsg, err.Error())
				lock.Unlock()
				return
			}
			lock.Lock()
			tags[n.Name] = remoteTags
			lock.Unlock()

		}(node)
	}
	wg.Wait()
	var err error = nil
	if globalErrMsg != "" {
		err = fmt.Errorf(globalErrMsg)
	}
	return tags, err
}

func reqUpdateProxy(node *common.Node, localNode *proto.Node, versionTag string) error {
	if !node.RegisteredRemote() {
		return nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.UpdateProxyReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		Tag: versionTag,
	}

	rsp, err := endNodeAccessClient.UpdateProxy(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if err != nil {
		return err
	}
	if rsp.GetCode() != 0 {
		return fmt.Errorf(rsp.GetMsg())
	} else if rsp.GetMsg() != "" {
		logger.Debug("Msg=%s|VersionTag=%s", rsp.GetMsg(), versionTag)
	}
	return nil
}

func (c *EndNodeClient) UpdateProxy(versionTag string) error {
	wg := &sync.WaitGroup{}
	lock := sync.Mutex{}
	succList := []string{}
	failedList := []string{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			err := reqUpdateProxy(n, &c.localNode.Node, versionTag)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				failedList = append(failedList, n.Name+" > "+err.Error())
				lock.Unlock()
				return
			}
			lock.Lock()
			succList = append(succList, n.Name)
			lock.Unlock()

		}(node)
	}
	wg.Wait()
	var err error = nil
	if len(failedList) != 0 {
		errMsg := fmt.Sprintf(
			"succ list: [%s], failed list: [%s]",
			strings.Join(succList, "|"),
			strings.Join(failedList, "|"),
		)
		err = fmt.Errorf(errMsg)
	}
	return err
}

const AddAdaptiveOpType = "addAdaptive"
const DeleteAdaptiveOpType = "deleteAdaptive"

func reqAdaptiveOp(node *common.Node, localNode *proto.Node, tags, ports []string, opType string) error {
	if !node.RegisteredRemote() {
		return nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.AdaptiveOpReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		Ports: ports,
		Tags:  tags,
	}

	var rsp *proto.AdaptiveRsp = nil
	switch opType {
	case AddAdaptiveOpType:
		rsp, err = endNodeAccessClient.AddAdaptiveConfig(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	case DeleteAdaptiveOpType:
		rsp, err = endNodeAccessClient.DeleteAdaptiveConfig(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	}

	if err != nil {
		return err
	}
	if rsp.GetCode() != 0 {
		return fmt.Errorf(rsp.GetMsg())
	}
	return nil
}

func (c *EndNodeClient) AdaptiveOp(ports, tags []string, opType string) error {
	wg := &sync.WaitGroup{}
	lock := sync.Mutex{}
	succList := []string{}
	failedList := []string{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			err := reqAdaptiveOp(n, &c.localNode.Node, tags, ports, opType)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				failedList = append(failedList, n.Name+" > "+err.Error())
				lock.Unlock()
				return
			}
			lock.Lock()
			succList = append(succList, n.Name)
			lock.Unlock()

		}(node)
	}
	wg.Wait()
	var err error = nil
	if len(failedList) != 0 {
		errMsg := fmt.Sprintf(
			"succ list: [%s], failed list: [%s]",
			strings.Join(succList, "|"),
			strings.Join(failedList, "|"),
		)
		err = fmt.Errorf(errMsg)
	}
	return err
}

func reqAdaptive(node *common.Node, localNode *proto.Node, tags []string) error {
	if !node.RegisteredRemote() {
		return nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.AdaptiveReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		Tags: tags,
	}

	rsp, err := endNodeAccessClient.Adaptive(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))

	if err != nil {
		return err
	}
	if rsp.GetCode() != 0 {
		return fmt.Errorf(rsp.GetMsg())
	}
	return nil
}

func (c *EndNodeClient) Adaptive(tags []string) error {
	wg := &sync.WaitGroup{}
	lock := sync.Mutex{}
	succList := []string{}
	failedList := []string{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			err := reqAdaptive(n, &c.localNode.Node, tags)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				failedList = append(failedList, n.Name+" > "+err.Error())
				lock.Unlock()
				return
			}
			lock.Lock()
			succList = append(succList, n.Name)
			lock.Unlock()

		}(node)
	}
	wg.Wait()
	var err error = nil
	if len(failedList) != 0 {
		errMsg := fmt.Sprintf(
			"succ list: [%s], failed list: [%s]",
			strings.Join(succList, "|"),
			strings.Join(failedList, "|"),
		)
		err = fmt.Errorf(errMsg)
	}
	return err
}

func reqSetGatewayModel(node *common.Node, localNode *proto.Node, enableGatewayModel bool) error {
	if !node.RegisteredRemote() {
		return nil
	}
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		return err
	}

	endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
	req := &proto.SetGatewayModelReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: node.OutToken,
			Node:  localNode,
		},
		EnableGatewayModel: enableGatewayModel,
	}

	rsp, err := endNodeAccessClient.SetGatewayModel(context.Background(), req, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))

	if err != nil {
		return err
	}
	if rsp.GetCode() != 0 {
		return fmt.Errorf(rsp.GetMsg())
	}
	return nil
}

func (c *EndNodeClient) SetGatewayModel(enableGatewayModel bool) error {
	wg := &sync.WaitGroup{}
	lock := sync.Mutex{}
	succList := []string{}
	failedList := []string{}
	for _, node := range *c.nodes {
		ch <- struct{}{}
		wg.Add(1)
		go func(n *common.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			err := reqSetGatewayModel(n, &c.localNode.Node, enableGatewayModel)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
				)
				lock.Lock()
				failedList = append(failedList, n.Name+" > "+err.Error())
				lock.Unlock()
				return
			}
			lock.Lock()
			succList = append(succList, n.Name)
			lock.Unlock()

		}(node)
	}
	wg.Wait()
	var err error = nil
	if len(failedList) != 0 {
		errMsg := fmt.Sprintf(
			"succ list: [%s], failed list: [%s]",
			strings.Join(succList, "|"),
			strings.Join(failedList, "|"),
		)
		err = fmt.Errorf(errMsg)
	}
	return err
}
