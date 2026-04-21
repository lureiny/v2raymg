package server

import (
	context "context"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

func (s *EndNodeServer) GetContainers(ctx context.Context, req *proto.GetContainersReq) (*proto.GetContainersRsp, error) {
	return &proto.GetContainersRsp{
		Code:       0,
		Containers: s.containerMgr.TypeStrings(),
	}, nil
}
