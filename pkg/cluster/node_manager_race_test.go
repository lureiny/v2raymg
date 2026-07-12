package cluster_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

func newRaceTestNode(name string) *cluster.Node {
	return &cluster.Node{
		Node: &proto.Node{
			Name: name,
		},
	}
}

// TestNodeManagerConcurrentGetAddDelete 复现读路径(Get/HaveNode)与写路径(Add/Delete)
// 的并发 map 读写: 修复前 -race 必报 DATA RACE, 无 -race 时可能触发
// "fatal error: concurrent map read and map write" 崩溃整个进程。
//
// 并发路径上不读写任何共享 Node 字段, 只验证 map 层安全,
// 避免被 Node 字段级竞态(独立缺陷)污染结果。
func TestNodeManagerConcurrentGetAddDelete(t *testing.T) {
	nm := cluster.NewNodeManager()
	const iters = 2000

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				key := fmt.Sprintf("n%d", i%16)
				if i%2 == 0 {
					nm.Add(key, newRaceTestNode(key))
				} else {
					nm.Delete(key)
				}
			}
		}()
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				key := fmt.Sprintf("n%d", i%16)
				nm.Get(key)
				nm.HaveNode(key)
			}
		}()
	}
	wg.Wait()
}

// TestNodeManagerGetAllNodeConcurrentIterate 复现 GetAllNode 交出内部 map 后
// 锁外 range 与并发 Add/Delete 构成的并发迭代+写: 修复前无 -race 也可能
// "fatal error: concurrent map iteration and map write"(不可恢复), -race 必报。
func TestNodeManagerGetAllNodeConcurrentIterate(t *testing.T) {
	nm := cluster.NewNodeManager()
	for i := 0; i < 16; i++ {
		key := fmt.Sprintf("seed%d", i)
		nm.Add(key, newRaceTestNode(key))
	}

	const iters = 2000
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			for range nm.GetAllNode() {
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			key := fmt.Sprintf("n%d", i%16)
			nm.Add(key, newRaceTestNode(key))
			nm.Delete(key)
		}
	}()
	wg.Wait()
}

// TestNodeManagerFilterConcurrentWithReads 复现 Filter 在写锁内交换 nodes 指针
// 与读路径无锁解引用该指针的竞态: 修复前 -race 必报(指针字段 8 字节竞争,
// 无 -race 时不易崩溃, 因此该用例的回归防护完全依赖 CI 的 -race 运行)。
func TestNodeManagerFilterConcurrentWithReads(t *testing.T) {
	nm := cluster.NewNodeManager()
	const iters = 1000

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			key := fmt.Sprintf("n%d", i%8)
			nm.Add(key, newRaceTestNode(key))
			nm.Filter(func(n *cluster.Node) bool {
				// 只读构造后不再变化的 Name 字段, 不触碰共享可变字段
				return n.Name != "n0"
			})
		}
	}()
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				nm.Get(fmt.Sprintf("n%d", i%8))
				for range nm.GetAllNode() {
				}
			}
		}()
	}
	wg.Wait()
}

// TestNodeManagerGetAllNodeReturnsCopy 固化 GetAllNode 返回浅拷贝快照的语义:
// 调用方修改返回的 map 不得影响内部状态。
func TestNodeManagerGetAllNodeReturnsCopy(t *testing.T) {
	nm := cluster.NewNodeManager()
	nm.Add("a", newRaceTestNode("a"))

	m := nm.GetAllNode()
	delete(m, "a")
	m["b"] = newRaceTestNode("b")

	if nm.Get("a") == nil {
		t.Fatal("GetAllNode 返回的 map 被 delete 后内部节点丢失: 返回的不是副本")
	}
	if nm.Get("b") != nil {
		t.Fatal("向 GetAllNode 返回的 map 写入影响了内部状态: 返回的不是副本")
	}
}
