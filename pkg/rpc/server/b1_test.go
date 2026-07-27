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
	if err := checkReplay(authReq("b1-nonce-valid", "self", "tok"), "self", "", false); err != nil {
		t.Fatalf("valid request should pass: %v", err)
	}
	// Same nonce again = replay.
	if err := checkReplay(authReq("b1-nonce-valid", "self", "tok"), "self", "", false); err == nil {
		t.Fatal("duplicate nonce must be rejected as a replay")
	}
}

func TestCheckReplay_Rejections(t *testing.T) {
	// missing timestamp
	r := authReq("b1-nonce-ts", "self", "tok")
	r.NodeAuthInfo.TimestampUs = 0
	if err := checkReplay(r, "self", "", false); err == nil {
		t.Error("missing timestamp must be rejected")
	}
	// stale timestamp (> 30s)
	r = authReq("b1-nonce-stale", "self", "tok")
	r.NodeAuthInfo.TimestampUs = time.Now().Add(-60 * time.Second).UnixMicro()
	if err := checkReplay(r, "self", "", false); err == nil {
		t.Error("stale timestamp must be rejected")
	}
	// missing nonce
	r = authReq("", "self", "tok")
	if err := checkReplay(r, "self", "", false); err == nil {
		t.Error("missing nonce must be rejected")
	}
	// cross-node replay: dest bound to another node
	if err := checkReplay(authReq("b1-nonce-dest", "other-node", "tok"), "self", "", false); err == nil {
		t.Error("a request destined for another node must be rejected")
	}
}

// TestCheckReplay_DestNodeIDBinding: the identity binding is what still holds
// when two nodes are misconfigured with the same name, which the identity-keyed
// directory now permits rather than refusing outright.
func TestCheckReplay_DestNodeIDBinding(t *testing.T) {
	withDestID := func(nonce, destID string) *proto.HeartBeatReq {
		r := authReq(nonce, "self", "tok")
		r.NodeAuthInfo.DestNodeId = destID
		return r
	}

	if err := checkReplay(withDestID("b1-id-ok", "my-id"), "self", "my-id", false); err != nil {
		t.Errorf("matching dest_node_id was rejected: %v", err)
	}
	if err := checkReplay(withDestID("b1-id-bad", "other-id"), "self", "my-id", false); err == nil {
		t.Error("a request bound to another node's identity must be rejected")
	}
	// Absent on either side means "not known", not "mismatch": a pre-2.8 sender
	// never sets it, and a node without persistence has no id of its own.
	if err := checkReplay(withDestID("b1-id-empty-src", ""), "self", "my-id", false); err != nil {
		t.Errorf("an unset dest_node_id was rejected: %v", err)
	}
	if err := checkReplay(withDestID("b1-id-empty-self", "some-id"), "self", "", false); err != nil {
		t.Errorf("an unset local identity caused a rejection: %v", err)
	}
}

// TestCheckReplay_RegisterNodeIsExemptFromIdentityBinding is load-bearing, not a
// nicety. RegisterNode is the call that REPAIRS a stale identity: a peer whose
// database was rebuilt comes back with a new id, so everyone still holding the
// old one addresses it by that old id. Enforcing the binding here would reject
// the repair in the interceptor, the response carrying the responder's real
// identity would never come back, and the tombstone path could never fire — the
// whole mechanism would be unreachable in exactly the case it exists for.
func TestCheckReplay_RegisterNodeIsExemptFromIdentityBinding(t *testing.T) {
	r := authReq("b1-register-stale-id", "self", "tok")
	r.NodeAuthInfo.DestNodeId = "the-identity-this-node-used-to-have"

	if err := checkReplay(r, "self", "its-new-identity", true); err != nil {
		t.Errorf("RegisterNode was rejected for carrying a stale destination identity: %v", err)
	}

	// The name binding still applies to RegisterNode, as do nonce and timestamp.
	other := authReq("b1-register-wrong-name", "someone-else", "tok")
	if err := checkReplay(other, "self", "its-new-identity", true); err == nil {
		t.Error("the destination NAME binding must still apply to RegisterNode")
	}
	if err := checkReplay(r, "self", "its-new-identity", true); err == nil {
		t.Error("nonce replay protection must still apply to RegisterNode")
	}
}
