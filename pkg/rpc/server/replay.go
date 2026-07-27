package server

import (
	"fmt"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// authInfoCarrier is satisfied by every cluster RPC request (all carry a
// NodeAuthInfo), so the interceptor can validate anti-replay fields generically.
type authInfoCarrier interface {
	GetNodeAuthInfo() *proto.NodeAuthInfo
}

// nonceCache remembers recently-seen request nonces to reject duplicates within
// the accepted timestamp window. Entries older than 2x the window are swept.
type nonceCache struct {
	mu       sync.Mutex
	seen     map[string]int64 // nonce -> first-seen unix micros
	lastSwept int64
}

func newNonceCache() *nonceCache {
	return &nonceCache{seen: make(map[string]int64)}
}

// seenOrAdd records the nonce and returns an error if it was already seen within
// the window (a replay). windowUs is the retention/acceptance window.
func (c *nonceCache) seenOrAdd(nonce string, nowUs, windowUs int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if nowUs-c.lastSwept > windowUs {
		for k, ts := range c.seen {
			if nowUs-ts > 2*windowUs {
				delete(c.seen, k)
			}
		}
		c.lastSwept = nowUs
	}
	if _, dup := c.seen[nonce]; dup {
		return fmt.Errorf("replayed request (duplicate nonce)")
	}
	c.seen[nonce] = nowUs
	return nil
}

// serverNonceCache is a process-wide cache shared by the end/center interceptors.
var serverNonceCache = newNonceCache()

// checkReplay validates a request's anti-replay fields: a fresh timestamp within
// the drift window, a destination binding (so a frame captured for another node
// cannot be replayed here), and a non-duplicate nonce.
//
// The destination is checked against this node's identity first and its name
// second. Both are tolerant of an empty value on the wire — a sender that has
// not learned our id yet, or a peer older than the field — but a mismatched
// non-empty value is rejected. The id check is the one that still holds when two
// nodes are misconfigured with the same name, which the identity-keyed directory
// permits rather than refusing outright.
// isRegister exempts RegisterNode from the identity binding. That call is
// precisely the one that REPAIRS a stale identity: a peer whose database was
// rebuilt comes back with a new id, so everyone still holding the old one
// addresses it by that old id. Enforcing the binding here would reject the
// repair at the interceptor, the response carrying the responder's real identity
// would never come back, and the tombstone path could never fire — the mechanism
// would be unreachable in exactly the scenario it was written for.
//
// The exemption is narrow: RegisterNodeReq carries no payload beyond the auth
// info, the request is still encrypted under the cluster token, and the nonce,
// timestamp, dest_node name and dest_method bindings all still apply.
func checkReplay(req interface{}, selfName, selfID string, isRegister bool) error {
	carrier, ok := req.(authInfoCarrier)
	if !ok || carrier.GetNodeAuthInfo() == nil {
		return fmt.Errorf("missing node auth info")
	}
	ai := carrier.GetNodeAuthInfo()

	nowUs := time.Now().UnixMicro()
	ts := ai.GetTimestampUs()
	if ts == 0 {
		return fmt.Errorf("missing request timestamp")
	}
	drift := nowUs - ts
	if drift < 0 {
		drift = -drift
	}
	if drift > heartbeatMaxDriftUs {
		return fmt.Errorf("request timestamp out of window (drift %dms)", drift/1000)
	}

	if destID := ai.GetDestNodeId(); !isRegister && destID != "" && selfID != "" && destID != selfID {
		return fmt.Errorf("request destined for node_id %q, not this node %q", destID, selfID)
	}
	if dest := ai.GetDestNode(); dest != "" && selfName != "" && dest != selfName {
		return fmt.Errorf("request destined for %q, not this node %q", dest, selfName)
	}

	if len(ai.GetNonce()) == 0 {
		return fmt.Errorf("missing request nonce")
	}
	return serverNonceCache.seenOrAdd(string(ai.GetNonce()), nowUs, int64(heartbeatMaxDriftUs))
}
