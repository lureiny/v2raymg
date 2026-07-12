package server

import (
	context "context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	rpcClient "github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	usync "github.com/lureiny/v2raymg/pkg/proxy/usermanager/sync"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	pb "google.golang.org/protobuf/proto"
)

func (s *EndNodeServer) RegisterNode(ctx context.Context, registerNodeReq *proto.RegisterNodeReq) (*proto.RegisterNodeRsp, error) {
	registerNodeRsp := &proto.RegisterNodeRsp{}
	clusterToken := registerNodeReq.GetNodeAuthInfo().GetToken()
	node := registerNodeReq.GetNodeAuthInfo().GetNode()

	if node.Host == "" || node.Port <= 0 {
		errMsg := "empty host or invalid port"
		log.Error("register node failed", "err", errMsg, "src_host", node.Host, "src_port", node.Port)
		registerNodeRsp.Code = 100
		registerNodeRsp.Msg = errMsg
		return registerNodeRsp, nil
	}
	if node.Name == s.Name {
		errMsg := "remote node has same name with local node"
		log.Error("register node failed", "err", errMsg, "src_host", node.Host, "src_port", node.Port)
		registerNodeRsp.Code = 103
		registerNodeRsp.Msg = errMsg
		return registerNodeRsp, nil
	}

	if err := s.clusterState.IsSameCluster(node.GetClusterName(), clusterToken); err != nil {
		errMsg := err.Error()
		log.Info("cluster mismatch",
			"err", errMsg,
			"src_host", node.Host, "src_port", node.Port,
			"local_cluster", localNode.ClusterName,
			"registered_cluster", node.GetClusterName(),
		)
		s.clusterState.AddToWrongNodeList(&cluster.Node{
			Node:       node,
			CreateTime: time.Now().Unix(),
		})
		registerNodeRsp.Msg = errMsg
		registerNodeRsp.Code = 101
		return registerNodeRsp, nil
	}

	nodeName := node.Name
	token := ""
	s.clusterState.DeleteFromWrongTokenNodeList(nodeName)

	if n := s.clusterState.Get(nodeName); n != nil {
		if !n.CompareWithProtoNode(node) {
			errMsg := "repeated register, bug with different node"
			log.Error("register node failed", "err", errMsg, "src_name", nodeName)
			registerNodeRsp.Code = 105
			registerNodeRsp.Msg = errMsg
			return registerNodeRsp, nil
		}
		if n.RegisteredLocal() {
			errMsg := "repeated register"
			log.Error("register node failed", "err", errMsg, "src_name", nodeName)
			registerNodeRsp.Code = 102
			registerNodeRsp.Msg = errMsg
			token = n.GetInToken()
		} else {
			token = uuid.New().String()
			n.SetInToken(token)
			log.Info("register node update",
				"src_host", node.Host, "src_port", node.Port,
				"src_name", nodeName, "cluster", node.ClusterName,
			)
		}
		n.SetRecvHeartBeatTime(time.Now().Unix())
	} else {
		token = uuid.New().String()
		newNode := &cluster.Node{
			Node:       node,
			CreateTime: time.Now().Unix(),
		}
		newNode.SetInToken(token)
		newNode.SetRecvHeartBeatTime(time.Now().Unix())
		s.clusterState.Add(newNode)
		log.Info("register node new",
			"src_host", node.Host, "src_port", node.Port,
			"src_name", nodeName, "cluster", node.ClusterName,
		)
	}
	registerNodeRsp.Data = []byte(token)
	return registerNodeRsp, nil
}

