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
		{Name: "peer1", Host: "10.0.0.1", Port: 3000}, // same host+port → filtered
		{Name: "peer2", Host: "10.0.0.2", Port: 3000}, // different → loaded
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

func TestNodeManager_LoadStaticNode_FiltersSameName(t *testing.T) {
	localNode := cluster.GetLocalNode()
	localNode.Node = proto.Node{
		Name: "local",
		Host: "10.0.0.1",
		Port: 3000,
	}

	c := newTestCluster("c1", "tok")
	nodes := []cluster.StaticNode{
		{Name: "local", Host: "10.0.0.2", Port: 4000}, // same name → filtered
	}
	if err := c.LoadStaticNode(nodes); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	if n, _ := c.FindByName("local"); n != nil {
		t.Error("expected same-name node filtered")
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
		{"valid peer", cluster.StaticNode{Name: "peer", Host: "10.0.0.2", Port: 4000}, true},
		{"same host+port", cluster.StaticNode{Name: "peer", Host: "10.0.0.1", Port: 3000}, false},
		{"same name", cluster.StaticNode{Name: "local", Host: "10.0.0.2", Port: 4000}, false},
		{"empty host", cluster.StaticNode{Name: "peer", Host: "", Port: 4000}, false},
		{"low port", cluster.StaticNode{Name: "peer", Host: "10.0.0.2", Port: 999}, false},
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
