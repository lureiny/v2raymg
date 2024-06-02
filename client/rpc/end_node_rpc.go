package rpc

import (
	"context"
	"fmt"
	"sync"

	"github.com/lureiny/v2raymg/cluster"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/common/rpc"
	gc "github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/server/rpc/proto"
	"google.golang.org/grpc"
	pb "google.golang.org/protobuf/proto"
)

const MaxConcurrencyClientNum = 64

type ReqToEndNodeFunc func(context.Context, []byte, proto.EndNodeAccessClient, *proto.NodeAuthInfo, string) (interface{}, error)

var reqFuncMap = map[ReqToEndNodeType]ReqToEndNodeFunc{}

func init() {
	// add user
	registerReqToEndNodeFunc(AddUsersReqType, ReqAddUsers)
	// delete user
	registerReqToEndNodeFunc(DeleteUsersReqType, ReqDeleteUsers)
	// update user
	registerReqToEndNodeFunc(UpdateUsersReqType, ReqUpdateUsers)
	// reset user
	registerReqToEndNodeFunc(ResetUserReqType, ReqResetUser)
	// get sub
	registerReqToEndNodeFunc(GetSubReqType, ReqGetSub)
	// get bandwidth stats
	registerReqToEndNodeFunc(GetBandWidthStatsReqType, ReqGetBandwidthStats)
	// add inbound
	registerReqToEndNodeFunc(AddInboundReqType, ReqAddInbound)
	// delete inbound
	registerReqToEndNodeFunc(DeleteInboundReqType, ReqDeleteInbound)
	// transfer inbound
	registerReqToEndNodeFunc(TransferInboundReqType, ReqTransferInbound)
	// copy inbound
	registerReqToEndNodeFunc(CopyInboundReqType, ReqCopyInbound)
	// copy user
	registerReqToEndNodeFunc(CopyUserReqType, ReqCopyUser)
	// get users
	registerReqToEndNodeFunc(GetUsersReqType, ReqGetUsers)
	// get inbound
	registerReqToEndNodeFunc(GetInboundReqType, ReqGetInbound)
	// get tag
	registerReqToEndNodeFunc(GetTagReqType, ReqGetTag)
	// update proxy
	registerReqToEndNodeFunc(UpdateProxyReqType, ReqUpdateProxy)
	// add adaptive
	registerReqToEndNodeFunc(AddAdaptiveConfigReqType, ReqAddAdaptiveOp)
	// delete adaptive
	registerReqToEndNodeFunc(DeleteAdaptiveConfigReqType, ReqDeleteAdaptiveOp)
	// adaptive
	registerReqToEndNodeFunc(AdaptiveReqType, ReqAdaptive)
	// obtain new cert
	registerReqToEndNodeFunc(ObtainNewCertType, ReqObtainNewCert)
	// set gateway model
	registerReqToEndNodeFunc(SetGatewayModelReqType, ReqSetGatewayModel)
	// fast add inbound
	registerReqToEndNodeFunc(FastAddInboundType, ReqFastAddInbound)
	// Transfer Cert
	registerReqToEndNodeFunc(TransferCertType, ReqTransferCert)
	// get certs
	registerReqToEndNodeFunc(GetCertsType, ReqGetCerts)
	// clear users
	registerReqToEndNodeFunc(ClearUsersType, ReqClearUsers)
	// get ping metric
	registerReqToEndNodeFunc(GetPingMetricType, ReqGetPingMetric)
	// register node
	registerReqToEndNodeFunc(RegisterNodeType, ReqRegisterNode)
}

func registerReqToEndNodeFunc(reqType ReqToEndNodeType, f ReqToEndNodeFunc) {
	reqFuncMap[reqType] = f
}

// 控制全局rpc请求数
var ch = make(chan struct{}, MaxConcurrencyClientNum)

type EndNodeClient struct {
	nodes     []*cluster.Node
	localNode *cluster.LocalNode
}

