package rpc

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// TestMethodBinding_RejectsCrossMethodSplice is the regression anchor for
// finding #2: five methods share UserOpReq (AddUsers/DeleteUsers/UpdateUsers/
// ResetUser/ResetUserTraffic), so before this fix a ciphertext built for one
// could be redirected to another. With method binding, the payload declares the
// method it was built for and the server rejects any mismatch.
func TestMethodBinding_RejectsCrossMethodSplice(t *testing.T) {
	const reset = "/proto.EndNodeAccess/ResetUserTraffic"
	const del = "/proto.EndNodeAccess/DeleteUsers"

	// A request legitimately built for ResetUserTraffic.
	req := &proto.UserOpReq{NodeAuthInfo: &proto.NodeAuthInfo{Token: "tok"}}
	StampDestMethod(req, reset)

	if got := req.GetNodeAuthInfo().GetDestMethod(); got != reset {
		t.Fatalf("StampDestMethod set %q, want %q", got, reset)
	}

	// Dispatched as the method it was built for -> accepted.
	if err := VerifyDestMethod(req, reset); err != nil {
		t.Errorf("legit request must pass its own method: %v", err)
	}

	// The splice attack: same ciphertext/payload redirected to DeleteUsers.
	// DestMethod still says ResetUserTraffic -> rejected.
	if err := VerifyDestMethod(req, del); err == nil {
		t.Error("payload bound to ResetUserTraffic must be rejected as DeleteUsers")
	}
}

func TestMethodBinding_RejectsMissingAndEmpty(t *testing.T) {
	const m = "/proto.EndNodeAccess/AddUsers"

	// Missing NodeAuthInfo -> fail closed.
	if err := VerifyDestMethod(&proto.UserOpReq{}, m); err == nil {
		t.Error("request without NodeAuthInfo must be rejected")
	}
	// NodeAuthInfo present but DestMethod never stamped (e.g. legacy client) -> reject.
	unstamped := &proto.UserOpReq{NodeAuthInfo: &proto.NodeAuthInfo{Token: "tok"}}
	if err := VerifyDestMethod(unstamped, m); err == nil {
		t.Error("request with empty dest_method must be rejected")
	}
	// A type that carries no NodeAuthInfo at all -> reject (not an authInfoCarrier).
	if err := VerifyDestMethod(struct{}{}, m); err == nil {
		t.Error("non-carrier request must be rejected")
	}
}

func TestStampDestMethod_NilAuthInfoIsNoop(t *testing.T) {
	// Must not panic when NodeAuthInfo is nil; simply leaves it unset.
	req := &proto.UserOpReq{}
	StampDestMethod(req, "/proto.EndNodeAccess/AddUsers")
	if req.GetNodeAuthInfo() != nil {
		t.Error("stamping a nil NodeAuthInfo must not allocate one")
	}
}
