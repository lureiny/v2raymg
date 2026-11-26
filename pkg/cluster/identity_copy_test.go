package cluster

import (
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	pb "google.golang.org/protobuf/proto"
)

// This is the guard the whole helper exists for.
//
// The directory rebuilds nodes field by field in several places. Before
// CopyIdentity they each enumerated the fields inline, so adding one to
// proto.Node meant remembering every site — and a miss produced a silently
// dropped attribute on one path only. For tls that means a peer dialled in
// plaintext and unreachable, with nothing in the logs naming the cause.
//
// So: populate every wire field with a distinguishable non-zero value, copy,
// and assert nothing came back zero. A new field on proto.Node fails here until
// CopyIdentity handles it.
func TestCopyIdentity_CoversEveryProtoField(t *testing.T) {
	src := &proto.Node{}
	v := reflect.ValueOf(src).Elem()
	rt := v.Type()

	populated := 0
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue // protoimpl bookkeeping (state/sizeCache/unknownFields)
		}
		fv := v.Field(i)
		switch fv.Kind() {
		case reflect.String:
			fv.SetString("v-" + f.Name)
		case reflect.Int32, reflect.Int64:
			fv.SetInt(4321)
		case reflect.Uint32, reflect.Uint64:
			fv.SetUint(4321)
		case reflect.Bool:
			fv.SetBool(true)
		default:
			t.Fatalf("proto.Node.%s has unhandled kind %s — extend this test and CopyIdentity",
				f.Name, fv.Kind())
		}
		populated++
	}
	if populated == 0 {
		t.Fatal("no exported fields found on proto.Node; the reflection walk is broken")
	}

	got := CopyIdentity(src)
	gv := reflect.ValueOf(got).Elem()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		if gv.Field(i).IsZero() {
			t.Errorf("CopyIdentity dropped proto.Node.%s — every wire attribute must ride "+
				"along with host/port, or this path silently loses it", f.Name)
		}
	}
}

func TestCopyIdentity_ValuesMatch(t *testing.T) {
	src := &proto.Node{
		Name:   "n1",
		Host:   "cluster.example.com",
		Port:   443,
		NodeId: "id-1",
		Tls:    true,
	}
	got := CopyIdentity(src)

	if !pb.Equal(got, src) {
		t.Fatalf("copy differs from source:\n got %v\nwant %v", got, src)
	}
	if got == src {
		t.Fatal("CopyIdentity must return a new message, not the same pointer")
	}
}

func TestCopyIdentity_NilIsEmptyNotNil(t *testing.T) {
	got := CopyIdentity(nil)
	if got == nil {
		t.Fatal("CopyIdentity(nil) must return an empty message, not nil")
	}
}

func TestCopyIdentityWithout_ClearsOnlyTheIdentity(t *testing.T) {
	src := &proto.Node{
		Name:   "n1",
		Host:   "cluster.example.com",
		Port:   443,
		NodeId: "id-1",
		Tls:    true,
	}
	got := CopyIdentityWithout(src)

	if got.GetNodeId() != "" {
		t.Errorf("node_id should be cleared, got %q", got.GetNodeId())
	}
	// The address and how to reach it are exactly what a demoted entry still
	// needs — only the identity has been invalidated.
	if got.GetHost() != src.GetHost() || got.GetPort() != src.GetPort() {
		t.Errorf("address must survive demotion, got %s:%d", got.GetHost(), got.GetPort())
	}
	if got.GetTls() != src.GetTls() {
		t.Error("transport must survive demotion: the address is unreachable without it")
	}
}
