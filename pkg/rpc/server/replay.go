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
// the drift window, a destination-node binding (so a frame captured for another
// node cannot be replayed here), and a non-duplicate nonce. selfName is this
// server's node name; an empty dest_node is accepted for backward tolerance
// within a coordinated upgrade but a mismatched non-empty dest is rejected.
func checkReplay(req interface{}, selfName string) error {
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

	if dest := ai.GetDestNode(); dest != "" && selfName != "" && dest != selfName {
		return fmt.Errorf("request destined for %q, not this node %q", dest, selfName)
	}

	if len(ai.GetNonce()) == 0 {
		return fmt.Errorf("missing request nonce")
	}
	return serverNonceCache.seenOrAdd(string(ai.GetNonce()), nowUs, int64(heartbeatMaxDriftUs))
}
