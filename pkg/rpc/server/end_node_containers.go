package server

import (
	context "context"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

func (s *EndNodeServer) GetContainers(ctx context.Context, req *proto.GetContainersReq) (*proto.GetContainersRsp, error) {
	if s.containerMgr == nil {
		return &proto.GetContainersRsp{Code: 0}, nil
	}
	typeStrs := s.containerMgr.TypeStrings()
	infos := make([]*proto.ContainerInfo, 0, len(typeStrs))
	for _, ts := range typeStrs {
		supportsChain := false
		if c, ok := s.containerMgr.Get(contracts.ContainerType(ts)); ok {
			if cc, ok := c.(container.ChainCapable); ok && cc.SupportsChainOutbound() {
				supportsChain = true
			}
		}
		infos = append(infos, &proto.ContainerInfo{
			Type:          ts,
			SupportsChain: supportsChain,
		})
	}
	return &proto.GetContainersRsp{
		Code:           0,
		Containers:     typeStrs,
		ContainerInfos: infos,
	}, nil
}
