package cluster_test

import (
	"math"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// --- Node tests ---

func makeNode(name, host string, port int32, clusterName string) *cluster.Node {
	return &cluster.Node{
		Node: &proto.Node{
			Name: name,
			Host: host,
			Port: port,
		},
	}
}

func TestNode_IsValid_RecentHeartbeat(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.SetRecvHeartBeatTime(time.Now().Unix())
	if !n.IsValid() {
		t.Error("expected valid with recent recvHeartBeatTime")
	}
}

func TestNode_IsValid_RecentReport(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.SetReportHeartBeatTime(time.Now().Unix())
	if !n.IsValid() {
		t.Error("expected valid with recent reportHeartBeatTime")
	}
}

func TestNode_IsValid_RecentCreate(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.CreateTime = time.Now().Unix()
	if !n.IsValid() {
		t.Error("expected valid with recent CreateTime")
	}
}

func TestNode_IsValid_Expired(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	// all timestamps zero → all expired
	if n.IsValid() {
		t.Error("expected invalid with all zero timestamps")
	}
}

func TestNode_IsCompleteRegister_Both(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.SetRecvHeartBeatTime(time.Now().Unix())
	n.SetReportHeartBeatTime(time.Now().Unix())
	if !n.IsCompleteRegister() {
		t.Error("expected complete register")
	}
}

func TestNode_IsCompleteRegister_OnlyOne(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.SetRecvHeartBeatTime(time.Now().Unix())
	if n.IsCompleteRegister() {
		t.Error("expected incomplete register with only recvHeartBeatTime")
	}
}

func TestNode_RegisteredLocal(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.SetInToken("tok")
	n.SetRecvHeartBeatTime(time.Now().Unix())
	if !n.RegisteredLocal() {
		t.Error("expected RegisteredLocal true")
	}
}

func TestNode_RegisteredLocal_NoToken(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.SetRecvHeartBeatTime(time.Now().Unix())
	if n.RegisteredLocal() {
		t.Error("expected RegisteredLocal false without inToken")
	}
}

func TestNode_RegisteredRemote(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.SetOutToken("tok")
	n.SetReportHeartBeatTime(time.Now().Unix())
	if !n.RegisteredRemote() {
		t.Error("expected RegisteredRemote true")
	}
}

func TestNode_RegisteredRemote_Expired(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.SetOutToken("tok")
	n.SetReportHeartBeatTime(0)
	if n.RegisteredRemote() {
		t.Error("expected RegisteredRemote false with expired timestamp")
	}
}

// --- Cluster.IsSameCluster tests ---

func newTestCluster(name, token string) *cluster.Cluster {
	c := &cluster.Cluster{
		Name:  name,
		Token: token,
	}
	c.Init()
	return c
}

// IsSameCluster is token-only since the cluster name was removed: the token is
// also the HKDF input for every end<->end RPC key, so a peer that presents it can
// already read and write the user plane — a second name factor added nothing and
// could only reject legitimate peers over a typo.

func TestCluster_IsSameCluster_Match(t *testing.T) {
	c := newTestCluster("mycluster", "mytoken")
	if err := c.IsSameCluster("mytoken"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCluster_IsSameCluster_WrongToken(t *testing.T) {
	c := newTestCluster("mycluster", "mytoken")
	err := c.IsSameCluster("badtoken")
	if err == nil {
		t.Error("expected error for wrong token")
	}
}

// --- Cluster.AuthRemoteNode tests ---

func TestCluster_AuthRemoteNode_NotExist(t *testing.T) {
	c := newTestCluster("c1", "tok")
	remoteNode := makeNode("unknown", "10.0.0.1", 2000, "c1")
	err := c.AuthRemoteNode(&remoteNode)
	if err == nil {
		t.Error("expected error for non-existent node")
	}
}

func TestCluster_AuthRemoteNode_WrongToken(t *testing.T) {
	c := newTestCluster("c1", "tok")
	local := makeNode("n1", "10.0.0.1", 2000, "c1")
	local.SetInToken("correct-token")
	local.CreateTime = time.Now().Unix()
	c.Add(local)

	remote := makeNode("n1", "10.0.0.1", 2000, "c1")
	remote.SetInToken("wrong-token")
	err := c.AuthRemoteNode(&remote)
	if err == nil {
		t.Error("expected error for wrong token")
	}
}

func TestCluster_AuthRemoteNode_Success(t *testing.T) {
	c := newTestCluster("c1", "tok")
	local := makeNode("n1", "10.0.0.1", 2000, "c1")
	local.SetInToken("correct-token")
	local.SetRecvHeartBeatTime(time.Now().Unix())
	local.CreateTime = time.Now().Unix()
	c.Add(local)

	remote := makeNode("n1", "10.0.0.1", 2000, "c1")
	remote.SetInToken("correct-token")
	err := c.AuthRemoteNode(&remote)
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
	// After auth, remote should point to local node
	if remote != local {
		t.Error("expected remote to be replaced with local node")
	}
}

func TestCluster_AuthRemoteNode_Expired(t *testing.T) {
	c := newTestCluster("c1", "tok")
	local := makeNode("n1", "10.0.0.1", 2000, "c1")
	local.SetInToken("tok")
	local.SetRecvHeartBeatTime(1) // long expired
	local.CreateTime = time.Now().Unix()
	c.Add(local)

	remote := makeNode("n1", "10.0.0.1", 2000, "c1")
	remote.SetInToken("tok")
	err := c.AuthRemoteNode(&remote)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

// --- NodeManager.LoadStaticNode tests ---

func TestNodeManager_LoadStaticNode_FiltersSameHostPort(t *testing.T) {
	// Set up global local node
	localNode := cluster.GetLocalNode()
	localNode.Node = proto.Node{
		Name: "local",
		Host: "10.0.0.1",
		Port: 3000,
	}

	c := newTestCluster("c1", "tok")
	nodes := []cluster.StaticNode{
		{Host: "10.0.0.1", Port: 3000}, // same host+port → filtered
		{Host: "10.0.0.2", Port: 3000}, // different → loaded
	}
	if err := c.LoadStaticNode(nodes); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	// Static peers are filed provisionally, by address: the config supplies a
	// name but nothing has confirmed it, and the name may well be a typo.
	if c.FindByAddr("10.0.0.1", 3000) != nil {
		t.Error("expected peer1 filtered (same host+port)")
	}
	if c.FindByAddr("10.0.0.2", 3000) == nil {
		t.Error("expected peer2 loaded")
	}
	if !c.HaveNode(cluster.ProvisionalKey("10.0.0.2", 3000)) {
		t.Error("expected peer2 filed under its provisional address key")
	}
}

func TestNodeManager_LoadStaticNode_SeedsCarryNoName(t *testing.T) {
	localNode := cluster.GetLocalNode()
	localNode.Node = proto.Node{
		Name: "local",
		Host: "10.0.0.1",
		Port: 3000,
	}

	c := newTestCluster("c1", "tok")
	if err := c.LoadStaticNode([]cluster.StaticNode{{Host: "10.0.0.2", Port: 4000}}); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}

	seed := c.FindByAddr("10.0.0.2", 4000)
	if seed == nil {
		t.Fatal("seed was not loaded")
	}
	// Nothing has told us who is at that address yet, so the entry must not claim
	// to know. Presenting an operator's guess as a name would put it straight into
	// /api/node and the logs as if it were fact.
	if got := seed.GetName(); got != "" {
		t.Errorf("seed name = %q, want empty until the peer reports its own", got)
	}
	if got := seed.GetNodeId(); got != "" {
		t.Errorf("seed node_id = %q, want empty until the peer identifies itself", got)
	}
	if !seed.IsLocal() {
		t.Error("a configured seed must be flagged isLocal so it is never evicted")
	}
}

// --- EndNodeClusterManager tests ---

func TestEndNodeClusterManager_GetClusterToken(t *testing.T) {
	mgr := cluster.NewEndNodeClusterManager()
	mgr.Token = "secret-token"
	if got := mgr.GetClusterToken(); got != "secret-token" {
		t.Errorf("GetClusterToken: got %q, want %q", got, "secret-token")
	}
}

func TestNewEndNodeClusterManagerFromConfig(t *testing.T) {
	clusterCfg := cluster.ClusterInitConfig{
		ClusterToken: "testtoken",
		StaticNodes:  nil,
	}
	nodeCfg := cluster.NodeInitConfig{
		Name: "node1",
		Host: "10.0.0.1",
		Port: 5000,
		ID:   "node1-id",
	}

	mgr, localNode, err := cluster.NewEndNodeClusterManagerFromConfig(clusterCfg, nodeCfg)
	if err != nil {
		t.Fatalf("NewEndNodeClusterManagerFromConfig: %v", err)
	}
	// mgr.Name is no longer derived from config: the cluster name was removed and
	// the token is the only membership identifier.
	if mgr.Token != "testtoken" {
		t.Errorf("cluster token: got %q, want %q", mgr.Token, "testtoken")
	}
	if localNode.Token == "" {
		t.Error("expected localNode.Token to be set")
	}
	if localNode.Name != "node1" {
		t.Errorf("localNode.Name: got %q, want %q", localNode.Name, "node1")
	}
	if localNode.GetNodeId() != "node1-id" {
		t.Errorf("localNode.NodeId: got %q, want %q", localNode.GetNodeId(), "node1-id")
	}
	// The local node is filed under its identity, and every outbound request
	// carries the same *proto.Node, so writing the id once propagates it.
	n := mgr.Get("node1-id")
	if n == nil {
		t.Fatal("expected local node registered in cluster under its node_id")
	}
	if !n.IsSelf() {
		t.Error("local node must be flagged isSelf: address de-duplication and name " +
			"resolution both exempt it")
	}
	if n.GetRecvHeartBeatTime() != math.MaxInt64-cluster.NodeTimeOut {
		t.Errorf("expected permanent heartbeat, got %d", n.GetRecvHeartBeatTime())
	}
}

// --- StaticNode.IsValide tests ---

func TestStaticNode_IsValide(t *testing.T) {
	localNode := &cluster.Node{
		Node: &proto.Node{
			Name: "local",
			Host: "10.0.0.1",
			Port: 3000,
		},
	}

	tests := []struct {
		name   string
		sn     cluster.StaticNode
		expect bool
	}{
		// A seed is an ADDRESS and carries no name, so host+port is the whole test.
		// The old "same name as this node" case is gone with the field: filtering on
		// a name only ever caught the one spelling of the mistake that matched
		// exactly, and self-detection that works happens at runtime by comparing the
		// responder's node_id (see EndNodeServer.assertResponder).
		{"valid peer", cluster.StaticNode{Host: "10.0.0.2", Port: 4000}, true},
		{"same host+port as this node", cluster.StaticNode{Host: "10.0.0.1", Port: 3000}, false},
		{"same host, different port", cluster.StaticNode{Host: "10.0.0.1", Port: 4000}, true},
		{"empty host", cluster.StaticNode{Host: "", Port: 4000}, false},
		{"low port", cluster.StaticNode{Host: "10.0.0.2", Port: 999}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.sn.IsValide(localNode)
			if got != tt.expect {
				t.Errorf("IsValide: got %v, want %v", got, tt.expect)
			}
		})
	}
}