func (s *EndNodeServer) HeartBeat(ctx context.Context, heartBeatReq *proto.HeartBeatReq) (*proto.HeartBeatRsp, error) {
	heartBeatRsp := &proto.HeartBeatRsp{}

	// Validate heartbeat timestamp (skip if 0 for backward compatibility).
	if ts := heartBeatReq.GetTimestampUs(); ts != 0 {
		drift := time.Now().UnixMicro() - ts
		if drift < 0 {
			drift = -drift
		}
		if drift > heartbeatMaxDriftUs {
			heartBeatRsp.Code = 105
			heartBeatRsp.Msg = "heartbeat timestamp drift too large"
			log.Error("heartbeat rejected", "err", "timestamp drift exceeds 30s",
				"src_name", heartBeatReq.GetNodeAuthInfo().GetNode().GetName(),
				"drift_ms", drift/1000)
			return heartBeatRsp, nil
		}
	}

	node := s.clusterState.Get(heartBeatReq.GetNodeAuthInfo().Node.GetName())
	if node == nil {
		heartBeatRsp.Code = 104
		heartBeatRsp.Msg = "node has been drop"
		log.Error("heartbeat failed", "err", fmt.Sprintf("node[%s] has been drop", heartBeatReq.GetNodeAuthInfo().Node.GetName()))
		return heartBeatRsp, nil
	}
	heartBeatRsp.NodesMap = s.clusterState.GetProtoNodesWithFilter(
		func(node *cluster.Node) bool {
			return node.Name != s.Name && node.IsCompleteRegister()
		},
	)

	// Cluster user digest comparison (only when feature is enabled).
	if s.userMgr.IsClusterEnabled() {
		protoDigests := heartBeatReq.GetUserDigests()
		digests := make([]usync.UserDigest, 0, len(protoDigests))
		for _, pd := range protoDigests {
			if pd == nil {
				continue
			}
			digests = append(digests, usync.UserDigest{
				Username:    pd.GetUsername(),
				UpdatedAtUs: pd.GetUpdatedAtUs(),
				OriginNode:  pd.GetOriginNode(),
				Hash:        pd.GetHash(),
			})
		}
		getLocal := func(username string) (*contracts.User, error) {
			u := s.userMgr.GetUserForSync(username)
			return u, nil
		}
		needFull, err := usync.CompareDigests(getLocal, digests)
		if err != nil {
			log.Warn("heartbeat: compare digests had partial failures", "err", err)
		}
		heartBeatRsp.NeedClusterUsers = needFull
	}

	return heartBeatRsp, nil
}

func (s *EndNodeServer) registerToEndNode(node *cluster.Node, wg *sync.WaitGroup, ch chan struct{}) {
	defer func() {
		wg.Done()
		<-ch
	}()
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		log.Error("register to end node failed",
			"err", fmt.Sprintf("did not connect > %v", err),
			"dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
		)
		return
	}

	c := proto.NewEndNodeAccessClient(conn)
	nodeAuthInfo := &proto.NodeAuthInfo{
		Token: s.clusterState.GetClusterToken(),
		Node:  &localNode.Node,
	}
	var registerNodeReq interface{} = &proto.RegisterNodeReq{}
	reqData, _ := pb.Marshal(registerNodeReq.(pb.Message))
	rsp, err := rpcClient.ReqRegisterNode(rpcClient.NewContext(), reqData, c, nodeAuthInfo, s.clusterState.GetClusterToken())
	if err != nil {
		log.Error("register to end node failed",
			"err", fmt.Sprintf("register to end node failed > %v", err),
			"dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
		)
		return
	}
	registerRsp, ok := rsp.(*proto.RegisterNodeRsp)
	if !ok || registerRsp == nil {
		log.Error("register to end node failed", "err", "unexpected nil response",
			"dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
		)
		return
	}
	if registerRsp.GetCode() != 0 {
		errMsg := registerRsp.GetMsg()
		if !node.IsLocal() && registerRsp.GetCode() > 0 && registerRsp.GetCode() != 102 {
			s.clusterState.Delete(node.GetName())
			s.clusterState.AddToWrongNodeList(node)
		}
		log.Error("register to end node failed",
			"err", errMsg, "dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
		)
	}
	if len(registerRsp.GetData()) != 0 {
		token := string(registerRsp.GetData())
		node.SetOutToken(token)
		node.SetReportHeartBeatTime(time.Now().Unix())
	}
}

