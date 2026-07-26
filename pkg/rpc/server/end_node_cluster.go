package server

import (
	"bytes"
	context "context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	commonrpc "github.com/lureiny/v2raymg/pkg/common/rpc"
	rpcClient "github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/log"
	grpc "google.golang.org/grpc"
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
	// Node-directory delta sync. This response used to carry the full node map on
	// every tick, which is O(N) bytes per heartbeat and therefore O(N^2) per node
	// per round. Now the digest goes out every time and the full map only when it
	// can actually be needed:
	//
	//   1. reconcile heartbeat (request carries nodes) — the peer already found a
	//      mismatch; merge what it sent and answer with our full map so the single
	//      extra round trip converges BOTH directions;
	//   2. legacy peer (request carries no nodes_sum) — it cannot compare digests,
	//      so keep doing exactly what we did before or it never learns new nodes;
	//   3. steady state — digest only.
	//
	// The digest is never used here to decide a mismatch: reconciliation is driven
	// entirely by the client, so this stays a single mechanism.
	//
	// Map and digest come from one snapshot: the map we return must be exactly the
	// set the digest was computed over, or the peer merges a set that does not
	// match the sum it was told and burns an extra reconcile round every tick.
	advertised, nodesSum := s.clusterState.GetAdvertisedNodes()
	if !s.cfg.Cluster.NodeSumSync {
		// Kill switch: withhold the digest AND always answer with the full map, so
		// peers classify us as legacy and never reconcile against us either. That
		// makes this a complete rollback rather than a half-disabled state, and it
		// is safe to flip one node at a time.
		heartBeatRsp.NodesMap = advertised
	} else {
		heartBeatRsp.NodesSum = nodesSum
		switch {
		case len(heartBeatReq.GetNodes()) > 0:
			// Merging cannot grow the advertised set (freshly learned nodes have no
			// heartbeat timestamps yet, so IsCompleteRegister is false for them), so
			// the snapshot above stays accurate for this response.
			mergeRemoteNodes(heartBeatReq.GetNodes(), s, "EndReconcile")
			heartBeatRsp.NodesMap = advertised
		case len(heartBeatReq.GetNodesSum()) == 0:
			heartBeatRsp.NodesMap = advertised
		}
	}

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
	nodeAuthInfo := rpcClient.NewNodeAuthInfo(s.clusterState.GetClusterToken(), &localNode.Node, node.GetName())
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

// heartbeatToEndNode sends one heartbeat to a peer. advertised/advertisedSum are
// this round's node-directory snapshot, computed once by the caller and shared by
// every peer in the round — the advertised set does not depend on the peer, and
// the map and its digest must stay paired (we advertise the digest now and may
// push the very same map a moment later).
func (s *EndNodeServer) heartbeatToEndNode(node *cluster.Node, wg *sync.WaitGroup, ch chan struct{},
	advertised map[string]*proto.Node, advertisedSum []byte) {
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
	nodeAuthInfo := rpcClient.NewNodeAuthInfo(node.GetOutToken(), &localNode.Node, node.GetName())
	heartBeatMsg := &proto.HeartBeatReq{
		TimestampUs: time.Now().UnixMicro(),
		// Digest of our advertised node set. Also the capability signal: a peer
		// that receives no sum treats us as legacy and keeps returning the full
		// directory, so an unupgraded fleet member still converges.
		NodesSum: advertisedSum,
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
		// Empty in steady state; populated when the peer is answering a reconcile
		// heartbeat or still treats us as legacy.
		mergeRemoteNodes(heartBeatRsp.GetNodesMap(), s, "End")
		s.reconcileNodesWithPeer(node, c, heartBeatRsp.GetNodesSum(), advertised, advertisedSum)

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
					// Fresh NodeAuthInfo (new nonce) — reusing the heartbeat's
					// would look like a replay and be rejected by the server.
					upsertAuth := rpcClient.NewNodeAuthInfo(node.GetOutToken(), &localNode.Node, node.GetName())
					if _, err := rpcClient.ReqUpsertClusterUsers(rpcClient.NewContext(), upsertData, c, upsertAuth, s.clusterState.GetClusterToken()); err != nil {
						log.Error("heartbeat: push cluster users failed", "dst_name", node.Name, "count", len(toSend), "err", err)
					} else {
						log.Debug("heartbeat: pushed cluster users", "dst_name", node.Name, "requested", len(needUsers), "pushed", len(toSend))
					}
				}
			}
		}
	}
}

