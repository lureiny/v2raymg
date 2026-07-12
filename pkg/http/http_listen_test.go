package http

import (
	"net"
	"testing"
)

// TestHttpServer_Listen_PortInUse: an unbindable HTTP address surfaces an error
// for fail-fast handling instead of being swallowed in a goroutine (finding #2).
func TestHttpServer_Listen_PortInUse(t *testing.T) {
	occ, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occ.Close()
	port := occ.Addr().(*net.TCPAddr).Port
	s := &HttpServer{Host: "127.0.0.1", Port: port}
	if _, err := s.Listen(); err == nil {
		t.Fatal("expected a bind error for an occupied HTTP port")
	}
}
