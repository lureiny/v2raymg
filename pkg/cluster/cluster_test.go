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
			Name:        name,
			Host:        host,
			Port:        port,
			ClusterName: clusterName,
		},
	}
}

func TestNode_IsValid_RecentHeartbeat(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.GetHeartBeatTime = time.Now().Unix()
	if !n.IsValid() {
		t.Error("expected valid with recent GetHeartBeatTime")
	}
}

func TestNode_IsValid_RecentReport(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.ReportHeartBeatTime = time.Now().Unix()
	if !n.IsValid() {
		t.Error("expected valid with recent ReportHeartBeatTime")
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
	n.GetHeartBeatTime = time.Now().Unix()
	n.ReportHeartBeatTime = time.Now().Unix()
	if !n.IsCompleteRegister() {
		t.Error("expected complete register")
	}
}

func TestNode_IsCompleteRegister_OnlyOne(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.GetHeartBeatTime = time.Now().Unix()
	if n.IsCompleteRegister() {
		t.Error("expected incomplete register with only GetHeartBeatTime")
	}
}

func TestNode_RegisteredLocal(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.InToken = "tok"
	n.GetHeartBeatTime = time.Now().Unix()
	if !n.RegisteredLocal() {
		t.Error("expected RegisteredLocal true")
	}
}

func TestNode_RegisteredLocal_NoToken(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.GetHeartBeatTime = time.Now().Unix()
	if n.RegisteredLocal() {
		t.Error("expected RegisteredLocal false without InToken")
	}
}

func TestNode_RegisteredRemote(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.OutToken = "tok"
	n.ReportHeartBeatTime = time.Now().Unix()
	if !n.RegisteredRemote() {
		t.Error("expected RegisteredRemote true")
	}
}

func TestNode_RegisteredRemote_Expired(t *testing.T) {
	n := makeNode("n1", "10.0.0.1", 2000, "c1")
	n.OutToken = "tok"
	n.ReportHeartBeatTime = 0
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

func TestCluster_IsSameCluster_Match(t *testing.T) {
	c := newTestCluster("mycluster", "mytoken")
	if err := c.IsSameCluster("mycluster", "mytoken"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCluster_IsSameCluster_WrongName(t *testing.T) {
	c := newTestCluster("mycluster", "mytoken")
	err := c.IsSameCluster("other", "mytoken")
	if err == nil {
		t.Error("expected error for wrong cluster name")
	}
}

func TestCluster_IsSameCluster_WrongToken(t *testing.T) {
	c := newTestCluster("mycluster", "mytoken")
	err := c.IsSameCluster("mycluster", "badtoken")
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
	local.InToken = "correct-token"
	local.CreateTime = time.Now().Unix()
	c.Add(local)

	remote := makeNode("n1", "10.0.0.1", 2000, "c1")
	remote.InToken = "wrong-token"
	err := c.AuthRemoteNode(&remote)
	if err == nil {
		t.Error("expected error for wrong token")
	}
}

func TestCluster_AuthRemoteNode_Success(t *testing.T) {
	c := newTestCluster("c1", "tok")
	local := makeNode("n1", "10.0.0.1", 2000, "c1")
	local.InToken = "correct-token"
	local.GetHeartBeatTime = time.Now().Unix()
	local.CreateTime = time.Now().Unix()
	c.Add(local)

	remote := makeNode("n1", "10.0.0.1", 2000, "c1")
	remote.InToken = "correct-token"
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
	local.InToken = "tok"
	local.GetHeartBeatTime = 1 // long expired
	local.CreateTime = time.Now().Unix()
	c.Add(local)

	remote := makeNode("n1", "10.0.0.1", 2000, "c1")
	remote.InToken = "tok"
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
		Name:        "local",
		Host:        "10.0.0.1",
		Port:        3000,
		ClusterName: "c1",
	}

	c := newTestCluster("c1", "tok")
	nodes := []cluster.StaticNode{
		{Name: "peer1", Host: "10.0.0.1", Port: 3000}, // same host+port → filtered
		{Name: "peer2", Host: "10.0.0.2", Port: 3000}, // different → loaded
	}
	if err := c.LoadStaticNode(nodes); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	if c.HaveNode("peer1") {
		t.Error("expected peer1 filtered (same host+port)")
	}
	if !c.HaveNode("peer2") {
		t.Error("expected peer2 loaded")
	}
}

func TestNodeManager_LoadStaticNode_FiltersSameName(t *testing.T) {
	localNode := cluster.GetLocalNode()
	localNode.Node = proto.Node{
		Name:        "local",
		Host:        "10.0.0.1",
		Port:        3000,
		ClusterName: "c1",
	}

	c := newTestCluster("c1", "tok")
	nodes := []cluster.StaticNode{
		{Name: "local", Host: "10.0.0.2", Port: 4000}, // same name → filtered
	}
	if err := c.LoadStaticNode(nodes); err != nil {
		t.Fatalf("LoadStaticNode: %v", err)
	}
	if c.HaveNode("local") {
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
		ClusterName:  "testcluster",
		ClusterToken: "testtoken",
		StaticNodes:  nil,
	}
	nodeCfg := cluster.NodeInitConfig{
		Name: "node1",
		Host: "10.0.0.1",
		Port: 5000,
	}

	mgr, localNode, err := cluster.NewEndNodeClusterManagerFromConfig(clusterCfg, nodeCfg)
	if err != nil {
		t.Fatalf("NewEndNodeClusterManagerFromConfig: %v", err)
	}
	if mgr.Name != "testcluster" {
		t.Errorf("cluster name: got %q, want %q", mgr.Name, "testcluster")
	}
	if mgr.Token != "testtoken" {
		t.Errorf("cluster token: got %q, want %q", mgr.Token, "testtoken")
	}
	if localNode.Token == "" {
		t.Error("expected localNode.Token to be set")
	}
	if localNode.Name != "node1" {
		t.Errorf("localNode.Name: got %q, want %q", localNode.Name, "node1")
	}
	// Local node should be registered and permanently valid
	n := mgr.Get("node1")
	if n == nil {
		t.Fatal("expected local node registered in cluster")
	}
	if n.GetHeartBeatTime != math.MaxInt64-cluster.NodeTimeOut {
		t.Errorf("expected permanent heartbeat, got %d", n.GetHeartBeatTime)
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
