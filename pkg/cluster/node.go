package cluster

import (
	"fmt"
	"sync"
	"time"

	commonrpc "github.com/lureiny/v2raymg/pkg/common/rpc"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

const NodeTimeOut int64 = 60

// Node wraps a proto.Node with the local cluster's mutable runtime state.
//
// Concurrency: a single *Node is shared (stored by pointer in NodeManager) and
// touched by many goroutines at once — inbound gRPC handlers (auth/register),
// the periodic heartbeat/register goroutines, and HTTP fan-out. The four
// mutable runtime fields below are therefore unexported and guarded by mu;
// all reads/writes must go through the accessor methods so the compiler forces
// every call site through the lock. The embedded *proto.Node identity fields
// (Host/Port/Name/NodeId), CreateTime and isLocal are written only at
// construction (before the node is published via NodeManager.Add, which
// establishes a happens-before edge) and are treated as read-only afterwards.
//
// That contract is why a peer which changed its address or name is handled by
// building a REPLACEMENT node and swapping it into the directory, rather than
// by assigning to Host/Name on the live one: ComputeNodesSum and the gRPC
// marshaller both read those fields with no lock held, off the shared
// *proto.Node that GetAdvertisedNodes hands out. See NodeManager.ResolveRegistration.
//
// mu is a leaf lock: methods holding it only touch this node's own fields and
// never call back into NodeManager/ClusterManager. Compound predicates take a
// single RLock and read the fields directly — they must NOT call the locked
// accessors, since sync.RWMutex is not reentrant.
type Node struct {
	*proto.Node
	CreateTime int64
	isLocal    bool // 是否为从本地文件中加载的node, 本地节点是为了不使用中心节点的场景而设计的
	// isSelf marks THIS process's own directory entry. Address de-duplication and
	// name resolution must never touch it: it is the one entry whose identity is
	// not a claim from the network but a local fact.
	isSelf bool

	mu                  sync.RWMutex
	inToken             string // 远端节点访问本地节点时使用, 用于验证远端节点是否有权限访问本地节点
	outToken            string // 本地节点访问远端节点时使用, 用于验证本地节点是否有权限访问远端节点
	recvHeartBeatTime   int64  // 上次获取该节点心跳的时间 (原 GetHeartBeatTime 字段)
	reportHeartBeatTime int64  // 上次上报到该节点的时间
	grpcClientConn      *grpc.ClientConn
	// nodesSumMismatchRounds counts consecutive heartbeat rounds in which this
	// peer's node-directory digest differed from ours. It exists purely to damp
	// a disagreement that cannot be resolved — e.g. the peer advertises a node
	// that sits in our wrong-token list and can never be merged — where
	// reconciling every round would ship two full directories forever, worse
	// than the unconditional full map this scheme replaced.
	nodesSumMismatchRounds int32
}

// --- accessors for the mu-guarded runtime fields ---

// GetInToken returns the token remote nodes use to access the local node.
func (n *Node) GetInToken() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.inToken
}

// SetInToken sets the in-token. Safe for concurrent use.
func (n *Node) SetInToken(v string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.inToken = v
}

// BumpNodesSumMismatch records one more consecutive round of node-directory
// disagreement with this peer and returns the new streak length.
func (n *Node) BumpNodesSumMismatch() int32 {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.nodesSumMismatchRounds++
	return n.nodesSumMismatchRounds
}

// ResetNodesSumMismatch clears the disagreement streak; called as soon as this
// peer's node-directory digest matches ours again.
func (n *Node) ResetNodesSumMismatch() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.nodesSumMismatchRounds = 0
}

// GetOutToken returns the token the local node uses to access this remote node.
func (n *Node) GetOutToken() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.outToken
}

// SetOutToken sets the out-token. Safe for concurrent use.
func (n *Node) SetOutToken(v string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.outToken = v
}

// GetRecvHeartBeatTime returns the last time a heartbeat was received from this node.
func (n *Node) GetRecvHeartBeatTime() int64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.recvHeartBeatTime
}

// SetRecvHeartBeatTime records the last received-heartbeat time. Safe for concurrent use.
func (n *Node) SetRecvHeartBeatTime(t int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.recvHeartBeatTime = t
}

