package client

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// TestReqToMultiEndNodeServer_ConcurrentResultWrites closes the test gap flagged
// by finding TEST-GAPS-#46: the concurrent fan-out orchestrator
// ReqToMultiEndNodeServer had no -race coverage, even though every goroutine
// writes the shared succList/failedList maps.
//
// It registers many nodes pointing at a fast-refusing address so each goroutine
// reaches the mutex-guarded failedList write without needing a live server, then
// asserts every node produced exactly one result. Under `go test -race` this
// exercises the concurrent map writes; removing the lock around succList/
// failedList makes the detector fire (verified while authoring), so the test has
// teeth against a future regression that drops the synchronization.
func TestReqToMultiEndNodeServer_ConcurrentResultWrites(t *testing.T) {
	const n = 40
	now := time.Now().Unix()
	nodes := make([]*cluster.Node, 0, n)
	for i := 0; i < n; i++ {
		nd := &cluster.Node{
			Node: &proto.Node{
				Name: fmt.Sprintf("node-%d", i),
				Host: "127.0.0.1",
				Port: 1, // nothing listens on :1 → fast connection refused
			},
			CreateTime: now,
		}
		// RegisteredRemote() == true requires a non-empty out-token and a recent
		// reported-heartbeat, so the fan-out does not skip the node.
		nd.SetOutToken("tok")
		nd.SetReportHeartBeatTime(now)
		nodes = append(nodes, nd)
	}

	local := &cluster.LocalNode{
		Node:  proto.Node{Name: "local", Host: "127.0.0.1", Port: 2},
		Token: "tok",
	}
	c := NewEndNodeClient(nodes, local)
	if c == nil {
		t.Fatal("NewEndNodeClient returned nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	succ, failed, err := c.ReqToMultiEndNodeServer(ctx, AddUsersReqType, &proto.UserOpReq{}, "tok")
	if err != nil {
		t.Fatalf("fan-out returned a top-level error: %v", err)
	}
	// No server is listening, so every node lands in failedList; the invariant
	// under test is that all N concurrent writes are accounted for exactly once.
	if got := len(succ) + len(failed); got != n {
		t.Errorf("expected %d results across succ+failed, got %d (succ=%d failed=%d)", n, got, len(succ), len(failed))
	}
}
