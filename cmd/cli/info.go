package cli

import (
	"sync"
	"time"

	"github.com/lureiny/v2raymg/cmd/cli/client"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// info

var (
	localNodeList = map[string]*cluster.Node{}
	nodeMutex     = sync.Mutex{}

	localUserList = map[string][]*proto.User{}
	userMutex     = sync.Mutex{}

	// localInboundList is keyed by node name, value is the list of inbounds on that node.
	localInboundList = map[string][]*proto.InboundInfo{}
	inboundMutex     = sync.Mutex{}
)

var updateCycle = 5 * time.Second

func updateLocalNodeList() {
	nodeMutex.Lock()
	defer nodeMutex.Unlock()
	localNodeList, _ = client.ListNode(getHost(), getToken())
}

func getNode(nodeName string) *cluster.Node {
	return localNodeList[nodeName]
}

func updateLocalUserList() {
	userMutex.Lock()
	defer userMutex.Unlock()
	localUserList, _ = client.ListUser(getHost(), getToken(), "all")
}

func updateLocalInboundList() {
	inboundMutex.Lock()
	defer inboundMutex.Unlock()
	result, err := client.ListInboundsStructured(getHost(), getToken(), "all")
	if err != nil || result == nil {
		return
	}
	localInboundList = result
}
