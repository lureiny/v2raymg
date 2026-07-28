package server

import (
	"bytes"
	context "context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	usync "github.com/lureiny/v2raymg/pkg/proxy/usermanager/sync"
	rpcClient "github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	pb "google.golang.org/protobuf/proto"
)

// RegisterNode files a peer into the local directory and hands back the token it
// must present from then on.
//
// The peer is the authority on its own name, address and identity, so a
// registration that disagrees with what we hold is applied, not refused. The
// pre-2.8 code rejected any mismatch of the host+port+name triple (code 105),
// which meant a node whose config had been edited could not rejoin until its
// stale entry aged out — and could not age out at all while the cluster could
// still reach the old address. Refusal is structurally unfixable here: the party
// that would have to withdraw the stale claim is the instance that no longer
// exists. What the old code bought — noticing a takeover — is now delivered by
// reporting it (see the outcome logging below) rather than by blocking it.
//
// Two things are still refused, because both are unresolvable config errors that
// silently corrupt addressing rather than transient disagreements:
// a peer presenting OUR identity (a copied data directory) and a peer presenting
// OUR name.
func (s *EndNodeServer) RegisterNode(ctx context.Context, registerNodeReq *proto.RegisterNodeReq) (*proto.RegisterNodeRsp, error) {
	registerNodeRsp := &proto.RegisterNodeRsp{
		ResponderNodeId: localNode.Node.GetNodeId(),
		ResponderName:   s.Name,
	}
	clusterToken := registerNodeReq.GetNodeAuthInfo().GetToken()
	node := registerNodeReq.GetNodeAuthInfo().GetNode()

	if node.GetHost() == "" || node.GetPort() <= 0 || node.GetName() == "" {
		errMsg := "empty host, empty name or invalid port"
		log.Error("register node failed", "err", errMsg,
			"src_host", node.GetHost(), "src_port", node.GetPort(), "src_name", node.GetName())
		registerNodeRsp.Code = 100
		registerNodeRsp.Msg = errMsg
		return registerNodeRsp, nil
	}
	if id := node.GetNodeId(); id != "" && id == localNode.Node.GetNodeId() {
		errMsg := "remote node presents this node's identity; a data directory was copied"
		log.Error("register node failed", "err", errMsg,
			"src_host", node.GetHost(), "src_port", node.GetPort(), "src_name", node.GetName(),
			"node_id", id)
		registerNodeRsp.Code = 103
		registerNodeRsp.Msg = errMsg
		return registerNodeRsp, nil
	}
	if node.GetName() == s.Name {
		errMsg := "remote node has same name with local node"
		log.Error("register node failed", "err", errMsg,
			"src_host", node.GetHost(), "src_port", node.GetPort())
		registerNodeRsp.Code = 103
		registerNodeRsp.Msg = errMsg
		return registerNodeRsp, nil
	}

	if err := s.clusterState.IsSameCluster(clusterToken); err != nil {
		errMsg := err.Error()
		log.Info("cluster mismatch",
			"err", errMsg,
			"src_host", node.GetHost(), "src_port", node.GetPort(),
			"src_name", node.GetName(),
		)
		s.clusterState.AddToWrongNodeList(&cluster.Node{
			Node:       node,
			CreateTime: time.Now().Unix(),
		})
		registerNodeRsp.Msg = errMsg
		registerNodeRsp.Code = 101
		return registerNodeRsp, nil
	}

	s.clusterState.DeleteFromWrongTokenNodeList(node.GetName())

	res := s.clusterState.ResolveRegistration(node, uuid.New().String(), time.Now().Unix())
	// Outside the directory lock: a stale connection is pinned to the address it
	// was dialled with, so it has to go, but closing it under the lock would
	// block every other directory reader on a network operation.
	for _, conn := range res.RetiredConns {
		_ = conn.Close()
	}
	s.reportResolve(res, node)

	registerNodeRsp.Data = []byte(res.Token)
	if res.Repeat {
		// Preserved verbatim for peers that predate this change: they treat 102
		// as "already registered, reuse the token in Data".
		registerNodeRsp.Code = 102
		registerNodeRsp.Msg = "repeated register"
	}
	return registerNodeRsp, nil
}

