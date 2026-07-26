package cluster_test

import (
	"sync"
	"testing"

	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// TestNode_ConcurrentFieldAccess drives one shared *Node from writer goroutines
// (mirroring inbound-RPC heartbeat writes and the heartbeat goroutine's
// token/report writes) and reader goroutines (mirroring HTTP fan-out and the
// filter loop). Without per-Node locking, -race reports the unsynchronized
// int64/string field access; with the accessor locks the run is clean.
func TestNode_ConcurrentFieldAccess(t *testing.T) {
	n := &cluster.Node{Node: &proto.Node{Name: "n1", Host: "10.0.0.1", Port: 2000}}
	const iters = 2000
	var wg sync.WaitGroup

	// writer: inbound RPC refreshes received-heartbeat time
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			n.SetRecvHeartBeatTime(int64(i))
		}
	}()
	// writer: heartbeat goroutine sets/clears out-token and report time
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			if i%2 == 0 {
				n.SetOutToken("tok")
			} else {
				n.SetOutToken("")
			}
			n.SetReportHeartBeatTime(int64(i))
		}
	}()
	// readers: predicates used by fan-out / filter
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				_ = n.RegisteredRemote()
				_ = n.IsValid()
				_ = n.IsCompleteRegister()
				_ = n.RegisteredLocal()
				_ = n.GetOutToken()
			}
		}()
	}
	wg.Wait()
}

// TestNode_GetGrpcClientConn_NoRealloc asserts the client connection is created
// once and reused. grpc.Dial is non-blocking, so a fresh conn starts IDLE (never
// Ready); the old "reconnect when != Ready" logic re-Dialed on virtually every
// call and never Closed the orphan, leaking a connection each time. The fixed
// version reuses any non-Shutdown conn.
func TestNode_GetGrpcClientConn_NoRealloc(t *testing.T) {
	n := &cluster.Node{Node: &proto.Node{Name: "n1", Host: "127.0.0.1", Port: 1}}
	c1, err := n.GetGrpcClientConn()
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer c1.Close()
	c2, err := n.GetGrpcClientConn()
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	if c1 != c2 {
		t.Fatal("GetGrpcClientConn re-dialed instead of reusing the connection (connection leak)")
	}
}

// TestNode_GetGrpcClientConn_Concurrent runs many concurrent GetGrpcClientConn
// calls on one node; -race flags the old unsynchronized grpcClientConn pointer
// write, and the single-flight lock keeps it clean.
func TestNode_GetGrpcClientConn_Concurrent(t *testing.T) {
	n := &cluster.Node{Node: &proto.Node{Name: "n1", Host: "127.0.0.1", Port: 1}}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if conn, err := n.GetGrpcClientConn(); err == nil && conn != nil {
					_ = conn.GetState()
				}
			}
		}()
	}
	wg.Wait()
	if c, _ := n.GetGrpcClientConn(); c != nil {
		_ = c.Close()
	}
}