func (s *EndNodeServer) heartbeatToEndNode(node *cluster.Node, wg *sync.WaitGroup, ch chan struct{}) {
	defer func() {
		wg.Done()
		<-ch
	}()
	conn, err := node.GetGrpcClientConn()
	if err != nil {
		log.Error("heartbeat to end node failed",
			"err", fmt.Sprintf("did not connect > %v", err),
			"dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
		)
		return
	}

	c := proto.NewEndNodeAccessClient(conn)
	nodeAuthInfo := &proto.NodeAuthInfo{
		Token: node.GetOutToken(),
		Node:  &localNode.Node,
	}
	heartBeatMsg := &proto.HeartBeatReq{
		TimestampUs: time.Now().UnixMicro(),
	}
	if s.userMgr.IsClusterEnabled() {
		localDigests := s.userMgr.ListDigests()
		digests := make([]*proto.UserDigest, 0, len(localDigests))
		for _, d := range localDigests {
			digests = append(digests, &proto.UserDigest{
				Username:    d.Username,
				UpdatedAtUs: d.UpdatedAtUs,
				OriginNode:  d.OriginNode,
				Hash:        d.Hash,
			})
		}
		heartBeatMsg.UserDigests = digests
	}
	var heartBeatReq interface{} = heartBeatMsg
	reqData, _ := pb.Marshal(heartBeatReq.(pb.Message))
	rsp, err := rpcClient.ReqHeartBeat(rpcClient.NewContext(), reqData, c, nodeAuthInfo, s.clusterState.GetClusterToken())
	if err != nil {
		node.SetOutToken("")
		log.Error("heartbeat to end node failed",
			"err", fmt.Sprintf("heartbeat to end node failed > %v", err),
			"dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
		)
		return
	}
	heartBeatRsp, ok := rsp.(*proto.HeartBeatRsp)
	if !ok || heartBeatRsp == nil {
		node.SetOutToken("")
		log.Error("heartbeat to end node failed", "err", "unexpected nil response",
			"dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
		)
		return
	}
	if heartBeatRsp.GetCode() != 0 {
		node.SetOutToken("")
		node.SetReportHeartBeatTime(time.Now().Unix())
		log.Error("heartbeat to end node failed",
			"err", heartBeatRsp.GetMsg(), "dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
		)
	} else {
		node.SetReportHeartBeatTime(time.Now().Unix())
		addRemoteNode(heartBeatRsp, s, "End")

		// Push full user payloads to the remote node for any users it requested.
		if s.userMgr.IsClusterEnabled() {
			if needUsers := heartBeatRsp.GetNeedClusterUsers(); len(needUsers) > 0 {
				toSend := make([]*proto.ClusterUserSync, 0, len(needUsers))
				for _, username := range needUsers {
					u := s.userMgr.GetUserForSync(username)
					if u == nil {
						continue
					}
					toSend = append(toSend, userToProtoClusterUserSync(u))
				}
				if len(toSend) > 0 {
					upsertData, _ := pb.Marshal(&proto.UpsertClusterUsersReq{Users: toSend})
					if _, err := rpcClient.ReqUpsertClusterUsers(rpcClient.NewContext(), upsertData, c, nodeAuthInfo, s.clusterState.GetClusterToken()); err != nil {
						log.Error("heartbeat: push cluster users failed", "dst_name", node.Name, "count", len(toSend), "err", err)
					} else {
						log.Debug("heartbeat: pushed cluster users", "dst_name", node.Name, "requested", len(needUsers), "pushed", len(toSend))
					}
				}
			}
		}
	}
}

func (s *EndNodeServer) registerOrHeartBeatToEndNode() {
	ch := make(chan struct{}, 10)
	wg := sync.WaitGroup{}
	for _, node := range s.clusterState.GetAllNode() {
		if node.Name == s.Name {
			continue
		}
		if node.IsLocal() {
			node.SetReportHeartBeatTime(time.Now().Unix())
		}
		if !node.IsValid() {
			// Skip only this invalid node; using return here (finding #7)
			// aborted the whole round, so a single stale peer silently
			// starved every subsequent node of heartbeat/registration.
			log.Info("skip heartbeat to invalid node",
				"dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
			)
			continue
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
	if s.centerNode.Host == "" || s.centerNode.Port <= 1000 {
		return
	}
	conn, err := s.centerNode.GetGrpcClientConn()
	if err != nil {
		log.Error("heartbeat to center node failed",
			"err", fmt.Sprintf("did not connect > %v", err),
			"center_host", s.centerNode.Host,
		)
		return
	}
	c := proto.NewCenterNodeAccessClient(conn)
	heartBeatReq := &proto.HeartBeatReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token: "",
			Node:  &localNode.Node,
		},
		TimestampUs: time.Now().UnixMicro(),
	}
	rsp, err := c.HeartBeat(rpcClient.NewContext(), heartBeatReq)
	if err != nil {
		log.Error("heartbeat to center node failed",
			"err", fmt.Sprintf("heartbeat failed > %v", err),
			"center_host", s.centerNode.Host, "center_port", s.centerNode.Port,
		)
	} else {
		addRemoteNode(rsp, s, "Center")
	}
}

