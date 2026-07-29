package appconfig

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Cluster-plane addresses are written as URLs so the transport travels with the
// address instead of being guessed at dial time:
//
//	grpc://node.example.com:9090     plaintext h2c — what this plane has always spoken
//	grpcs://cluster.example.com:443  TLS, normally terminated by a reverse proxy
//
// Guessing was the alternative, and it is a downgrade attack: a client that
// probes for TLS and falls back to plaintext when the handshake fails hands an
// on-path attacker the whole point of the TLS in the first place, since breaking
// a handshake only takes an RST. Declaring the scheme removes the choice.
const (
	SchemeGRPC  = "grpc"
	SchemeGRPCS = "grpcs"

	// Default ports mirror http/https. gRPC has no registered default, so
	// borrowing the pair everybody already knows beats inventing one.
	defaultGRPCPort  = 80
	defaultGRPCSPort = 443
)

// ClusterAddr is a parsed cluster-plane address.
type ClusterAddr struct {
	TLS  bool
	Host string
	Port int32
}

// String renders the canonical form. Round-trips through ParseClusterAddr.
func (a ClusterAddr) String() string {
	scheme := SchemeGRPC
	if a.TLS {
		scheme = SchemeGRPCS
	}
	return scheme + "://" + net.JoinHostPort(a.Host, strconv.Itoa(int(a.Port)))
}

// ParseClusterAddr accepts "grpc://host:port", "grpcs://host:port", and the
// abbreviations an operator is likely to write instead:
//
//   - no scheme      -> grpc:// (plaintext, the pre-2.10 behaviour)
//   - no port        -> 80 for grpc, 443 for grpcs
//   - IPv6 literals  -> "grpcs://[::1]:443", brackets stripped from Host
//
// Only grpc and grpcs are accepted. Anything else is an error rather than a
// silent coercion: "https://..." almost certainly means the operator confused
// the management API with the cluster plane, and quietly treating it as grpcs
// would hide that until peers failed to connect.
func ParseClusterAddr(raw string) (ClusterAddr, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ClusterAddr{}, fmt.Errorf("appconfig: empty cluster address")
	}

	// url.Parse only recognises a scheme when "//" follows it, and treats a bare
	// "host:port" as scheme "host" with opaque "port". Adding the default scheme
	// up front sidesteps both quirks.
	if !strings.Contains(s, "://") {
		s = SchemeGRPC + "://" + s
	}

	u, err := url.Parse(s)
	if err != nil {
		return ClusterAddr{}, fmt.Errorf("appconfig: invalid cluster address %q: %w", raw, err)
	}

	scheme := strings.ToLower(u.Scheme)
	var tlsOn bool
	switch scheme {
	case SchemeGRPC:
	case SchemeGRPCS:
		tlsOn = true
	default:
		return ClusterAddr{}, fmt.Errorf(
			"appconfig: cluster address %q has scheme %q; only %q and %q are supported",
			raw, u.Scheme, SchemeGRPC, SchemeGRPCS)
	}

	if u.Path != "" && u.Path != "/" {
		return ClusterAddr{}, fmt.Errorf(
			"appconfig: cluster address %q must not carry a path — gRPC routes on the "+
				"method name, so a path prefix cannot be honoured", raw)
	}

	host := u.Hostname() // strips IPv6 brackets
	if host == "" {
		return ClusterAddr{}, fmt.Errorf("appconfig: cluster address %q has no host", raw)
	}

	port := int32(defaultGRPCPort)
	if tlsOn {
		port = defaultGRPCSPort
	}
	if p := u.Port(); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil || n == 0 {
			return ClusterAddr{}, fmt.Errorf("appconfig: cluster address %q has an invalid port %q", raw, p)
		}
		port = int32(n)
	}

	return ClusterAddr{TLS: tlsOn, Host: host, Port: port}, nil
}
