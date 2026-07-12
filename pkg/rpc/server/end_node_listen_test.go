package server

import (
	"net"
	"testing"
)

// TestEndNodeServer_Listen_UnconfiguredDisabled: an unconfigured RPC address is
// a legitimate disabled-RPC (HTTP-only) node, so Listen returns (nil, nil) —
// not an error that would fail-fast the whole node.
func TestEndNodeServer_Listen_UnconfiguredDisabled(t *testing.T) {
	s := &EndNodeServer{} // Host="", Port=0
	lis, err := s.Listen()
	if err != nil {
		t.Fatalf("unconfigured RPC must not error, got: %v", err)
	}
	if lis != nil {
		lis.Close()
		t.Fatal("unconfigured RPC must return a nil listener (disabled)")
	}
}

// TestEndNodeServer_Listen_PortInUse: a configured-but-unbindable RPC address
// surfaces an error for fail-fast handling (finding #2).
func TestEndNodeServer_Listen_PortInUse(t *testing.T) {
	occ, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occ.Close()
	port := occ.Addr().(*net.TCPAddr).Port
	s := &EndNodeServer{Host: "127.0.0.1", Port: port}
	if _, err := s.Listen(); err == nil {
		t.Fatal("expected a bind error for an occupied RPC port")
	}
}
