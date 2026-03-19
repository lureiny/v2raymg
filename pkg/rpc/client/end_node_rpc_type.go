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
	GetTagReqType
	UpdateProxyReqType
	_ // was AddAdaptiveConfigReqType
	_ // was DeleteAdaptiveConfigReqType
	_ // was AdaptiveReqType
	SetGatewayModelReqType
	ObtainNewCertType
	FastAddInboundType
	TransferCertType
	GetCertsType
	ClearUsersType
	GetPingMetricType
	RegisterNodeType
	HeartBeatType
	SetPingCheckType
	GetNodeMetricType
)