func addRemoteNode(rsp *proto.HeartBeatRsp, s *EndNodeServer, remoteServerType string) {
	for key, remoteNode := range rsp.NodesMap {
		remoteNodeName := remoteNode.GetName()
		if node := s.clusterState.Get(key); node == nil && remoteNode.Name != localNode.Name {
			if wrongNode := s.clusterState.GetNodeFromWrongNodeList(remoteNodeName); wrongNode != nil {
				log.Debug("skip add wrong node",
					"remote_server_type", remoteServerType,
					"node_host", remoteNode.GetHost(), "node_port", remoteNode.GetPort(), "node_name", remoteNode.GetName(),
				)
				continue
			}
			log.Info("add node from remote",
				"remote_server_type", remoteServerType,
				"node_host", remoteNode.GetHost(), "node_port", remoteNode.GetPort(), "node_name", remoteNode.GetName(),
			)
			s.clusterState.Add(&cluster.Node{
				Node:       remoteNode,
				CreateTime: time.Now().Unix(),
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Proto <-> contracts.User conversion helpers
// ---------------------------------------------------------------------------

// userToProtoClusterUserSync converts a contracts.User to proto.ClusterUserSync.
func userToProtoClusterUserSync(u *contracts.User) *proto.ClusterUserSync {
	if u == nil {
		return nil
	}
	return &proto.ClusterUserSync{
		User:        userToProtoUser(u),
		Deleted:     u.IsDeleting(),
		UpdatedAtUs: u.UpdatedAtUs,
		OriginNode:  u.OriginNode,
		Hash:        u.Hash,
	}
}

// userToProtoUser converts a contracts.User to proto.User.
func userToProtoUser(u *contracts.User) *proto.User {
	if u == nil {
		return nil
	}
	var expire int64
	if !u.ExpiryTime.IsZero() {
		expire = u.ExpiryTime.Unix()
	}
	role := u.Role
	if role == "" {
		role = "normal"
	}
	return &proto.User{
		Name:                  u.Username,
		AuthToken:             u.AuthToken,
		ExpireTime:            expire,
		Role:                  role,
		Group:                 u.TargetGroup,
		UploadBps:             u.BandwidthUploadBps,
		DownloadBps:           u.BandwidthDownloadBps,
		MaxClients:            int32(u.MaxClients),
		ClientRecycleDelaySec: int32(u.ClientRecycleDelaySec),
		ClientDrainSec:        int32(u.ClientDrainSec),
		Uplink:                u.TrafficTotalUplink,
		Downlink:              u.TrafficTotalDownlink,
		LoginPasswordHash:     u.LoginPassword,
	}
}

// protoClusterUserSyncToUser converts a proto.ClusterUserSync to contracts.User.
func protoClusterUserSyncToUser(p *proto.ClusterUserSync) *contracts.User {
	if p == nil {
		return nil
	}
	pu := p.GetUser()
	if pu == nil {
		return nil
	}
	u := &contracts.User{}
	u.Username = pu.GetName()
	u.AuthToken = pu.GetAuthToken()
	if pu.GetExpireTime() > 0 {
		u.ExpiryTime = time.Unix(pu.GetExpireTime(), 0)
	}
	u.Role = pu.GetRole()
	u.TargetGroup = pu.GetGroup()
	if p.GetDeleted() {
		u.MarkDeleting()
	}
	u.UpdatedAtUs = p.GetUpdatedAtUs()
	u.OriginNode = p.GetOriginNode()
	u.Hash = p.GetHash()
	u.BandwidthUploadBps = pu.GetUploadBps()
	u.BandwidthDownloadBps = pu.GetDownloadBps()
	u.MaxClients = int(pu.GetMaxClients())
	u.ClientRecycleDelaySec = int(pu.GetClientRecycleDelaySec())
	u.ClientDrainSec = int(pu.GetClientDrainSec())
	u.LoginPassword = pu.GetLoginPasswordHash()
	return u
}

func (s *EndNodeServer) heartBeatAndRegisterToNodeOrCenterNode() {
	log.Info("start heartbeat to center and end node or register to end node")
	defer log.Info("heartbeat and register exit")
	ticker := time.NewTicker(heartbeatInterval)
	for {
		s.heartbeatToCenterNode()
		s.registerOrHeartBeatToEndNode()
		<-ticker.C
	}
}
