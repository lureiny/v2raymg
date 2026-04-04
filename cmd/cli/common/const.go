package common

const (
	Login       = "api/login"
	ListNodeURI = "api/node"
	ListCert    = "api/getCerts"

	Gateway   = "api/gateway"
	PingCheck = "api/pingCheck"
	ApplyCert = "api/cert"

	FastAddInbound = "api/inbound/fast"
	Inbound        = "api/inbound"
	Inbounds       = "api/inbounds"

	CopyUserBetweenNodes = "api/copyUserBetweenNodes"
	User      = "api/user"
	UserReset = "api/user/reset"

	Update = "api/update"

	RotatePort        = "api/rotatePort"
	RotateInboundPort = "api/rotateInboundPort"
	RotateAllPorts    = "api/rotateAllPorts"

	UserRole        = "api/user" // used as base path for /api/user/:name/role
	UserBandwidth   = "api/user" // used as base path for /api/user/:name/bandwidth
	UserClientLimit = "api/user" // used as base path for /api/user/:name/client-limit

	Logout         = "api/logout"
	Profile        = "api/profile"
	ChangePassword = "api/profile/password"
	Status         = "api/status"
	TransferCert   = "api/cert/transfer"
)
