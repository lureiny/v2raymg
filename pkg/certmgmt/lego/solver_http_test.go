package certmgmtlego_test

import (
	"testing"

	certmgmtlego "github.com/lureiny/v2raymg/pkg/certmgmt/lego"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

func TestNewHTTP01Provider_Server(t *testing.T) {
	cfg := &domain.HTTPChallengeConfig{
		Mode:       "server",
		ListenAddr: ":8080",
	}
	p, err := certmgmtlego.NewHTTP01Provider(cfg)
	if err != nil {
		t.Fatalf("NewHTTP01Provider(server): %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewHTTP01Provider_ServerDefault(t *testing.T) {
	// Empty config should default to server mode on :80
	p, err := certmgmtlego.NewHTTP01Provider(nil)
	if err != nil {
		t.Fatalf("NewHTTP01Provider(nil): %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewHTTP01Provider_ServerWithProxyHeader(t *testing.T) {
	cfg := &domain.HTTPChallengeConfig{
		Mode:        "server",
		ListenAddr:  ":8080",
		ProxyHeader: "X-Forwarded-Host",
	}
	p, err := certmgmtlego.NewHTTP01Provider(cfg)
	if err != nil {
		t.Fatalf("NewHTTP01Provider(server+proxy): %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewHTTP01Provider_Webroot(t *testing.T) {
	t.TempDir() // ensure we have a temp dir
	dir := t.TempDir()
	cfg := &domain.HTTPChallengeConfig{
		Mode:    "webroot",
		WebRoot: dir,
	}
	p, err := certmgmtlego.NewHTTP01Provider(cfg)
	if err != nil {
		t.Fatalf("NewHTTP01Provider(webroot): %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewHTTP01Provider_Memcached(t *testing.T) {
	cfg := &domain.HTTPChallengeConfig{
		Mode:           "memcached",
		MemcachedHosts: []string{"127.0.0.1:11211"},
	}
	p, err := certmgmtlego.NewHTTP01Provider(cfg)
	if err != nil {
		t.Fatalf("NewHTTP01Provider(memcached): %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewHTTP01Provider_UnknownMode(t *testing.T) {
	cfg := &domain.HTTPChallengeConfig{
		Mode: "invalid",
	}
	_, err := certmgmtlego.NewHTTP01Provider(cfg)
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
}
