package mihomo

import (
	"errors"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestFromParams_Vmess(t *testing.T) {
	inb, err := FromParams("vmess-a", map[string]any{
		"protocol": "vmess",
		"port":     10001,
		"uuid":     "some-uuid",
	})
	if err != nil {
		t.Fatalf("FromParams: %v", err)
	}
	if inb.Protocol() != contracts.ProtocolVMess || inb.Port() != 10001 || inb.SharedCred.UUID != "some-uuid" {
		t.Errorf("unexpected inbound: %+v", inb)
	}
	if inb.ListenAddr() != "127.0.0.1" {
		t.Errorf("default listen_addr should be 127.0.0.1, got %q", inb.ListenAddr())
	}
}

func TestFromParams_Trojan(t *testing.T) {
	inb, err := FromParams("trojan-a", map[string]any{
		"protocol": "trojan",
		"port":     10002,
		"password": "pw",
	})
	if err != nil {
		t.Fatalf("FromParams: %v", err)
	}
	if inb.Protocol() != contracts.ProtocolTrojan || inb.SharedCred.Password != "pw" {
		t.Errorf("unexpected inbound: %+v", inb)
	}
}

func TestFromParams_Shadowsocks(t *testing.T) {
	inb, err := FromParams("ss-a", map[string]any{
		"protocol": "shadowsocks",
		"port":     10003,
		"password": "pw",
		"cipher":   "aes-128-gcm",
	})
	if err != nil {
		t.Fatalf("FromParams: %v", err)
	}
	if inb.SharedCred.Password != "pw" || inb.SharedCred.Cipher != "aes-128-gcm" {
		t.Errorf("unexpected cred: %+v", inb.SharedCred)
	}
}

func TestFromParams_ListenAddrOverride(t *testing.T) {
	inb, err := FromParams("t", map[string]any{
		"protocol":    "vmess",
		"port":        10001,
		"uuid":        "u",
		"listen_addr": "0.0.0.0",
	})
	if err != nil {
		t.Fatalf("FromParams: %v", err)
	}
	if inb.ListenAddr() != "0.0.0.0" {
		t.Errorf("listen_addr override not applied: got %q", inb.ListenAddr())
	}
}

func TestFromParams_ErrorPaths(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]any
		wantErr    error
		wantSubstr string
	}{
		{
			name:       "missing protocol",
			params:     map[string]any{"port": 10001, "uuid": "u"},
			wantSubstr: `"protocol" required`,
		},
		{
			name:    "unknown protocol",
			params:  map[string]any{"protocol": "tuic", "port": 10001, "uuid": "u"},
			wantErr: ErrProtocolNotSupported,
		},
		{
			name:       "missing port",
			params:     map[string]any{"protocol": "vmess", "uuid": "u"},
			wantSubstr: `"port" required`,
		},
		{
			name:       "port out of range (too high)",
			params:     map[string]any{"protocol": "vmess", "port": 70000, "uuid": "u"},
			wantSubstr: `out of range`,
		},
		{
			name:       "port negative",
			params:     map[string]any{"protocol": "vmess", "port": -1, "uuid": "u"},
			wantSubstr: `out of range`,
		},
		{
			name:       "port not numeric",
			params:     map[string]any{"protocol": "vmess", "port": "10001", "uuid": "u"},
			wantSubstr: `must be numeric`,
		},
		{
			name:       "port fractional float",
			params:     map[string]any{"protocol": "vmess", "port": 10001.5, "uuid": "u"},
			wantSubstr: `must be integer`,
		},
		{
			name:       "vmess missing uuid",
			params:     map[string]any{"protocol": "vmess", "port": 10001},
			wantSubstr: `"uuid" required`,
		},
		{
			name:       "vmess empty uuid",
			params:     map[string]any{"protocol": "vmess", "port": 10001, "uuid": ""},
			wantSubstr: `must not be empty`,
		},
		{
			name:       "trojan missing password",
			params:     map[string]any{"protocol": "trojan", "port": 10001},
			wantSubstr: `"password" required`,
		},
		{
			name:       "shadowsocks missing cipher",
			params:     map[string]any{"protocol": "shadowsocks", "port": 10001, "password": "pw"},
			wantSubstr: `"cipher" required`,
		},
		{
			name: "port below [100, 65535] fails final Validate",
			// port=50 decodes fine from requirePort but trips
			// DefaultInbound.Validate's 100..65535 bound.
			params:     map[string]any{"protocol": "vmess", "port": 50, "uuid": "u"},
			wantSubstr: `port`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FromParams("tag", tc.params)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("err should wrap %v, got: %v", tc.wantErr, err)
			}
			if tc.wantSubstr != "" && !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("err should contain %q, got: %v", tc.wantSubstr, err)
			}
		})
	}
}

func TestFromParams_PortAcceptsAllNumericShapes(t *testing.T) {
	// int32 in particular is the shape produced by proto-generated structs
	// (pkg/rpc/proto/rpc_server.pb.go FastAddInboundReq.Port is int32); a
	// regression that drops it would silently break the RPC dispatch path
	// when stage 5/6 wires mihomo into getContainerByType.
	shapes := map[string]any{
		"uint32":  uint32(10001),
		"uint16":  uint16(10001),
		"uint":    uint(10001),
		"int":     int(10001),
		"int32":   int32(10001),
		"int64":   int64(10001),
		"float64": float64(10001),
	}
	for name, portVal := range shapes {
		t.Run(name, func(t *testing.T) {
			inb, err := FromParams("t", map[string]any{
				"protocol": "vmess",
				"port":     portVal,
				"uuid":     "u",
			})
			if err != nil {
				t.Fatalf("FromParams: %v", err)
			}
			if inb.Port() != 10001 {
				t.Errorf("port = %d, want 10001", inb.Port())
			}
		})
	}
}
