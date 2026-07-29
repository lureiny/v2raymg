package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseClusterAddr(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want ClusterAddr
	}{
		{"full plaintext", "grpc://node.example.com:9090", ClusterAddr{false, "node.example.com", 9090}},
		{"full tls", "grpcs://cluster.example.com:443", ClusterAddr{true, "cluster.example.com", 443}},
		{"scheme omitted means plaintext", "node.example.com:9090", ClusterAddr{false, "node.example.com", 9090}},
		{"port omitted, plaintext", "grpc://node.internal", ClusterAddr{false, "node.internal", 80}},
		{"port omitted, tls", "grpcs://node.internal", ClusterAddr{true, "node.internal", 443}},
		{"bare host takes both defaults", "node.internal", ClusterAddr{false, "node.internal", 80}},
		{"ipv4 literal", "grpcs://10.0.0.5:8443", ClusterAddr{true, "10.0.0.5", 8443}},
		{"ipv6 literal keeps host unbracketed", "grpcs://[2001:db8::1]:443", ClusterAddr{true, "2001:db8::1", 443}},
		{"ipv6 literal without port", "grpc://[::1]", ClusterAddr{false, "::1", 80}},
		{"scheme case is ignored", "GRPCS://node.internal:443", ClusterAddr{true, "node.internal", 443}},
		{"surrounding space is trimmed", "  grpcs://node.internal:443  ", ClusterAddr{true, "node.internal", 443}},
		{"trailing slash is not a path", "grpcs://node.internal:443/", ClusterAddr{true, "node.internal", 443}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseClusterAddr(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseClusterAddr_Rejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"blank", "   "},
		// https almost certainly means the operator aimed at the management API.
		// Silently treating it as grpcs would hide that until peers failed to connect.
		{"https scheme", "https://node.internal:443"},
		{"http scheme", "http://node.internal:8080"},
		{"unknown scheme", "tcp://node.internal:9090"},
		{"no host", "grpc://:9090"},
		{"port zero", "grpc://node.internal:0"},
		{"port out of range", "grpc://node.internal:70000"},
		{"non-numeric port", "grpc://node.internal:abc"},
		// gRPC routes on the method name, so a path prefix can never be honoured.
		{"path prefix", "grpcs://node.internal:443/cluster"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseClusterAddr(tc.in)
			assert.Error(t, err)
		})
	}
}

func TestClusterAddr_StringRoundTrips(t *testing.T) {
	for _, in := range []string{
		"grpc://node.example.com:9090",
		"grpcs://cluster.example.com:443",
		"grpcs://[2001:db8::1]:443",
	} {
		parsed, err := ParseClusterAddr(in)
		require.NoError(t, err)
		again, err := ParseClusterAddr(parsed.String())
		require.NoError(t, err)
		assert.Equal(t, parsed, again, "String() must round-trip through ParseClusterAddr")
	}
}

// The load-bearing compatibility property: with no reverse proxy configured, a
// node advertises exactly what every release before this one advertised.
func TestResolveAdvertise_WithoutReverseProxyIsUnchanged(t *testing.T) {
	cfg := &EndNodeConfig{}
	cfg.ProxyHost = "node1.example.com"
	cfg.RpcPort = 9090

	got, err := cfg.ResolveAdvertise()
	require.NoError(t, err)
	assert.Equal(t, ClusterAddr{TLS: false, Host: "node1.example.com", Port: 9090}, got)
}

func TestResolveAdvertise_ReverseProxyWins(t *testing.T) {
	cases := []struct {
		name string
		addr string
		want ClusterAddr
	}{
		{"tls proxy", "grpcs://cluster.example.com:443",
			ClusterAddr{true, "cluster.example.com", 443}},
		{"plaintext proxy", "grpc://cluster.example.com:8080",
			ClusterAddr{false, "cluster.example.com", 8080}},
		{"scheme omitted", "cluster.example.com:8080",
			ClusterAddr{false, "cluster.example.com", 8080}},
		{"port omitted takes the scheme default", "grpcs://cluster.example.com",
			ClusterAddr{true, "cluster.example.com", 443}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &EndNodeConfig{}
			cfg.ProxyHost = "node1.example.com" // must be ignored
			cfg.RpcPort = 9090
			cfg.ReverseProxy.GrpcAddr = tc.addr

			got, err := cfg.ResolveAdvertise()
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveAdvertise_Errors(t *testing.T) {
	t.Run("bad reverse proxy address", func(t *testing.T) {
		cfg := &EndNodeConfig{}
		cfg.ProxyHost = "node1.example.com"
		cfg.RpcPort = 9090
		cfg.ReverseProxy.GrpcAddr = "https://cluster.example.com"

		_, err := cfg.ResolveAdvertise()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reverse_proxy.grpc_addr")
	})

	// An empty proxy_host must NOT fail here. Validate already requires one when
	// rpc_port >= 1000, and a single node with the cluster plane switched off has
	// nothing to advertise to — erroring would refuse a configuration that has
	// always been legal.
	t.Run("no proxy_host is not an error at this layer", func(t *testing.T) {
		cfg := &EndNodeConfig{}
		cfg.RpcPort = 9090

		got, err := cfg.ResolveAdvertise()
		require.NoError(t, err)
		assert.Equal(t, ClusterAddr{TLS: false, Host: "", Port: 9090}, got)
	})
}

// Both spellings must decode, and the mapping form must keep meaning exactly
// what it meant before the URL form existed.
func TestStaticNodes_BothSpellings(t *testing.T) {
	content := `
store:
  dsn: /tmp/x.db
end_node:
  listen: "0.0.0.0"
  rpc_port: 9090
  name: n1
  proxy_host: n1.example.com
  cluster:
    static_nodes:
      - "grpcs://node2.example.com:443"
      - "grpc://node3.internal"
      - "node4.internal:9091"
      - host: node5.internal
        port: 9090
containers:
  containers:
    - type: xray
      enabled: true
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := LoadFromFile(path)
	require.NoError(t, err)

	require.Len(t, cfg.EndNode.Cluster.StaticNodes, 4)
	assert.Equal(t, StaticNodeConfig{Host: "node2.example.com", Port: 443, TLS: true},
		cfg.EndNode.Cluster.StaticNodes[0])
	assert.Equal(t, StaticNodeConfig{Host: "node3.internal", Port: 80, TLS: false},
		cfg.EndNode.Cluster.StaticNodes[1])
	assert.Equal(t, StaticNodeConfig{Host: "node4.internal", Port: 9091, TLS: false},
		cfg.EndNode.Cluster.StaticNodes[2])
	// The original form is untouched: no scheme, therefore plaintext.
	assert.Equal(t, StaticNodeConfig{Host: "node5.internal", Port: 9090, TLS: false},
		cfg.EndNode.Cluster.StaticNodes[3])
}

func TestStaticNodes_InvalidURLFailsTheLoad(t *testing.T) {
	content := `
store:
  dsn: /tmp/x.db
end_node:
  cluster:
    static_nodes:
      - "https://node2.example.com:443"
containers:
  containers:
    - type: xray
      enabled: true
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	_, err := LoadFromFile(path)
	require.Error(t, err, "a malformed seed must fail the load, not be silently dropped")
}
