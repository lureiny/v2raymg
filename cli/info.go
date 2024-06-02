package main

import (
	"sync"
	"time"

	"github.com/lureiny/v2raymg/cli/client"
	"github.com/lureiny/v2raymg/cluster"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

// info

var (
	localNodeList = map[string]*cluster.Node{}
	nodeMutex     = sync.Mutex{}

	localUserList = map[string][]*proto.User{}
	userMutex     = sync.Mutex{}
)

var updateCycle = 5 * time.Second

func updateLocalNodeList() {
	nodeMutex.Lock()
	defer nodeMutex.Unlock()
	localNodeList, _ = client.ListNode(getHost(), getToken())

}

func updateLocalUserList() {
	userMutex.Lock()
	defer userMutex.Unlock()
	localUserList, _ = client.ListUser(getHost(), getToken(), "all")
}
