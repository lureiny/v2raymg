package common

const (
	ListNodeURI = "node"
	ListCert    = "getCerts"

	Gateway   = "gateway"
	ApplyCert = "cert"

	FastAddInbound = "fastAddInbound"

	CopyUserBetweenNodes = "copyUserBetweenNodes"
	User                 = "user"
	ClearUsers           = "clearUsers"

	Bound = "bound"
)

// user op type
type UserOpType int

const (
	AddUser UserOpType = iota + 1
	UpdateUser
	DeleteUser
	ResetUser
	ListUser
)
