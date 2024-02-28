package rpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/golang/protobuf/proto"
	"github.com/lureiny/v2raymg/cluster"
	"github.com/lureiny/v2raymg/common/log/logger"
	gc "github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/server/rpc"
	"github.com/lureiny/v2raymg/server/rpc/proto"
	"google.golang.org/grpc"
)

const MaxConcurrencyClientNum = 64

const RpcTimeOut = 10 // 秒

type ReqToEndNodeFunc func([]byte, proto.EndNodeAccessClient, *proto.NodeAuthInfo, context.Context) (interface{}, error)

var reqFuncMap = map[ReqToEndNodeType]ReqToEndNodeFunc{}

func init() {
	// add user
	registerReqToEndNodeFunc(AddUsersReqType, reqAddUsers)
	// delete user
	registerReqToEndNodeFunc(DeleteUsersReqType, reqDeleteUsers)
	// update user
	registerReqToEndNodeFunc(UpdateUsersReqType, reqUpdateUsers)
	// reset user
	registerReqToEndNodeFunc(ResetUserReqType, reqResetUser)
	// get sub
	registerReqToEndNodeFunc(GetSubReqType, reqGetSub)
	// get bandwidth stats
	registerReqToEndNodeFunc(GetBandWidthStatsReqType, reqGetBandwidthStats)
	// add inbound
	registerReqToEndNodeFunc(AddInboundReqType, reqAddInbound)
	// delete inbound
	registerReqToEndNodeFunc(DeleteInboundReqType, reqDeleteInbound)
	// transfer inbound
	registerReqToEndNodeFunc(TransferInboundReqType, reqTransferInbound)
	// copy inbound
	registerReqToEndNodeFunc(CopyInboundReqType, reqCopyInbound)
	// copy user
	registerReqToEndNodeFunc(CopyUserReqType, reqCopyUser)
	// get users
	registerReqToEndNodeFunc(GetUsersReqType, reqGetUsers)
	// get inbound
	registerReqToEndNodeFunc(GetInboundReqType, reqGetInbound)
	// get tag
	registerReqToEndNodeFunc(GetTagReqType, reqGetTag)
	// update proxy
	registerReqToEndNodeFunc(UpdateProxyReqType, reqUpdateProxy)
	// add adaptive
	registerReqToEndNodeFunc(AddAdaptiveConfigReqType, reqAddAdaptiveOp)
	// delete adaptive
	registerReqToEndNodeFunc(DeleteAdaptiveConfigReqType, reqDeleteAdaptiveOp)
	// adaptive
	registerReqToEndNodeFunc(AdaptiveReqType, reqAdaptive)
	// obtain new cert
	registerReqToEndNodeFunc(ObtainNewCertType, reqObtainNewCert)
	// set gateway model
	registerReqToEndNodeFunc(SetGatewayModelReqType, reqSetGatewayModel)
	// fast add inbound
	registerReqToEndNodeFunc(FastAddInboundType, reqFastAddInbound)
	// Transfer Cert
	registerReqToEndNodeFunc(TransferCertType, reqTransferCert)
	// get certs
	registerReqToEndNodeFunc(GetCertsType, reqGetCerts)
	// clear users
	registerReqToEndNodeFunc(ClearUsersType, reqClearUsers)
	// get ping metric
	registerReqToEndNodeFunc(GetPingMetricType, reqGetPingMetric)
}

func registerReqToEndNodeFunc(reqType ReqToEndNodeType, f ReqToEndNodeFunc) {
	reqFuncMap[reqType] = f
}

// 控制全局rpc请求数
var ch = make(chan struct{}, MaxConcurrencyClientNum)

type EndNodeClient struct {
	nodes     *[]*cluster.Node
	localNode *cluster.LocalNode
}