// reportResolve turns a registration outcome into operator-visible signal. This
// is where "a node was silently replaced" stops being silent: acceptance is
// never in question, but every identity or address change is announced.
func (s *EndNodeServer) reportResolve(res cluster.ResolveResult, claim *proto.Node) {
	switch res.Outcome {
	case cluster.ResolveNew:
		log.Info("register node new",
			"src_host", claim.GetHost(), "src_port", claim.GetPort(),
			"src_name", claim.GetName(), "node_id", claim.GetNodeId())
	case cluster.ResolveMoved:
		log.Warn("node address changed",
			"src_name", claim.GetName(), "node_id", claim.GetNodeId(),
			"prev_host", res.PrevHost, "prev_port", res.PrevPort,
			"new_host", claim.GetHost(), "new_port", claim.GetPort())
	case cluster.ResolveRenamed:
		log.Warn("node renamed",
			"node_id", claim.GetNodeId(),
			"prev_name", res.PrevName, "new_name", claim.GetName())
	case cluster.ResolveReplaced:
		log.Warn("node identity replaced",
			"src_name", claim.GetName(),
			"src_host", claim.GetHost(), "src_port", claim.GetPort(),
			"prev_node_id", res.PrevNodeID, "new_node_id", claim.GetNodeId())
	}
	if len(res.Absorbed) > 0 {
		log.Info("absorbed stale entries at the same address",
			"src_name", claim.GetName(),
			"src_host", claim.GetHost(), "src_port", claim.GetPort(),
			"absorbed", res.Absorbed)
	}
	if len(res.LiveAddrRivals) > 0 {
		log.Error("two live nodes claim the same address",
			"src_name", claim.GetName(),
			"host", claim.GetHost(), "port", claim.GetPort(),
			"rivals", res.LiveAddrRivals)
	}
}