// reconcileNodesWithPeer performs the node-directory reconcile for one peer when
// the digests disagree: it immediately re-sends the heartbeat with our full node
// map attached. The peer merges it and answers with its own full map, so a single
// extra round trip converges both directions.
//
// Doing this in the same round rather than flagging the peer and pushing on the
// next tick is what keeps the mechanism stateless — no per-peer flag has to live
// on cluster.Node (where every mutable field must go through the mutex and
// survive the peer being reclaimed and rebuilt by filter() between rounds). It
// also mirrors the cluster-user catch-up directly above: same shape, one extra
// authenticated call issued right after the heartbeat that revealed the gap.
//
// The reconcile response is merged but never compared again, so a peer costs at
// most two heartbeats per round and a permanent disagreement cannot self-sustain
// a loop.
func (s *EndNodeServer) reconcileNodesWithPeer(node *cluster.Node, c proto.EndNodeAccessClient,
	peerSum []byte, advertised map[string]*proto.Node, advertisedSum []byte) {
	if !s.cfg.Cluster.NodeSumSync {
		return
	}
	if len(peerSum) == 0 {
		// Legacy peer: it cannot compute a digest and has already returned its
		// full directory, which the caller merged. Nothing left to reconcile.
		return
	}
	if bytes.Equal(peerSum, advertisedSum) {
		node.ResetNodesSumMismatch()
		return
	}

	// Damping. A disagreement normally clears in one round, so a streak means the
	// gap cannot be closed by exchanging directories — the peer advertises a node
	// we refuse to merge (wrong-token list), or vice versa. Reconciling every
	// round then costs two full directories per round indefinitely, which is
	// worse than the unconditional full map this replaced. After a few rounds,
	// drop to roughly one attempt per minute; correctness is unaffected because
	// the digest is only ever a hint.
	if streak := node.BumpNodesSumMismatch(); streak > nodesReconcileStreakLimit &&
		streak%nodesReconcileBackoffRounds != 0 {
		log.Debug("heartbeat: node directory still diverged, backing off",
			"dst_name", node.Name, "streak", streak)
		return
	}

	// user_digests is deliberately left empty: this call exists only to settle the
	// node directory, and re-sending the per-user digest list would double that
	// payload on exactly the ticks that are already the expensive ones. The server
	// reads an empty digest list as "nothing to compare" and returns no
	// need_cluster_users, so the user-sync path is unaffected.
	reconcileMsg := &proto.HeartBeatReq{
		TimestampUs: time.Now().UnixMicro(),
		NodesSum:    advertisedSum,
		Nodes:       advertised,
	}
	reqData, err := pb.Marshal(reconcileMsg)
	if err != nil {
		log.Error("heartbeat: marshal node reconcile failed", "dst_name", node.Name, "err", err)
		return
	}
	// Fresh NodeAuthInfo (new nonce) — reusing the heartbeat's would be a
	// duplicate nonce and checkReplay would drop this call as a replay.
	reconcileAuth := rpcClient.NewNodeAuthInfo(node.GetOutToken(), &localNode.Node, node.GetName())
	rsp, err := rpcClient.ReqHeartBeat(rpcClient.NewContext(), reqData, c, reconcileAuth, s.clusterState.GetClusterToken())
	if err != nil {
		// The peer answered the heartbeat a moment ago, so a failure here is not
		// evidence that it is down: log and let the next round retry rather than
		// invalidating the token.
		log.Error("heartbeat: node reconcile failed", "dst_name", node.Name, "err", err)
		return
	}
	reconcileRsp, ok := rsp.(*proto.HeartBeatRsp)
	if !ok || reconcileRsp == nil {
		log.Error("heartbeat: node reconcile failed", "dst_name", node.Name, "err", "unexpected nil response")
		return
	}
	if reconcileRsp.GetCode() != 0 {
		log.Error("heartbeat: node reconcile rejected", "dst_name", node.Name, "err", reconcileRsp.GetMsg())
		return
	}
	mergeRemoteNodes(reconcileRsp.GetNodesMap(), s, "EndReconcile")
	log.Debug("heartbeat: reconciled node directory", "dst_name", node.Name,
		"pushed", len(advertised), "received", len(reconcileRsp.GetNodesMap()))
}