// NewEndNodeClient ...
func NewEndNodeClient(nodes *[]*cluster.Node, localNode *cluster.LocalNode) *EndNodeClient {
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

func reqAddUsers(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	addUsersReq := &proto.UserOpReq{}
	if err := pb.Unmarshal(reqData, addUsersReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to add users req > %v", reqData, err)
	}

	addUsersReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.AddUsers(ctx, addUsersReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	return processUserOpRsp(rsp, err)
}

func reqDeleteUsers(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	deleteUsersReq := &proto.UserOpReq{}
	if err := pb.Unmarshal(reqData, deleteUsersReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to delete users req > %v", reqData, err)
	}

	deleteUsersReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.DeleteUsers(ctx, deleteUsersReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	return processUserOpRsp(rsp, err)
}

func reqUpdateUsers(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	updateUsersReq := &proto.UserOpReq{}
	if err := pb.Unmarshal(reqData, updateUsersReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to update users req > %v", reqData, err)
	}

	updateUsersReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.UpdateUsers(ctx, updateUsersReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	return processUserOpRsp(rsp, err)
}

func reqResetUser(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	resetUserReq := &proto.UserOpReq{}
	if err := pb.Unmarshal(reqData, resetUserReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to reset users req > %v", reqData, err)
	}

	resetUserReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.ResetUser(ctx, resetUserReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	return processUserOpRsp(rsp, err)
}

func reqGetSub(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	getSubReq := &proto.GetSubReq{}
	if err := pb.Unmarshal(reqData, getSubReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to get sub req > %v", reqData, err)
	}

	getSubReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetSub(ctx, getSubReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if err != nil {
		return nil, err
	}
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return rsp.GetUris(), nil
}

func reqGetBandwidthStats(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	getBandWidthStatsReq := &proto.GetBandwidthStatsReq{}
	if err := pb.Unmarshal(reqData, getBandWidthStatsReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to get badnwidth stats req > %v", reqData, err)
	}

	getBandWidthStatsReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetBandWidthStats(ctx, getBandWidthStatsReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if err != nil {
		return []*proto.Stats{}, err
	}
	if rsp.GetCode() != 0 {
		return []*proto.Stats{}, fmt.Errorf(rsp.GetMsg())
	}
	return rsp.GetStats(), nil

}

func reqAddInbound(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	addInboundReq := &proto.InboundOpReq{}
	if err := pb.Unmarshal(reqData, addInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to add inbound req > %v", reqData, err)
	}

	addInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.AddInbound(ctx, addInboundReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return nil, err
}

func reqDeleteInbound(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	deleteInboundReq := &proto.InboundOpReq{}
	if err := pb.Unmarshal(reqData, deleteInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to delete inbound req > %v", reqData, err)
	}

	deleteInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.DeleteInbound(ctx, deleteInboundReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return nil, err
}

func reqTransferInbound(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	transferInboundReq := &proto.TransferInboundReq{}
	if err := pb.Unmarshal(reqData, transferInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to TransferInboundReq > %v", reqData, err)
	}

	transferInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.TransferInbound(ctx, transferInboundReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return nil, err
}

func reqCopyInbound(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	copyInboundReq := &proto.CopyInboundReq{}
	if err := pb.Unmarshal(reqData, copyInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to CopyInboundReq > %v", reqData, err)
	}

	copyInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.CopyInbound(ctx, copyInboundReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return nil, err
}

func reqCopyUser(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	copyUserReq := &proto.CopyUserReq{}
	if err := pb.Unmarshal(reqData, copyUserReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to CopyUserReq > %v", reqData, err)
	}

	copyUserReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.CopyUser(ctx, copyUserReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	return nil, err
}

func reqGetUsers(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	getUsersReq := &proto.GetUsersReq{}
	if err := pb.Unmarshal(reqData, getUsersReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to GetUsersReq > %v", reqData, err)
	}

	getUsersReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetUsers(ctx, getUsersReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return rsp.GetUsers(), nil
}

func reqGetInbound(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	getInboundReq := &proto.GetInboundReq{}
	if err := pb.Unmarshal(reqData, getInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to GetInboundReq > %v", reqData, err)
	}

	getInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetInbound(ctx, getInboundReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return "", fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return "", err
	}
	return rsp.GetData(), nil
}

func reqGetTag(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	getTagReq := &proto.GetTagReq{}
	if err := pb.Unmarshal(reqData, getTagReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to GetTagReq > %v", reqData, err)
	}

	getTagReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetTag(ctx, getTagReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return "", fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return "", err
	}
	return rsp.GetTags(), nil
}

func reqUpdateProxy(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	updateProxyReq := &proto.UpdateProxyReq{}
	if err := pb.Unmarshal(reqData, updateProxyReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to UpdateProxyReq > %v", reqData, err)
	}

	updateProxyReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.UpdateProxy(ctx, updateProxyReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func reqAddAdaptiveOp(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	addAdaptiveOp := &proto.AdaptiveOpReq{}
	if err := pb.Unmarshal(reqData, addAdaptiveOp); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to add adaptive op req > %v", reqData, err)
	}

	addAdaptiveOp.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.AddAdaptiveConfig(ctx, addAdaptiveOp, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func reqDeleteAdaptiveOp(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	delteteAdaptiveOp := &proto.AdaptiveOpReq{}
	if err := pb.Unmarshal(reqData, delteteAdaptiveOp); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to delete adaptive op req > %v", reqData, err)
	}

	delteteAdaptiveOp.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.AddAdaptiveConfig(ctx, delteteAdaptiveOp, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func reqAdaptive(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	adaptiveReq := &proto.AdaptiveReq{}
	if err := pb.Unmarshal(reqData, adaptiveReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to AdaptiveReq > %v", reqData, err)
	}

	adaptiveReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.Adaptive(ctx, adaptiveReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func reqSetGatewayModel(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	setGatewayModelReq := &proto.SetGatewayModelReq{}
	if err := pb.Unmarshal(reqData, setGatewayModelReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to SetGatewayModelReq > %v", reqData, err)
	}

	setGatewayModelReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.SetGatewayModel(ctx, setGatewayModelReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func reqObtainNewCert(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	obtainNewCertReq := &proto.ObtainNewCertReq{}
	if err := pb.Unmarshal(reqData, obtainNewCertReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to ObtainNewCertReq > %v", reqData, err)
	}

	obtainNewCertReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.ObtainNewCert(ctx, obtainNewCertReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func reqFastAddInbound(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	fastAddInboundReq := &proto.FastAddInboundReq{}
	if err := pb.Unmarshal(reqData, fastAddInboundReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to FastAddInboundReq > %v", reqData, err)
	}

	fastAddInboundReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.FastAddInbound(ctx, fastAddInboundReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func reqTransferCert(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	transferCertReq := &proto.TransferCertReq{}
	if err := pb.Unmarshal(reqData, transferCertReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to TransferCertReq > %v", reqData, err)
	}

	transferCertReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.TransferCert(ctx, transferCertReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func reqGetCerts(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	getCertsReq := &proto.GetCertsReq{}
	if err := pb.Unmarshal(reqData, getCertsReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to GetCertsReq > %v", reqData, err)
	}

	getCertsReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetCerts(ctx, getCertsReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if err != nil {
		return nil, err
	}
	return rsp.GetCerts(), nil
}

func reqClearUsers(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	clearUsersReq := &proto.ClearUsersReq{}
	if err := pb.Unmarshal(reqData, clearUsersReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to ClearUsersReq > %v", reqData, err)
	}

	clearUsersReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.ClearUsers(ctx, clearUsersReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
	if rsp.GetCode() != 0 {
		return nil, fmt.Errorf(rsp.GetMsg())
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func reqGetPingMetric(reqData []byte, endNodeAccessClient proto.EndNodeAccessClient, nodeAuthInfo *proto.NodeAuthInfo, ctx context.Context) (interface{}, error) {
	getPingMetricReq := &proto.GetPingMetricReq{}
	if err := pb.Unmarshal(reqData, getPingMetricReq); err != nil {
		return nil, fmt.Errorf("can't unmarshal req[%v] to GetPingMetricReq > %v", reqData, err)
	}

	getPingMetricReq.NodeAuthInfo = nodeAuthInfo
	rsp, err := endNodeAccessClient.GetPingMetric(ctx, getPingMetricReq, grpc.ForceCodec(&rpc.EncryptMessageCodec{}))
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

func (c *EndNodeClient) ReqToMultiEndNodeServer(reqType ReqToEndNodeType, req interface{}) (succList map[string]interface{}, failedList map[string]string, err error) {
	succList = map[string]interface{}{}
	failedList = map[string]string{}
	err = nil
	wg := &sync.WaitGroup{}
	lock := sync.Mutex{}
	reqData, err := pb.Marshal(req.(pb.Message))
	if err != nil {
		err = fmt.Errorf("req message can't marshal > %v, req: %v", err, req)
		return
	}
	reqFunc := getReqAndCallbakcFunc(reqType)
	if reqFunc == nil {
		err = fmt.Errorf("unsupport req type: %v, req: %v", reqType, req)
		return
	}
	for _, node := range *c.nodes {
		if !node.RegisteredRemote() {
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
			ctx, _ := context.WithTimeout(context.Background(), RpcTimeOut*time.Second)
			result, err := reqFunc(reqData, endNodeAccessClient, nodeAuthInfo, ctx)
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
