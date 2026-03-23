package ping

// common option
type OptionFunc func(PingChecker)

// WithNodeManager sets the NodeManager for IcmpPingChecker and TcpPingChecker.
func WithNodeManager(nm NodeManager) func(PingChecker) {
	return func(pc PingChecker) {
		if ipc, ok := pc.(*IcmpPingChecker); ok {
			ipc.nodeManager = nm
		}
		if tpc, ok := pc.(*TcpPingChecker); ok {
			tpc.nodeManager = nm
		}
	}
}

// WithGetNodeFunc sets a custom node retrieval function (for IcmpPingChecker only).
func WithGetNodeFunc(f getNodeFunc) func(PingChecker) {
	return func(pc PingChecker) {
		if ipc, ok := pc.(*IcmpPingChecker); ok {
			ipc.getNodeFunc = f
		}
	}
}

// WithICMPPingInterval sets the interval between ICMP pings in seconds.
func WithICMPPingInterval(interval int) func(PingChecker) {
	return func(pc PingChecker) {
		if ipc, ok := pc.(*IcmpPingChecker); ok {
			if interval > 0 {
				ipc.interval = interval
			}
		}
	}
}

// WithICMPPingTimeout sets the ICMP ping timeout in seconds.
func WithICMPPingTimeout(timeout int) func(PingChecker) {
	return func(pc PingChecker) {
		if ipc, ok := pc.(*IcmpPingChecker); ok {
			if timeout > 0 {
				ipc.timeout = timeout
			}
		}
	}
}

// WithTCPPingInterval sets the interval between TCP pings in seconds.
func WithTCPPingInterval(interval int) func(PingChecker) {
	return func(pc PingChecker) {
		if tpc, ok := pc.(*TcpPingChecker); ok {
			if interval > 0 {
				tpc.interval = interval
			}
		}
	}
}

// WithTCPPingTimeout sets the TCP ping timeout in seconds.
func WithTCPPingTimeout(timeout int) func(PingChecker) {
	return func(pc PingChecker) {
		if tpc, ok := pc.(*TcpPingChecker); ok {
			if timeout > 0 {
				tpc.timeout = timeout
			}
		}
	}
}
