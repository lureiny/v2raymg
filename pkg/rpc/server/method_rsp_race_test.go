package server

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// stubClusterState implements ClusterState for authRemoteNode tests.
// AuthRemoteNode always fails with a message carrying the requesting node's
// name, forcing authRemoteNode into the reflect-write failure branch.
type stubClusterState struct{}

func (stubClusterState) GetClusterToken() string    { return "" }
func (stubClusterState) IsSameCluster(string) error { return nil }
func (stubClusterState) AuthRemoteNode(node **cluster.Node) error {
	return fmt.Errorf("denied %s", (*node).GetName())
}
func (stubClusterState) Get(string) *cluster.Node             { return nil }
func (stubClusterState) LookupPeer(*proto.Node) *cluster.Node { return nil }
func (stubClusterState) MarkDirty(string, int64)              {}
func (stubClusterState) IsDirty(string, int64) bool           { return false }
func (stubClusterState) Add(*cluster.Node)                    {}
func (stubClusterState) AddIfAbsent(*cluster.Node) bool       { return true }
func (stubClusterState) Delete(string)                        {}
func (stubClusterState) DeleteNode(*cluster.Node)             {}
func (stubClusterState) GetAllNode() map[string]*cluster.Node { return nil }
func (stubClusterState) ResolveRegistration(*proto.Node, string, int64) cluster.ResolveResult {
	return cluster.ResolveResult{}
}
func (stubClusterState) AdoptIdentity(string, int32, string, string) (*cluster.Node, bool) {
	return nil, false
}
func (stubClusterState) RefreshName(string, string) (*cluster.Node, bool) {
	return nil, false
}
func (stubClusterState) GetProtoNodesWithFilter(cluster.NodeFilter) map[string]*proto.Node {
	return nil
}
func (stubClusterState) GetAdvertisedNodes() (map[string]*proto.Node, []byte) {
	return nil, nil
}
func (stubClusterState) Filter(cluster.NodeFilter)                     {}
func (stubClusterState) AddToWrongNodeList(*cluster.Node)              {}
func (stubClusterState) DeleteFromWrongTokenNodeList(string)           {}
func (stubClusterState) GetNodeFromWrongNodeList(string) *cluster.Node { return nil }

// TestNewEmptyRsp_DistinctInstances proves newEmptyRsp mints a fresh response
// per call instead of handing out the shared methodRspMap prototype (which
// concurrent requests would clobber).
func TestNewEmptyRsp_DistinctInstances(t *testing.T) {
	const method = "/proto.EndNodeAccess/GetUsers"
	a, _ := newEmptyRsp(method)
	b, _ := newEmptyRsp(method)
	if a == nil || b == nil {
		t.Fatal("newEmptyRsp returned nil")
	}
	if a == b {
		t.Fatal("newEmptyRsp returned the shared prototype; concurrent requests would share one struct")
	}
}

// TestAuthRemoteNode_NoCrossTalk runs many concurrent auth failures for the same
// method and asserts each response carries its own request's node name. With the
// old shared-prototype reflect write, concurrent SetString calls raced (-race
// fires) and bled one request's error message into another; the per-call
// reflect.New instance isolates them.
func TestAuthRemoteNode_NoCrossTalk(t *testing.T) {
	// authRemoteNode enters the failure branch only when
	// localNode.Token != req token. Pin the process-wide singleton deterministically.
	saved := localNode.Token
	localNode.Token = "local-secret"
	defer func() { localNode.Token = saved }()

	s := &EndNodeServer{clusterState: stubClusterState{}}

	const method = "/proto.EndNodeAccess/GetUsers"
	const n = 100
	var wg sync.WaitGroup
	errs := make(chan string, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("node-%d", i)
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			req := &proto.GetUsersReq{
				NodeAuthInfo: &proto.NodeAuthInfo{
					Node:  &proto.Node{Name: name},
					Token: "remote-bad-token",
				},
			}
			ok, rsp, _ := s.authRemoteNode(req, method)
			if ok {
				errs <- "expected auth failure but got success"
				return
			}
			r, _ := rsp.(*proto.GetUsersRsp)
			if r == nil {
				errs <- "nil or wrong-typed response"
				return
			}
			if r.GetCode() != 400 {
				errs <- fmt.Sprintf("code=%d, want 400", r.GetCode())
			}
			if !strings.Contains(r.GetMsg(), name) {
				errs <- fmt.Sprintf("response msg %q missing own node name %q (cross-request bleed)", r.GetMsg(), name)
			}
		}(name)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