// NewEndNodeClient ...
func NewEndNodeClient(nodes []*cluster.Node, localNode *cluster.LocalNode) *EndNodeClient {
	if nodes == nil {
		return nil
	}

	if localNode == nil {
		localNode = gc.LocalNode
	}
	endNodeClient := &EndNodeClient{}
	endNodeClient.nodes = nodes
	endNodeClient.localNode = localNode
	return endNodeClient
}

func processUserOpRsp(rsp *proto.UserOpRsp, err error) (interface{}, error) {
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return nil, err
}

func ReqRegisterNode(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	registerNodeReq := &proto.RegisterNodeReq{}
	if err := pb.Unmarshal(reqData, registerNodeReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to register node req > %v", reqData, err)
	}
	registerNodeReq.NodeAuthInfo = nodeAuthInfo
	return endNodeAccessClient.RegisterNode(ctx, registerNodeReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
}

func ReqHeartBeat(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	heartBeatReq := &proto.HeartBeatReq{}
	if err := pb.Unmarshal(reqData, heartBeatReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to heart beat req > %v", reqData, err)
	}
	heartBeatReq.NodeAuthInfo = nodeAuthInfo
	return endNodeAccessClient.HeartBeat(ctx, heartBeatReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
}

func ReqAddUsers(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	addUsersReq := &proto.UserOpReq{}
	if err := pb.Unmarshal(reqData, addUsersReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to add users req > %v", reqData, err)
	}

	addUsersReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.AddUsers(ctx, addUsersReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	return processUserOpRsp(rsp, err)
}

func ReqDeleteUsers(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	deleteUsersReq := &proto.UserOpReq{}
	if err := pb.Unmarshal(reqData, deleteUsersReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to delete users req > %v", reqData, err)
	}

	deleteUsersReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.DeleteUsers(ctx, deleteUsersReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	return processUserOpRsp(rsp, err)
}

func ReqUpdateUsers(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	updateUsersReq := &proto.UserOpReq{}
	if err := pb.Unmarshal(reqData, updateUsersReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to update users req > %v", reqData, err)
	}

	updateUsersReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.UpdateUsers(ctx, updateUsersReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	return processUserOpRsp(rsp, err)
}

func ReqResetUser(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	resetUserReq := &proto.UserOpReq{}
	if err := pb.Unmarshal(reqData, resetUserReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to reset users req > %v", reqData, err)
	}

	resetUserReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.ResetUser(ctx, resetUserReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	return processUserOpRsp(rsp, err)
}

func ReqGetSub(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	getSubReq := &proto.GetSubReq{}
	if err := pb.Unmarshal(reqData, getSubReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to get sub req > %v", reqData, err)
	}

	getSubReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetSub(ctx, getSubReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if err != nil {
		return nil, err
	}
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return rsp.GetUris(), nil
}

func ReqGetBandwidthStats(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	getBandWidthStatsReq := &proto.GetBandwidthStatsReq{}
	if err := pb.Unmarshal(reqData, getBandWidthStatsReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to get badnwidth stats req > %v", reqData, err)
	}

	getBandWidthStatsReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetBandWidthStats(ctx, getBandWidthStatsReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if err != nil {
		return []*proto.Stats{}, err
	}
	if rsp.GetCode() != 0 {
		return []*proto.Stats{}, fmt.Errorf(rsp.GetMsg())
	}
	return rsp.GetStats(), nil

}

func ReqAddInbound(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	addInboundReq := &proto.InboundOpReq{}
	if err := pb.Unmarshal(reqData, addInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to add inbound req > %v", reqData, err)
	}

	addInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.AddInbound(ctx, addInboundReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return nil, err
}

func ReqDeleteInbound(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	deleteInboundReq := &proto.InboundOpReq{}
	if err := pb.Unmarshal(reqData, deleteInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to delete inbound req > %v", reqData, err)
	}

	deleteInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.DeleteInbound(ctx, deleteInboundReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return nil, err
}

func ReqTransferInbound(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	transferInboundReq := &proto.TransferInboundReq{}
	if err := pb.Unmarshal(reqData, transferInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to TransferInboundReq > %v", reqData, err)
	}

	transferInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.TransferInbound(ctx, transferInboundReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return nil, err
}

func ReqCopyInbound(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	copyInboundReq := &proto.CopyInboundReq{}
	if err := pb.Unmarshal(reqData, copyInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to CopyInboundReq > %v", reqData, err)
	}

	copyInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.CopyInbound(ctx, copyInboundReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return nil, err
}

func ReqCopyUser(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	copyUserReq := &proto.CopyUserReq{}
	if err := pb.Unmarshal(reqData, copyUserReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to CopyUserReq > %v", reqData, err)
	}

	copyUserReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.CopyUser(ctx, copyUserReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return nil, err
}

func ReqGetUsers(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	getUsersReq := &proto.GetUsersReq{}
	if err := pb.Unmarshal(reqData, getUsersReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to GetUsersReq > %v", reqData, err)
	}

	getUsersReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetUsers(ctx, getUsersReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return rsp.GetUsers(), nil
}

func ReqGetInbound(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	getInboundReq := &proto.GetInboundReq{}
	if err := pb.Unmarshal(reqData, getInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to GetInboundReq > %v", reqData, err)
	}

	getInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetInbound(ctx, getInboundReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return "", fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return "", err
	}
	return rsp.GetData(), nil
}

func ReqGetTag(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	getTagReq := &proto.GetTagReq{}
	if err := pb.Unmarshal(reqData, getTagReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to GetTagReq > %v", reqData, err)
	}

	getTagReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetTag(ctx, getTagReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return "", fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return "", err
	}
	return rsp.GetTags(), nil
}

func ReqUpdateProxy(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	updateProxyReq := &proto.UpdateProxyReq{}
	if err := pb.Unmarshal(reqData, updateProxyReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to UpdateProxyReq > %v", reqData, err)
	}

	updateProxyReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.UpdateProxy(ctx, updateProxyReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func ReqAddAdaptiveOp(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	addAdaptiveOp := &proto.AdaptiveOpReq{}
	if err := pb.Unmarshal(reqData, addAdaptiveOp); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to add adaptive op req > %v", reqData, err)
	}

	addAdaptiveOp.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.AddAdaptiveConfig(ctx, addAdaptiveOp, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func ReqDeleteAdaptiveOp(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	delteteAdaptiveOp := &proto.AdaptiveOpReq{}
	if err := pb.Unmarshal(reqData, delteteAdaptiveOp); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to delete adaptive op req > %v", reqData, err)
	}

	delteteAdaptiveOp.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.AddAdaptiveConfig(ctx, delteteAdaptiveOp, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func ReqAdaptive(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	adaptiveReq := &proto.AdaptiveReq{}
	if err := pb.Unmarshal(reqData, adaptiveReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to AdaptiveReq > %v", reqData, err)
	}

	adaptiveReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.Adaptive(ctx, adaptiveReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func ReqSetGatewayModel(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	setGatewayModelReq := &proto.SetGatewayModelReq{}
	if err := pb.Unmarshal(reqData, setGatewayModelReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to SetGatewayModelReq > %v", reqData, err)
	}

	setGatewayModelReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.SetGatewayModel(ctx, setGatewayModelReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func ReqObtainNewCert(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	obtainNewCertReq := &proto.ObtainNewCertReq{}
	if err := pb.Unmarshal(reqData, obtainNewCertReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to ObtainNewCertReq > %v", reqData, err)
	}

	obtainNewCertReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.ObtainNewCert(ctx, obtainNewCertReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func ReqFastAddInbound(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	fastAddInboundReq := &proto.FastAddInboundReq{}
	if err := pb.Unmarshal(reqData, fastAddInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to FastAddInboundReq > %v", reqData, err)
	}

	fastAddInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.FastAddInbound(ctx, fastAddInboundReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func ReqTransferCert(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	transferCertReq := &proto.TransferCertReq{}
	if err := pb.Unmarshal(reqData, transferCertReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to TransferCertReq > %v", reqData, err)
	}

	transferCertReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.TransferCert(ctx, transferCertReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func ReqGetCerts(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	getCertsReq := &proto.GetCertsReq{}
	if err := pb.Unmarshal(reqData, getCertsReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to GetCertsReq > %v", reqData, err)
	}

	getCertsReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetCerts(ctx, getCertsReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if err != nil {
		return nil, err
	}
	return rsp.GetCerts(), nil
}

func ReqClearUsers(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	clearUsersReq := &proto.ClearUsersReq{}
	if err := pb.Unmarshal(reqData, clearUsersReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to ClearUsersReq > %v", reqData, err)
	}

	clearUsersReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.ClearUsers(ctx, clearUsersReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func ReqGetPingMetric(ctx context.Context, reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, token string) (interface{}, error) {
	getPingMetricReq := &proto.GetPingMetricReq{}
	if err := pb.Unmarshal(reqData, getPingMetricReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to GetPingMetricReq > %v", reqData, err)
	}

	getPingMetricReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetPingMetric(ctx, getPingMetricReq, grpc.ForceCodec(rpc.NewEncryptMessageCodec(token)))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return rsp.GetMetric(), nil
}

func getReqAndCallbakcFunc(reqType ReqToEndNodeType) ReqToEndNodeFunc {
	if reqFunc, ok := reqFuncMap[reqType]; ok {
		return reqFunc
	}
	return nil
}

func (c *EndNodeClient) ReqToMultiEndNodeServer(ctx context.Context, reqType ReqToEndNodeType, req interface{}, token string) (succList map[string]interface{}, failedList map[string]string, err error) {
	succList = map[string]interface{}{}
	failedList = map[string]string{}
	err = nil
	wg := &sync.WaitGroup{}
	lock := sync.Mutex{}
	reqData, err := pb.Marshal(req.(pb.Message))
	if err != nil {
		err = fmt.Errorf("req message can't marshal > %v, Req: %v", err, req)
		return
	}
	reqFunc := getReqAndCallbakcFunc(reqType)
	if reqFunc == nil {
		err = fmt.Errorf("unsupport req type: %v, Req: %v", reqType, req)
		return
	}
	for _, node := range c.nodes {
		if reqType != RegisterNodeType && !node.RegisteredRemote() {
			continue
		}
		ch <- struct{}{}
		wg.Add(1)
		go func(n *cluster.Node) {
			defer func() {
				<-ch
				wg.Done()
			}()
			conn, err := n.GetGrpcClientConn()
			if err != nil {
				lock.Lock()
				failedList[n.Name] = err.Error()
				lock.Unlock()
				return
			}
			endNodeAccessClient := proto.NewEndNodeAccessClient(conn)
			nodeAuthInfo := &proto.NodeAuthInfo{
				Token: n.OutToken,
				Node:  &c.localNode.Node,
			}
			reqCtx := NewContext()
			if ctx != nil {
				reqCtx = ctx
			}
			result, err := reqFunc(reqCtx, reqData, endNodeAccessClient, nodeAuthInfo, token)
			if err != nil {
				logger.Error(
					"Err=%s|Dst=%s:%d|DstName=%s|ReqType=%d",
					err.Error(),
					n.Host,
					n.Port,
					n.Name,
					reqType,
				)
				lock.Lock()
				failedList[n.Name] = err.Error()
				lock.Unlock()
				return
			}
			lock.Lock()
			succList[n.Name] = result
			lock.Unlock()
		}(node)
	}
	wg.Wait()
	return
}
