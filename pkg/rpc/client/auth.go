package client

import (
	"crypto/rand"
	"io"
	"time"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// NewNodeAuthInfo builds a per-call NodeAuthInfo stamped for anti-replay: the
// cluster token, the local node identity, a fresh timestamp, a random nonce, and
// the destination node name (bind so a captured frame can't be replayed to a
// different node).
//
// IMPORTANT: call this once PER RPC — never reuse a NodeAuthInfo across two
// calls, or the second would carry a duplicate nonce and be rejected as a replay.
// destNode may be "" when the destination has no known node name (e.g. the
// center heartbeat).
func NewNodeAuthInfo(token string, node *proto.Node, destNode string) *proto.NodeAuthInfo {
	nonce := make([]byte, 16)
	_, _ = io.ReadFull(rand.Reader, nonce)
	return &proto.NodeAuthInfo{
		Token:       token,
		Node:        node,
		TimestampUs: time.Now().UnixMicro(),
		Nonce:       nonce,
		DestNode:    destNode,
	}
}