// GetReportHeartBeatTime returns the last time a heartbeat was reported to this node.
func (n *Node) GetReportHeartBeatTime() int64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.reportHeartBeatTime
}

// SetReportHeartBeatTime records the last reported-heartbeat time. Safe for concurrent use.
func (n *Node) SetReportHeartBeatTime(t int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.reportHeartBeatTime = t
}

// AuthAndTouch verifies inToken and that the received heartbeat has not timed
// out, and on success refreshes recvHeartBeatTime — all atomically under the
// write lock, so the check-then-act cannot race with a concurrent token/heartbeat
// update on the same node.
func (n *Node) AuthAndTouch(inToken string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.inToken != inToken {
		return fmt.Errorf("wrong token")
	}
	if n.recvHeartBeatTime != 0 && n.recvHeartBeatTime+int64(HeartBeatTimeout) < time.Now().Unix() {
		return fmt.Errorf("invalid token, token timeout")
	}
	n.recvHeartBeatTime = time.Now().Unix()
	return nil
}

// Compare reports whether two nodes are the same instance. Identity is
// host+port+name; the cluster name used to be part of it but was removed with
// the field — the cluster token is the only membership boundary that means
// anything, and a node that is registered has already proved it.
func (n1 *Node) Compare(n2 *Node) bool {
	return n1.Host == n2.Host && n1.Port == n2.Port && n1.Name == n2.Name
}

// CompareWithProtoNode is Compare against a wire-level node.
func (n1 *Node) CompareWithProtoNode(n2 *proto.Node) bool {
	return n1.Host == n2.Host && n1.Port == n2.Port && n1.Name == n2.Name
}

// GetGrpcClientConn returns a lazily-established gRPC client connection to this
// node, creating it once on first use. The connection is long-lived and
// self-healing: gRPC drives its own reconnect/backoff, so any state other than
// Shutdown is reused. Only a Shutdown connection (which can arise solely from an
// explicit Close, never taken on the reuse path) is discarded and rebuilt. This
// closes both bugs behind finding #1: the previous "reconnect whenever != Ready"
// logic re-Dialed on nearly every call (a freshly Dial'd conn starts IDLE, not
// Ready) and never Closed the orphaned connection, leaking one conn per call;
// and concurrent callers raced on the grpcClientConn pointer. The write lock
// single-flights dialing and the pointer write.
func (node *Node) GetGrpcClientConn() (*grpc.ClientConn, error) {
	node.mu.Lock()
	defer node.mu.Unlock()
	if node.grpcClientConn != nil && node.grpcClientConn.GetState() != connectivity.Shutdown {
		return node.grpcClientConn, nil
	}
	if node.grpcClientConn != nil {
		_ = node.grpcClientConn.Close()
		node.grpcClientConn = nil
	}
	addr := fmt.Sprintf("%s:%d", node.GetHost(), node.GetPort())
	// StampDestMethod binds every outgoing request to its gRPC method inside the
	// authenticated payload, so an on-path attacker cannot redirect a ciphertext
	// to a sibling method sharing the same request type (finding #2).
	conn, err := grpc.Dial(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(commonrpc.StampDestMethodClientInterceptor),
	)
	if err != nil {
		// Do not poison the field with a nil/failed conn.
		return nil, err
	}
	node.grpcClientConn = conn
	return conn, nil
}

// takeConn detaches this node's gRPC connection and hands it to the caller,
// which becomes responsible for closing it.
//
// Needed because an address change is implemented by REPLACING the directory
// entry, never by writing Host in place. Two reasons that is not optional:
// the identity fields are published read-only (see the type comment) and are
// read unlocked by ComputeNodesSum and by gRPC marshalling the shared
// *proto.Node; and grpc.Dial pins its target at dial time, so a cached
// connection keeps reaching the OLD address no matter what Host says. The
// replacement therefore has to inherit nothing and the stale connection has to
// be closed explicitly — which also fixes a pre-existing leak, since Delete and
// Filter never closed the connections of the nodes they dropped.
//
// Safe to call while holding NodeManager's lock: mu is a leaf.
func (n *Node) takeConn() *grpc.ClientConn {
	n.mu.Lock()
	defer n.mu.Unlock()
	conn := n.grpcClientConn
	n.grpcClientConn = nil
	return conn
}

