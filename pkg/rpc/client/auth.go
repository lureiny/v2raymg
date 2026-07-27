package client

import (
	"crypto/rand"
	"io"
	"time"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// NewNodeAuthInfo builds a per-call NodeAuthInfo stamped for anti-replay: the
// cluster token, the local node identity, a fresh timestamp, a random nonce, and
// the destination binding (so a captured frame can't be replayed to a different
// node).
//
// The destination is bound by BOTH its node_id and its name. The id is the real
// binding — names are mutable and, since the directory became identity-keyed, no
// longer guaranteed unique, so a name-only binding would weaken exactly in the
// case where two nodes collide on one. The name is still sent for peers that
// predate node_id and check nothing else.
//
// IMPORTANT: call this once PER RPC — never reuse a NodeAuthInfo across two
// calls, or the second would carry a duplicate nonce and be rejected as a replay.
// dest may be nil, or carry an empty id, when the destination has not identified
// itself yet (a static seed we have never spoken to).
func NewNodeAuthInfo(token string, node *proto.Node, dest *proto.Node) *proto.NodeAuthInfo {
	nonce := make([]byte, 16)
	_, _ = io.ReadFull(rand.Reader, nonce)
	return &proto.NodeAuthInfo{
		Token:       token,
		Node:        node,
		TimestampUs: time.Now().UnixMicro(),
		Nonce:       nonce,
		DestNode:    dest.GetName(),
		DestNodeId:  dest.GetNodeId(),
	}
}