func (s *EndNodeServer) HeartBeat(ctx context.Context, heartBeatReq *proto.HeartBeatReq) (*proto.HeartBeatRsp, error) {
	heartBeatRsp := &proto.HeartBeatRsp{
		// Answered on every heartbeat this handler produces, including its error
		// paths: a caller holding a stale address needs to learn who actually lives
		// there even — especially — when we are rejecting it. Requests turned away
		// by the interceptor never reach here, so authRemoteNode stamps the same
		// two fields onto the response it synthesises.
		ResponderNodeId: localNode.Node.GetNodeId(),
		ResponderName:   s.Name,
	}

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

	// Resolved through LookupPeer (id, then address, then name) rather than by
	// name alone, so a peer that was renamed or moved still finds its own entry.
	claim := heartBeatReq.GetNodeAuthInfo().GetNode()
	node := s.clusterState.LookupPeer(claim)
	if node == nil {
		heartBeatRsp.Code = 104
		heartBeatRsp.Msg = "node has been drop"
		log.Error("heartbeat failed", "err", fmt.Sprintf("node[%s] has been drop", claim.GetName()))
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
	nodeAuthInfo := rpcClient.NewNodeAuthInfo(s.clusterState.GetClusterToken(), &localNode.Node, destBinding(node))
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
	node, ok = s.assertResponder(node, registerRsp.GetResponderNodeId(), registerRsp.GetResponderName())
	if !ok {
		return
	}
	if registerRsp.GetCode() != 0 {
		errMsg := registerRsp.GetMsg()
		// Which failures justify forgetting the peer, and which are about US.
		// The old blanket rule (drop and blacklist on any code but 102) turned a
		// local misconfiguration into cluster-wide amnesia: an empty proxy_host
		// makes every peer answer 100, so one round wiped and blacklisted the
		// entire directory.
		switch registerRsp.GetCode() {
		case 101:
			// Wrong cluster token: the peer genuinely does not belong here.
			if !node.IsLocal() {
				s.clusterState.DeleteNode(node)
				s.clusterState.AddToWrongNodeList(node)
			}
		case 103:
			// The peer says we collide with it on name or identity. Unfixable
			// without operator action, so stop dialling it, but do NOT blacklist:
			// the wrong-token list is keyed by name, and this peer's name is ours.
			if !node.IsLocal() {
				s.clusterState.DeleteNode(node)
			}
			log.Error("register to end node rejected as a local config conflict",
				"err", errMsg, "dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name)
		case 100:
			log.Error("peer rejected our own address as invalid; check end_node.proxy_host and end_node.name",
				"err", errMsg, "dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name)
		default:
			// Includes 105 from a pre-2.8 peer, which still refuses a changed
			// address. Keep retrying; its stale entry ages out within NodeTimeOut.
			log.Warn("register to end node rejected, will retry",
				"code", registerRsp.GetCode(), "err", errMsg,
				"dst_host", node.Host, "dst_port", node.Port, "dst_name", node.Name)
		}
	}
	if len(registerRsp.GetData()) != 0 {
		token := string(registerRsp.GetData())
		node.SetOutToken(token)
		node.SetReportHeartBeatTime(time.Now().Unix())
	}
}

// destBinding returns what we can honestly assert about a peer for the
// anti-replay destination binding.
//
// A static seed carries a name an operator typed into static_nodes and that
// nothing has ever confirmed. Binding a request to it makes checkReplay on the
// far side reject the call outright whenever that label is wrong — so a single
// mistyped seed name used to be permanently unusable in the outbound direction,
// and the ghost entry it left behind could only be cleaned up if the peer
// happened to register to us first. Sending no binding for an unconfirmed seed
// is the honest answer: we genuinely do not know who is at that address yet, and
// the first response tells us (see assertResponder).
//
// Every other entry's name came from the peer's own registration, so it is
// asserted normally. The nonce, timestamp and method bindings are unaffected
// either way.
func destBinding(node *cluster.Node) *proto.Node {
	if node.IsLocal() && node.GetNodeId() == "" {
		return nil
	}
	return node.Node
}

// assertResponder checks that the node answering at an entry's address is the
// node that entry claims to be, and reports whether the caller may keep using it.
//
// Nothing in a response used to say who produced it, so a directory entry could
// point at an address that had been taken over — by a rebuilt node with a fresh
// identity, or by an unrelated cluster member that inherited the IP — and the
// caller would keep registering to it happily. That is what kept the stale
// entry's reportHeartBeatTime fresh, which kept IsValid true, which meant the
// entry was never reclaimed: the one shape of this problem that does not
// self-heal.
//
// On a mismatch the stale id is tombstoned (which also drops the entry), so
// gossip cannot reintroduce it before the rest of the cluster notices.
//
// Returns the entry the caller must go on using. Adopting an identity REPLACES
// the directory entry, so a caller that kept writing to the pointer it started
// the round with would write the session it just negotiated into an object no
// longer in the directory — losing it, and costing a whole extra round on every
// static seed at startup.
func (s *EndNodeServer) assertResponder(node *cluster.Node, responderID, responderName string) (*cluster.Node, bool) {
	if responderID == "" {
		return node, true // pre-2.8 peer: nothing to check against
	}
	// A seed that turns out to be THIS node. static_nodes is a list of addresses
	// an operator typed, and nothing stops one of them being our own — under an
	// alias hostname, or simply a second way of spelling our own proxy_host, so
	// the host+port check at load time misses it. Left alone, the entry adopts
	// our own identity below and we heartbeat ourselves forever, because the
	// heartbeat round skips only the entry flagged isSelf.
	//
	// This is also what replaces the old name-based self-filter in
	// StaticNode.IsValide: asking who answered is authoritative, whereas
	// comparing a configured label only ever caught the one spelling of the
	// mistake that happened to match.
	if responderID == localNode.Node.GetNodeId() {
		log.Warn("a configured peer address resolves to this node itself; dropping it — "+
			"check end_node.cluster.static_nodes",
			"dst_host", node.GetHost(), "dst_port", node.GetPort(),
			"node_id", responderID)
		s.clusterState.DeleteNode(node)
		return node, false
	}

	known := node.GetNodeId()
	if known == "" {
		// A static seed or a replayed database row identifying itself for the
		// first time. Adopt it now so it is not re-learned as a second entry.
		if adopted, changed := s.clusterState.AdoptIdentity(node.GetHost(), node.GetPort(), responderID, responderName); changed {
			log.Info("resolved provisional peer identity",
				"dst_host", node.GetHost(), "dst_port", node.GetPort(),
				"configured_name", node.GetName(), "reported_name", responderName,
				"node_id", responderID)
			return adopted, true
		}
		return node, true
	}
	if known == responderID {
		// Same node. Its NAME may still have drifted: a rename reaches a peer
		// through that peer's own re-registration, so anyone who only ever
		// learned this node by gossip would keep the old label indefinitely and
		// ?target=<name> would resolve on some nodes but not others. Every
		// response carries the current name, so close the gap here — only when it
		// actually differs, since this replaces the directory entry.
		if responderName != "" && responderName != node.GetName() {
			if renamed, changed := s.clusterState.RefreshName(responderID, responderName); changed {
				log.Info("peer reported a new name",
					"node_id", responderID, "prev_name", node.GetName(), "new_name", responderName,
					"dst_host", node.GetHost(), "dst_port", node.GetPort())
				return renamed, true
			}
		}
		return node, true
	}
	log.Warn("directory entry is stale: its address is answered by a different node",
		"dst_host", node.GetHost(), "dst_port", node.GetPort(),
		"expected_name", node.GetName(), "expected_node_id", known,
		"actual_name", responderName, "actual_node_id", responderID)
	s.clusterState.MarkDirty(known, time.Now().Unix())
	return node, false
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
	nodeAuthInfo := rpcClient.NewNodeAuthInfo(node.GetOutToken(), &localNode.Node, destBinding(node))
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
	node, ok = s.assertResponder(node, heartBeatRsp.GetResponderNodeId(), heartBeatRsp.GetResponderName())
	if !ok {
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
					upsertAuth := rpcClient.NewNodeAuthInfo(node.GetOutToken(), &localNode.Node, destBinding(node))
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
	reconcileAuth := rpcClient.NewNodeAuthInfo(node.GetOutToken(), &localNode.Node, destBinding(node))
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
		// Skip by identity, not by name: a peer misconfigured with our name is
		// still a different node and must keep being contacted, and our own entry
		// is the only one flagged isSelf.
		if node.IsSelf() {
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

// mergeRemoteNodes folds a peer-supplied node directory into the local cluster
// view. It takes the raw map rather than a HeartBeatRsp because there are now two
// sources: the heartbeat response (a peer answering our request) and the request
// of a reconcile heartbeat (a peer pushing its view to us). Both arrive from an
// authenticated cluster member and get identical treatment.
//
// The merge is add-only and never rewrites an existing entry. Gossip is a hint
// about who exists, not evidence about where they live: only a node itself, over
// an authenticated connection, may change its own address or name (see
// RegisterNode). A peer that reconfigured is therefore learned from its own
// registration, not from a third party repeating what it used to know.
//
// Identity comes from the message body, never from the map key. The key used to
// be the lookup while the insert was keyed by node.Name, so a map whose key
// disagreed with its body slipped past the "already known?" guard and then
// overwrote an unrelated entry wholesale — tokens, heartbeat timestamps and
// address included. Honest peers always agree (the advertised map is name-keyed
// for exactly this reason), so a disagreement is discarded and reported.
//
// Entries are validated before being trusted. Until this change nothing checked
// them, because the only source was a peer's own response; the reconcile path
// lets a member write into our directory through the request as well, so both
// paths now require a structurally complete node in our own cluster. Rejections
// log at Warn — a legitimate peer never sends one, so silence is the expected
// steady state.
func mergeRemoteNodes(nodes map[string]*proto.Node, s *EndNodeServer, remoteServerType string) {
	now := time.Now().Unix()
	for key, remoteNode := range nodes {
		if remoteNode == nil {
			continue
		}
		remoteNodeName := remoteNode.GetName()
		if key != remoteNodeName {
			log.Warn("skip node whose directory key disagrees with its body",
				"remote_server_type", remoteServerType,
				"key", key, "node_name", remoteNodeName,
				"node_host", remoteNode.GetHost(), "node_port", remoteNode.GetPort(),
			)
			continue
		}
		// Never learn ourselves from someone else's view of us.
		if remoteNodeName == localNode.Name ||
			(remoteNode.GetNodeId() != "" && remoteNode.GetNodeId() == localNode.Node.GetNodeId()) {
			continue
		}
		// A tombstoned id is one we watched get superseded at its own address.
		// Peers that have not noticed yet keep advertising it; without this the
		// add-only merge would reinsert it with a fresh CreateTime every round
		// and it would never age out.
		if s.clusterState.IsDirty(remoteNode.GetNodeId(), now) {
			log.Debug("skip tombstoned node from remote",
				"remote_server_type", remoteServerType,
				"node_id", remoteNode.GetNodeId(), "node_name", remoteNodeName,
			)
			continue
		}
		if wrongNode := s.clusterState.GetNodeFromWrongNodeList(remoteNodeName); wrongNode != nil {
			log.Debug("skip add wrong node",
				"remote_server_type", remoteServerType,
				"node_host", remoteNode.GetHost(), "node_port", remoteNode.GetPort(), "node_name", remoteNodeName,
			)
			continue
		}
		candidate := &cluster.Node{
			Node:       remoteNode,
			CreateTime: now,
		}
		if !candidate.IsComplete() {
			log.Warn("skip incomplete node from remote",
				"remote_server_type", remoteServerType,
				"node_host", remoteNode.GetHost(), "node_port", remoteNode.GetPort(),
				"node_name", remoteNodeName,
			)
			continue
		}
		// Add-only, and the check has to be atomic with the insert: a Lookup
		// followed by an Add is two lock acquisitions, and a registration landing
		// in between would be overwritten by a token-less gossip copy — silently
		// destroying the session of a peer that had just completed its handshake.
		if !s.clusterState.AddIfAbsent(candidate) {
			continue
		}
		log.Info("add node from remote",
			"remote_server_type", remoteServerType,
			"node_host", remoteNode.GetHost(), "node_port", remoteNode.GetPort(),
			"node_name", remoteNodeName, "node_id", remoteNode.GetNodeId(),
		)
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

func (s *EndNodeServer) heartBeatAndRegisterToEndNode() {
	log.Info("start heartbeat/registration loop to end nodes")
	defer log.Info("heartbeat and register exit")
	ticker := time.NewTicker(s.heartbeatInterval())
	for {
		s.registerOrHeartBeatToEndNode()
		<-ticker.C
	}
}
