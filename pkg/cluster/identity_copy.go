package cluster

import "github.com/lureiny/v2raymg/pkg/rpc/proto"

// CopyIdentity returns a fresh *proto.Node carrying every wire attribute of src.
//
// This exists because the directory rebuilds nodes field by field in half a
// dozen places — a registration claim becoming an entry, an entry adopting a
// reported id, a rename, a static seed being demoted, rows replayed from the
// database. Each of those used to enumerate Name/Host/Port/NodeId inline, so
// adding a field to proto.Node meant finding all of them; miss one and the new
// attribute is silently dropped on that path only. For `tls` that failure mode
// is a peer quietly dialled in plaintext and unreachable, with nothing in the
// logs pointing at the omission.
//
// So: one function, and TestCopyIdentity_CoversEveryProtoField walks proto.Node
// by reflection to assert nothing has been left behind. Add a field to the
// message and that test fails until it is handled here.
//
// Note the gossip path does NOT go through this: mergeRemoteNodes stores the
// received *proto.Node pointer as-is, so it carries new fields for free. The
// asymmetry is why the rebuild sites are easy to forget.
func CopyIdentity(src *proto.Node) *proto.Node {
	if src == nil {
		return &proto.Node{}
	}
	return &proto.Node{
		Name:   src.GetName(),
		Host:   src.GetHost(),
		Port:   src.GetPort(),
		NodeId: src.GetNodeId(),
		Tls:    src.GetTls(),
	}
}

// CopyIdentityWithout returns a copy of src with node_id cleared.
//
// Used when a directory entry is demoted back to an address-only placeholder:
// the identity is what has been invalidated, the address is what we still need
// in order to reach whatever now answers there.
func CopyIdentityWithout(src *proto.Node) *proto.Node {
	n := CopyIdentity(src)
	n.NodeId = ""
	return n
}
