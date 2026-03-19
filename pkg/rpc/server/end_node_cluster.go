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
			token = n.InToken
		} else {
			token = uuid.New().String()
			n.InToken = token
			log.Info("register node update",
				"src_host", node.Host, "src_port", node.Port,
				"src_name", nodeName, "cluster", node.ClusterName,
			)
		}
		n.GetHeartBeatTime = time.Now().Unix()
	} else {
		token = uuid.New().String()
		newNode := &cluster.Node{
			Node:                node,
			InToken:             token,
			OutToken:            "",
			GetHeartBeatTime:    time.Now().Unix(),
			CreateTime:          time.Now().Unix(),
			ReportHeartBeatTime: 0,
		}
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
	registerRsp := rsp.(*proto.RegisterNodeRsp)
	errMsg := ""
	if err != nil {
		errMsg = fmt.Sprintf("register to end node failed > %v", err)
	} else if registerRsp.GetCode() != 0 {
		errMsg = registerRsp.GetMsg()
	}
	if errMsg != "" {
		if !node.IsLocal() && (registerRsp.GetCode() > 0 && registerRsp.GetCode() != 102) && err == nil {
			s.clusterState.Delete(node.GetName())
			s.clusterState.AddToWrongNodeList(node)
		}
		log.Error("register to end node failed",
			"err", errMsg, "dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
		)
	}
	if len(registerRsp.GetData()) != 0 {
		token := string(registerRsp.GetData())
		node.OutToken = token
		node.ReportHeartBeatTime = time.Now().Unix()
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
		Token: node.OutToken,
		Node:  &localNode.Node,
	}
	var heartBeatReq interface{} = &proto.HeartBeatReq{}
	reqData, _ := pb.Marshal(heartBeatReq.(pb.Message))
	rsp, err := rpcClient.ReqHeartBeat(rpcClient.NewContext(), reqData, c, nodeAuthInfo, s.clusterState.GetClusterToken())
	heartBeatRsp := rsp.(*proto.HeartBeatRsp)
	if err != nil || heartBeatRsp.GetCode() != 0 {
		errMsg := fmt.Sprintf("heartbeat to end node failed > %v", err)
		if heartBeatRsp.GetCode() != 0 {
			errMsg = heartBeatRsp.GetMsg()
			node.ReportHeartBeatTime = time.Now().Unix()
		}
		node.OutToken = ""
		log.Error("heartbeat to end node failed",
			"err", errMsg, "dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
		)
	} else {
		node.ReportHeartBeatTime = time.Now().Unix()
		addRemoteNode(heartBeatRsp, s, "End")
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
			node.ReportHeartBeatTime = time.Now().Unix()
		}
		if !node.IsValid() {
			log.Info("skip heartbeat to invalid node",
				"dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name,
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
				Node:                remoteNode,
				InToken:             "",
				OutToken:            "",
				GetHeartBeatTime:    0,
				ReportHeartBeatTime: 0,
				CreateTime:          time.Now().Unix(),
			})
		}
	}
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
