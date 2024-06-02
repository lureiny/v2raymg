package rpc

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
	TransferInboundReqType
	CopyInboundReqType
	CopyUserReqType
	GetUsersReqType
	GetInboundReqType
	GetTagReqType
	UpdateProxyReqType
	AddAdaptiveConfigReqType
	DeleteAdaptiveConfigReqType
	AdaptiveReqType
	SetGatewayModelReqType
	ObtainNewCertType
	FastAddInboundType
	TransferCertType
	GetCertsType
	ClearUsersType
	GetPingMetricType
	RegisterNodeType
	HeartBeatType
)
