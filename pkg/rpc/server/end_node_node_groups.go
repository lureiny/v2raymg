package server

import (
	context "context"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// GetNodeGroups returns the node group assignments for this node.
// If no groups are stored, ["default"] is returned.
func (s *EndNodeServer) GetNodeGroups(ctx context.Context, req *proto.GetNodeGroupsReq) (*proto.GetNodeGroupsRsp, error) {
	rsp := &proto.GetNodeGroupsRsp{}

	if !s.userMgr.IsClusterEnabled() {
		rsp.Code = 400
		rsp.Msg = "cluster_user disabled"
		return rsp, nil
	}

	groups, err := s.userMgr.NodeGroupsStore().List()
	if err != nil {
		rsp.Code = 500
		rsp.Msg = err.Error()
		return rsp, nil
	}

	if len(groups) == 0 {
		groups = []string{"default"}
	}

	rsp.Groups = groups
	return rsp, nil
}

// SetNodeGroups atomically replaces the node group assignments for this node.
// If req.Groups is empty, ["default"] is written.
func (s *EndNodeServer) SetNodeGroups(ctx context.Context, req *proto.SetNodeGroupsReq) (*proto.SetNodeGroupsRsp, error) {
	rsp := &proto.SetNodeGroupsRsp{}

	if !s.userMgr.IsClusterEnabled() {
		rsp.Code = 400
		rsp.Msg = "cluster_user disabled"
		return rsp, nil
	}

	groups := req.GetGroups()
	if len(groups) == 0 {
		groups = []string{"default"}
	}

	if err := s.userMgr.SetNodeGroups(groups); err != nil {
		rsp.Code = 500
		rsp.Msg = err.Error()
		return rsp, nil
	}

	return rsp, nil
}
