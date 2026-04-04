package client

type ReqToEndNodeType int

const (
	AddUsersReqType = iota
	DeleteUsersReqType
	UpdateUsersReqType
	ResetUserReqType
	GetSubReqType
	GetBandWidthStatsReqType
	AddInboundReqType
	DeleteInboundReqType
	GetUsersReqType
	GetInboundReqType
	_ // was GetTagReqType (removed)
	UpdateProxyReqType
	_ // was AddAdaptiveConfigReqType
	_ // was DeleteAdaptiveConfigReqType
	_ // was AdaptiveReqType
	SetGatewayModelReqType
	ObtainNewCertType
	FastAddInboundType
	TransferCertType
	GetCertsType
	_ // ClearUsersType removed — use DeleteClusterUsers tombstone instead
	GetPingMetricType
	RegisterNodeType
	HeartBeatType
	SetPingCheckType
	GetNodeMetricType
	ListInboundReqType
	DeleteInboundByNameReqType
	GetNodeGroupsReqType
	SetNodeGroupsReqType
	ListClusterUsersReqType
	GetClusterUsersByNameReqType
	UpsertClusterUsersReqType
	DeleteClusterUsersReqType
	GetStatusReqType
	GetProfileReqType
)
