package server

import (
	context "context"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// UpsertClusterUsers receives full user payloads from a remote peer during heartbeat
// push and applies them via UserManager's sync write path (version arbitration).
//
// The FromAdmin field is ignored — admin writes now go through the standard HTTP user
// API and are handled by UserManager directly.
func (s *EndNodeServer) UpsertClusterUsers(ctx context.Context, req *proto.UpsertClusterUsersReq) (*proto.UpsertClusterUsersRsp, error) {
	rsp := &proto.UpsertClusterUsersRsp{}

	if !s.userMgr.IsClusterEnabled() {
		rsp.Code = 400
		rsp.Msg = "cluster sync disabled"
		return rsp, nil
	}

	var applied int
	for _, pu := range req.GetUsers() {
		incoming := protoClusterUserSyncToUser(pu)
		if incoming == nil || incoming.Username == "" {
			continue
		}

		ok, err := s.userMgr.SyncUpsertUser(incoming)
		if err != nil {
			log.Error("UpsertClusterUsers: sync upsert failed", "username", incoming.Username, "err", err)
			continue
		}
		if ok {
			applied++
		}
	}

	log.Debug("UpsertClusterUsers: processed", "total", len(req.GetUsers()), "applied", applied)
	return rsp, nil
}
