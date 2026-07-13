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
// (Host/Port/Name/ClusterName), CreateTime and isLocal are written only at
// construction (before the node is published via NodeManager.Add, which
// establishes a happens-before edge) and are treated as read-only afterwards.
//
// mu is a leaf lock: methods holding it only touch this node's own fields and
// never call back into NodeManager/ClusterManager. Compound predicates take a
// single RLock and read the fields directly — they must NOT call the locked
// accessors, since sync.RWMutex is not reentrant.
type Node struct {
	*proto.Node
	CreateTime int64
	isLocal    bool // 是否为从本地文件中加载的node, 本地节点是为了不使用中心节点的场景而设计的

	mu                  sync.RWMutex
	inToken             string // 远端节点访问本地节点时使用, 用于验证远端节点是否有权限访问本地节点
	outToken            string // 本地节点访问远端节点时使用, 用于验证本地节点是否有权限访问远端节点
	recvHeartBeatTime   int64  // 上次获取该节点心跳的时间 (原 GetHeartBeatTime 字段)
	reportHeartBeatTime int64  // 上次上报到该节点的时间
	grpcClientConn      *grpc.ClientConn
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

// 比较两个node是否相同, 相同返回true
func (n1 *Node) Compare(n2 *Node) bool {
	return n1.Host == n2.Host && n1.Port == n2.Port && n1.ClusterName == n2.ClusterName && n1.Name == n2.Name
}

// 比较cluster node与proto node是否相同, 相同返回true
func (n1 *Node) CompareWithProtoNode(n2 *proto.Node) bool {
	return n1.Host == n2.Host && n1.Port == n2.Port && n1.ClusterName == n2.ClusterName && n1.Name == n2.Name
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

func (n *Node) IsLocal() bool {
	return n.isLocal
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

func (node *Node) IsComplete() bool {
	return node.Host != "" && node.Port > 1000 && node.ClusterName != "" && node.Name != ""
}

type StaticNode struct {
	Name string `json:"name,omitempty"`
	Host string `json:"host,omitempty"`
	Port int32  `json:"port,omitempty"`
}

// IsValide 判断静态节点是否有效.
// 过滤条件：host+port 与本地节点完全相同（同一个实例），或 name 相同。
func (sn *StaticNode) IsValide(node *Node) bool {
	sameInstance := sn.Host == node.Host && sn.Port == node.Port
	sameName := sn.Name == node.Name
	return sn.Host != "" && sn.Port > 1000 && !sameInstance && !sameName
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