func (s *EndNodeServer) registerOrHeartBeatToEndNode() {
	ch := make(chan struct{}, 10)
	wg := sync.WaitGroup{}
	// One node-directory snapshot for the whole round: the advertised set does not
	// vary by peer, and every peer must be told a digest that matches the map we
	// would push it moments later.
	advertised, advertisedSum := s.clusterState.GetAdvertisedNodes()
	if !s.cfg.Cluster.NodeSumSync {
		// Kill switch: withhold the digest so peers classify us as legacy and keep
		// answering with their full directory. Combined with the server-side
		// branch this restores the pre-optimisation behaviour in both directions,
		// and it is safe to flip on one node at a time.
		advertisedSum = nil
	}
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
			go s.heartbeatToEndNode(node, &wg, ch, advertised, advertisedSum)
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
	// Authenticate to the center with the cluster token (was empty = anyone
	// could forge a heartbeat). The center validates it against its configured
	// per-cluster tokens. dest_node is "" since the center has no node name
	// known to the end. When CenterToken is configured the whole exchange is
	// wrapped in an AES envelope keyed by that token (separate from the cluster
	// token), so the node directory and the inner cluster token are hidden from
	// on-path observers; the center still authenticates membership via the inner
	// cluster token. Empty CenterToken keeps the channel plaintext (legacy).
	heartBeatReq := &proto.HeartBeatReq{
		NodeAuthInfo: rpcClient.NewNodeAuthInfo(s.clusterState.GetClusterToken(), &localNode.Node, ""),
	}
	var centerOpts []grpc.CallOption
	if ct := s.cfg.Cluster.CenterToken; ct != "" {
		centerOpts = append(centerOpts, grpc.ForceCodec(commonrpc.NewEncryptMessageCodec(ct)))
	}
	rsp, err := c.HeartBeat(rpcClient.NewContext(), heartBeatReq, centerOpts...)
	if err != nil {
		log.Error("heartbeat to center node failed",
			"err", fmt.Sprintf("heartbeat failed > %v", err),
			"center_host", s.centerNode.Host, "center_port", s.centerNode.Port,
		)
	} else {
		// The center path is unchanged by the end<->end delta sync: the center
		// still returns its full directory on every heartbeat.
		mergeRemoteNodes(rsp.GetNodesMap(), s, "Center")
	}
}

// mergeRemoteNodes folds a peer-supplied node directory into the local cluster
// view. It takes the raw map rather than a HeartBeatRsp because there are now two
// sources: the heartbeat response (a peer answering our request) and the request
// of a reconcile heartbeat (a peer pushing its view to us). Both arrive from an
// authenticated cluster member and get identical treatment.
//
// The merge is add-only and never rewrites an existing entry: a node whose meta
// info changed is rejected by AuthRemoteNode's Compare and ages out of the
// advertised set within NodeTimeOut instead.
//
// Entries are validated before being trusted. Until this change nothing checked
// them, because the only source was a peer's own response; the reconcile path
// lets a member write into our directory through the request as well, so both
// paths now require a structurally complete node in our own cluster. Rejections
// log at Warn — a legitimate peer never sends one, so silence is the expected
// steady state.
func mergeRemoteNodes(nodes map[string]*proto.Node, s *EndNodeServer, remoteServerType string) {
	for key, remoteNode := range nodes {
		remoteNodeName := remoteNode.GetName()
		if node := s.clusterState.Get(key); node == nil && remoteNode.Name != localNode.Name {
			if wrongNode := s.clusterState.GetNodeFromWrongNodeList(remoteNodeName); wrongNode != nil {
				log.Debug("skip add wrong node",
					"remote_server_type", remoteServerType,
					"node_host", remoteNode.GetHost(), "node_port", remoteNode.GetPort(), "node_name", remoteNode.GetName(),
				)
				continue
			}
			candidate := &cluster.Node{
				Node:       remoteNode,
				CreateTime: time.Now().Unix(),
			}
			if !candidate.IsComplete() {
				log.Warn("skip incomplete node from remote",
					"remote_server_type", remoteServerType,
					"node_host", remoteNode.GetHost(), "node_port", remoteNode.GetPort(),
					"node_name", remoteNode.GetName(), "node_cluster", remoteNode.GetClusterName(),
				)
				continue
			}
			// RegisterNode gates on IsSameCluster, so every node that entered the
			// directory the normal way carries our cluster name; a peer-supplied
			// entry that does not is either a bug or cross-cluster contamination.
			if localNode.ClusterName != "" && remoteNode.GetClusterName() != localNode.ClusterName {
				log.Warn("skip node from remote with foreign cluster",
					"remote_server_type", remoteServerType,
					"node_name", remoteNode.GetName(),
					"node_cluster", remoteNode.GetClusterName(), "local_cluster", localNode.ClusterName,
				)
				continue
			}
			log.Info("add node from remote",
				"remote_server_type", remoteServerType,
				"node_host", remoteNode.GetHost(), "node_port", remoteNode.GetPort(), "node_name", remoteNode.GetName(),
			)
			s.clusterState.Add(candidate)
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
	ticker := time.NewTicker(s.heartbeatInterval())
	for {
		s.heartbeatToCenterNode()
		s.registerOrHeartBeatToEndNode()
		<-ticker.C
	}
}
