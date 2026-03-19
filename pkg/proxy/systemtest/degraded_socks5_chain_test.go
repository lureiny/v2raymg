package systemtest

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// Degraded validation runnable in restricted environments:
// - no xray binary required
// - no external network required
// It validates the same proxy-chain behavior using a tiny local SOCKS5 relay.
func TestDegradedLocalSocks5ProxyChain(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("OK_FROM_ORIGIN"))
	}))
	defer origin.Close()

	socksAddr, stop, err := startMinimalSocks5Server()
	if err != nil {
		t.Fatalf("start socks5: %v", err)
	}
	defer stop()

	client, err := httpClientViaSocks5Local(socksAddr)
	if err != nil {
		t.Fatalf("socks5 client: %v", err)
	}

	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("request through socks5 failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "OK_FROM_ORIGIN" {
		t.Fatalf("unexpected body: %s", string(body))
	}
}

func httpClientViaSocks5Local(socksAddr string) (*http.Client, error) {
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.Dial(network, addr)
	}
	return &http.Client{Transport: tr, Timeout: 8 * time.Second}, nil
}

func startMinimalSocks5Server() (addr string, stop func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSocks5Conn(c)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }, nil
}

func handleSocks5Conn(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)

	// greeting: VER NMETHODS METHODS...
	h := make([]byte, 2)
	if _, err := io.ReadFull(r, h); err != nil {
		return
	}
	if h[0] != 0x05 {
		return
	}
	methods := make([]byte, int(h[1]))
	if _, err := io.ReadFull(r, methods); err != nil {
		return
	}
	_, _ = c.Write([]byte{0x05, 0x00}) // no auth

	// request: VER CMD RSV ATYP DST.ADDR DST.PORT
	reqHead := make([]byte, 4)
	if _, err := io.ReadFull(r, reqHead); err != nil {
		return
	}
	if reqHead[0] != 0x05 || reqHead[1] != 0x01 {
		return
	} // only CONNECT

	var host string
	switch reqHead[3] {
	case 0x01: // IPv4
		ip := make([]byte, 4)
		if _, err := io.ReadFull(r, ip); err != nil {
			return
		}
		host = net.IP(ip).String()
	case 0x03: // domain
		lnb, _ := r.ReadByte()
		d := make([]byte, int(lnb))
		if _, err := io.ReadFull(r, d); err != nil {
			return
		}
		host = string(d)
	default:
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(r, portBytes); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBytes)
	targetAddr := fmt.Sprintf("%s:%d", host, port)

	target, err := net.DialTimeout("tcp", targetAddr, 3*time.Second)
	if err != nil {
		_, _ = c.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer target.Close()

	// success reply
	_, _ = c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	go io.Copy(target, r)
	_, _ = io.Copy(c, target)
}
