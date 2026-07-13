package rpc

import (
	"context"
	"fmt"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"google.golang.org/grpc"
)

// authInfoCarrier is satisfied by every cluster RPC request (all embed a
// NodeAuthInfo), so method binding can be applied generically.
type authInfoCarrier interface {
	GetNodeAuthInfo() *proto.NodeAuthInfo
}

// StampDestMethod records the gRPC full-method on the request's NodeAuthInfo,
// so the server can bind the encrypted+authenticated payload to the method it
// was built for. No-op when the request carries no NodeAuthInfo.
func StampDestMethod(req interface{}, fullMethod string) {
	if c, ok := req.(authInfoCarrier); ok {
		if ai := c.GetNodeAuthInfo(); ai != nil {
			ai.DestMethod = fullMethod
		}
	}
}

// VerifyDestMethod fails closed: the request must carry a NodeAuthInfo whose
// DestMethod equals the method actually dispatched. Because DestMethod lives
// inside the AES-GCM-authenticated body, an on-path attacker on the plaintext
// transport cannot move a ciphertext to a sibling method that shares the same
// request proto type (finding #2 — e.g. ResetUserTraffic -> DeleteUsers, both
// UserOpReq). Tampering DestMethod breaks the GCM tag; leaving it stale makes it
// mismatch the redirected method.
func VerifyDestMethod(req interface{}, fullMethod string) error {
	c, ok := req.(authInfoCarrier)
	if !ok || c.GetNodeAuthInfo() == nil {
		return fmt.Errorf("method binding: request carries no node auth info")
	}
	got := c.GetNodeAuthInfo().GetDestMethod()
	if got == "" {
		return fmt.Errorf("method binding: request not bound to any method")
	}
	if got != fullMethod {
		return fmt.Errorf("method binding: payload bound to %q but dispatched as %q", got, fullMethod)
	}
	return nil
}

// StampDestMethodClientInterceptor stamps every outgoing unary call on the
// connection with its full method before the message is marshaled/encrypted.
// Wire it in with grpc.WithChainUnaryInterceptor on the cluster RPC dial.
func StampDestMethodClientInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	StampDestMethod(req, method)
	return invoker(ctx, method, req, reply, cc, opts...)
}
