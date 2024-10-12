package ping

// common option
type OptionFunc func(PingChecker)

// for ping pe
// WithHost ...
func WithHost(host string) func(PingChecker) {
	return func(pc PingChecker) {
		if ppc, ok := pc.(*PingPeChecker); ok {
			ppc.host = host
		}
	}
}

// for icmp ping and tcp ping
func WithGetNodeFunc(f getNodeFunc) func(PingChecker) {
	return func(pc PingChecker) {
		if ipc, ok := pc.(*IcmpPingChecker); ok {
			ipc.getNodeFunc = f
		}
	}
}
