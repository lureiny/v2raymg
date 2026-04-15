package client

type ReqToEndNodeType int

const (
	AddUsersReqType ReqToEndNodeType = iota
	DeleteUsersReqType
	UpdateUsersReqType
	ResetUserReqType
	GetSubReqType
	GetBandWidthStatsReqType
	AddInboundReqType
	GetUsersReqType
	GetInboundReqType
	UpdateProxyReqType
	SetGatewayModelReqType
	ObtainNewCertType
	FastAddInboundType
	TransferCertType
	GetCertsType
	GetPingMetricType
	RegisterNodeType
	HeartBeatType
	SetPingCheckType
	GetNodeMetricType
	ListInboundReqType
	DeleteInboundByNameReqType
	GetNodeGroupsReqType
	SetNodeGroupsReqType
	UpsertClusterUsersReqType
	GetStatusReqType
	GetProfileReqType
	DeleteCertReqType
	RotateInboundPortReqType
	RotateAllPortsReqType
	ResetAuthTokenReqType
)
