package server

import (
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// Anti-replay regressions for the cluster RPC interceptor.
//
// The center-node token-auth cases that used to live here went away with the
// center itself: end<->end is now the only cluster channel, and it is
// authenticated by the encryption codec plus AuthRemoteNode rather than by an
// app-layer per-cluster token map.

func authReq(nonce, dest, token string) *proto.HeartBeatReq {
	return &proto.HeartBeatReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token:       token,
			Node:        &proto.Node{Name: "n1"},
			TimestampUs: time.Now().UnixMicro(),
			Nonce:       []byte(nonce),
			DestNode:    dest,
		},
	}
}

func TestCheckReplay_ValidAndDuplicate(t *testing.T) {
	if err := checkReplay(authReq("b1-nonce-valid", "self", "tok"), "self"); err != nil {
		t.Fatalf("valid request should pass: %v", err)
	}
	// Same nonce again = replay.
	if err := checkReplay(authReq("b1-nonce-valid", "self", "tok"), "self"); err == nil {
		t.Fatal("duplicate nonce must be rejected as a replay")
	}
}

func TestCheckReplay_Rejections(t *testing.T) {
	// missing timestamp
	r := authReq("b1-nonce-ts", "self", "tok")
	r.NodeAuthInfo.TimestampUs = 0
	if err := checkReplay(r, "self"); err == nil {
		t.Error("missing timestamp must be rejected")
	}
	// stale timestamp (> 30s)
	r = authReq("b1-nonce-stale", "self", "tok")
	r.NodeAuthInfo.TimestampUs = time.Now().Add(-60 * time.Second).UnixMicro()
	if err := checkReplay(r, "self"); err == nil {
		t.Error("stale timestamp must be rejected")
	}
	// missing nonce
	r = authReq("", "self", "tok")
	if err := checkReplay(r, "self"); err == nil {
		t.Error("missing nonce must be rejected")
	}
	// cross-node replay: dest bound to another node
	if err := checkReplay(authReq("b1-nonce-dest", "other-node", "tok"), "self"); err == nil {
		t.Error("a request destined for another node must be rejected")
	}
}