// CloseConn closes and clears this node's gRPC connection, if any.
func (n *Node) CloseConn() {
	if conn := n.takeConn(); conn != nil {
		_ = conn.Close()
	}
}

// handOffRuntimeTo moves every mutable runtime field to dst, which must not be
// published yet, and transfers ownership of the gRPC connection.
//
// Used when an entry is re-filed under a new key without its address changing —
// learning a provisional peer's real identity. The session (tokens, heartbeat
// timestamps, digest-mismatch streak) belongs to the peer, not to the key it was
// filed under, so losing it would tear down a working registration; and the
// connection is still pointed at the right address, so it is moved rather than
// closed.
//
// Deliberately NOT a general-purpose copy: the caller must not use it to fake
// heartbeat timestamps onto an entry that has not actually completed a
// handshake, since IsCompleteRegister verifies no token and would then admit an
// unauthenticated peer into the advertised set.
func (n *Node) handOffRuntimeTo(dst *Node) {
	n.mu.Lock()
	defer n.mu.Unlock()
	dst.inToken = n.inToken
	dst.outToken = n.outToken
	dst.recvHeartBeatTime = n.recvHeartBeatTime
	dst.reportHeartBeatTime = n.reportHeartBeatTime
	dst.nodesSumMismatchRounds = n.nodesSumMismatchRounds
	dst.grpcClientConn = n.grpcClientConn
	n.grpcClientConn = nil
}

func (n *Node) IsLocal() bool {
	return n.isLocal
}

// IsSelf reports whether this entry is this process's own node.
func (n *Node) IsSelf() bool {
	return n.isSelf
}

// IsValid 有效返回true
func (node *Node) IsValid() bool {
	currentTime := time.Now().Unix()
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.recvHeartBeatTime+NodeTimeOut > currentTime ||
		node.reportHeartBeatTime+NodeTimeOut > currentTime ||
		node.CreateTime+NodeTimeOut > currentTime
}

// IsCompleteRegister 是否完成双向注册
func (node *Node) IsCompleteRegister() bool {
	currentTime := time.Now().Unix()
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.recvHeartBeatTime+NodeTimeOut > currentTime &&
		node.reportHeartBeatTime+NodeTimeOut > currentTime
}

// 本地是否已经在node上注册
func (node *Node) RegisteredRemote() bool {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.outToken != "" && node.reportHeartBeatTime+NodeTimeOut > time.Now().Unix()
}

// 节点node在本地注册过
func (node *Node) RegisteredLocal() bool {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.inToken != "" && node.recvHeartBeatTime+NodeTimeOut > time.Now().Unix()
}

// IsComplete reports whether a node carries enough identity to be usable.
func (node *Node) IsComplete() bool {
	return node.Host != "" && node.Port > 1000 && node.Name != ""
}

// StaticNode is a configured bootstrap address — nothing more.
//
// There is deliberately no Name. A seed says where to dial, and until the peer
// there answers we do not know who it is; carrying a label an operator typed
// would mean presenting an unverified guess as fact in /api/node and in logs.
// The peer reports its real name in its first response, and that is the only
// name the entry ever holds. `name:` in static_nodes config is accepted and
// silently dropped, so existing configs keep working untouched.
type StaticNode struct {
	Host string `json:"host,omitempty"`
	Port int32  `json:"port,omitempty"`
}

// IsValide 判断静态节点是否有效.
// 过滤条件：host+port 与本地节点完全相同（同一个实例），或 name 相同。
// IsValide reports whether a configured seed is worth loading.
//
// There is no name to consult. Filtering on one could not do the job it appeared
// to do anyway: it only caught an operator listing this node under its own exact
// name, and missed the same mistake made under an alias. Self-detection that
// actually works happens at runtime, by comparing the responder's node_id
// against our own (see EndNodeServer.assertResponder); the host+port test here
// is just a cheap first pass.
func (sn *StaticNode) IsValide(node *Node) bool {
	sameInstance := sn.Host == node.Host && sn.Port == node.Port
	return sn.Host != "" && sn.Port > 1000 && !sameInstance
}

var globalLocalNode = &LocalNode{}

type LocalNode struct {
	proto.Node
	Token string // for req local rpc server
}

// GetLocalNode return global local node
func GetLocalNode() *LocalNode {
	return globalLocalNode
}
