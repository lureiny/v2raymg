package server

import (
	context "context"
	"time"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
	clusteruserhash "github.com/lureiny/v2raymg/pkg/cluster_user/hash"
	"github.com/lureiny/v2raymg/pkg/cluster_user/version"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// Note: clusterUserMu is a field on EndNodeServer (not a package-level var)
// to avoid cross-instance contention.

// ---------------------------------------------------------------------------
// Proto <-> ClusterUser conversion helpers
// ---------------------------------------------------------------------------

func protoToClusterUser(p *proto.ClusterUserInfo) *clusteruser.ClusterUser {
	if p == nil {
		return nil
	}
	return &clusteruser.ClusterUser{
		Username:    p.GetUsername(),
		Password:    p.GetPassword(),
		Expire:      p.GetExpire(),
		Role:        p.GetRole(),
		TargetGroup: p.GetTargetGroup(),
		Deleted:     p.GetDeleted(),
		UpdatedAtUs: p.GetUpdatedAtUs(),
		OriginNode:  p.GetOriginNode(),
		Hash:        p.GetHash(),
	}
}

func clusterUserToProto(u *clusteruser.ClusterUser) *proto.ClusterUserInfo {
	if u == nil {
		return nil
	}
	return &proto.ClusterUserInfo{
		Username:    u.Username,
		Password:    u.Password,
		Expire:      u.Expire,
		Role:        u.Role,
		TargetGroup: u.TargetGroup,
		Deleted:     u.Deleted,
		UpdatedAtUs: u.UpdatedAtUs,
		OriginNode:  u.OriginNode,
		Hash:        u.Hash,
	}
}

// ---------------------------------------------------------------------------
// gRPC handlers
// ---------------------------------------------------------------------------

// ListClusterUsers returns all non-deleted cluster users for the node, optionally
// filtered by group.  Tombstone records (Deleted=true) are not returned to API callers.
func (s *EndNodeServer) ListClusterUsers(ctx context.Context, req *proto.ListClusterUsersReq) (*proto.ListClusterUsersRsp, error) {
	rsp := &proto.ListClusterUsersRsp{}

	if !s.clusterUserEnabled {
		rsp.Code = 400
		rsp.Msg = "cluster_user disabled"
		return rsp, nil
	}

	var (
		users []*clusteruser.ClusterUser
		err   error
	)

	group := req.GetGroup()
	if group != "" {
		users, err = s.clusterUserStore.ListByGroup(group)
	} else {
		users, err = s.clusterUserStore.List()
	}
	if err != nil {
		rsp.Code = 500
		rsp.Msg = err.Error()
		return rsp, nil
	}

	// Filter out tombstones before returning.
	result := make([]*proto.ClusterUserInfo, 0, len(users))
	for _, u := range users {
		if u.Deleted {
			continue
		}
		result = append(result, clusterUserToProto(u))
	}

	rsp.Users = result
	return rsp, nil
}

// GetClusterUsersByName returns the full records for the requested usernames,
// including tombstones (used by the sync layer so peers can fetch deleted records).
func (s *EndNodeServer) GetClusterUsersByName(ctx context.Context, req *proto.GetClusterUsersByNameReq) (*proto.GetClusterUsersByNameRsp, error) {
	rsp := &proto.GetClusterUsersByNameRsp{}

	if !s.clusterUserEnabled {
		rsp.Code = 400
		rsp.Msg = "cluster_user disabled"
		return rsp, nil
	}

	result := make([]*proto.ClusterUserInfo, 0, len(req.GetUsernames()))
	for _, username := range req.GetUsernames() {
		u, err := s.clusterUserStore.Get(username)
		if err != nil {
			rsp.Code = 500
			rsp.Msg = err.Error()
			return rsp, nil
		}
		if u == nil {
			continue
		}
		result = append(result, clusterUserToProto(u))
	}

	rsp.Users = result
	return rsp, nil
}

// UpsertClusterUsers writes cluster user records.
// When req.FromAdmin is true (Admin HTTP write): auto-fills version fields and
// merges with existing records so that unset fields are not overwritten with zero values.
// When req.FromAdmin is false (Peer sync write): applies version arbitration and
// only stores records that are strictly newer than the local copy.
func (s *EndNodeServer) UpsertClusterUsers(ctx context.Context, req *proto.UpsertClusterUsersReq) (*proto.UpsertClusterUsersRsp, error) {
	rsp := &proto.UpsertClusterUsersRsp{}

	if !s.clusterUserEnabled {
		rsp.Code = 400
		rsp.Msg = "cluster_user disabled"
		return rsp, nil
	}

	fromAdmin := req.GetFromAdmin()

	s.clusterUserMu.Lock()
	defer s.clusterUserMu.Unlock()

	for _, pu := range req.GetUsers() {
		incoming := protoToClusterUser(pu)
		if incoming == nil || incoming.Username == "" {
			continue
		}

		if fromAdmin {
			// Admin HTTP write — read-modify-write so unset fields do not
			// overwrite existing values with zero/empty strings.
			prior, err := s.clusterUserStore.Get(incoming.Username)
			if err != nil {
				rsp.Code = 500
				rsp.Msg = err.Error()
				return rsp, nil
			}
			if prior != nil {
				if incoming.Password == "" {
					incoming.Password = prior.Password
				}
				if incoming.Role == "" {
					incoming.Role = prior.Role
				}
				if incoming.TargetGroup == "" {
					incoming.TargetGroup = prior.TargetGroup
				}
				if incoming.Expire == 0 {
					incoming.Expire = prior.Expire
				}
			} else if incoming.Password == "" {
				// New user with no prior record — password is required.
				rsp.Code = 400
				rsp.Msg = "password is required for new cluster user: " + incoming.Username
				return rsp, nil
			}
			if incoming.TargetGroup == "" {
				incoming.TargetGroup = "default"
			}
			incoming.UpdatedAtUs = time.Now().UnixMicro()
			incoming.OriginNode = s.Name
			incoming.Hash = clusteruserhash.ComputeHash(incoming)
		} else {
			// Peer sync write — apply version arbitration before writing.
			if incoming.OriginNode == "" {
				incoming.OriginNode = s.Name
			}
			incoming.Hash = clusteruserhash.ComputeHash(incoming)

			existing, err := s.clusterUserStore.Get(incoming.Username)
			if err != nil {
				rsp.Code = 500
				rsp.Msg = err.Error()
				return rsp, nil
			}
			if existing != nil && !version.IsNewer(incoming, existing) {
				continue
			}
		}

		if err := s.clusterUserStore.Upsert(incoming); err != nil {
			rsp.Code = 500
			rsp.Msg = err.Error()
			return rsp, nil
		}
	}

	return rsp, nil
}

// DeleteClusterUsers logically deletes the named users by setting Deleted=true (tombstone).
// Physical records are never removed so that tombstones propagate to peers.
func (s *EndNodeServer) DeleteClusterUsers(ctx context.Context, req *proto.DeleteClusterUsersReq) (*proto.DeleteClusterUsersRsp, error) {
	rsp := &proto.DeleteClusterUsersRsp{}

	if !s.clusterUserEnabled {
		rsp.Code = 400
		rsp.Msg = "cluster_user disabled"
		return rsp, nil
	}

	s.clusterUserMu.Lock()
	defer s.clusterUserMu.Unlock()

	for _, username := range req.GetUsernames() {
		existing, err := s.clusterUserStore.Get(username)
		if err != nil {
			rsp.Code = 500
			rsp.Msg = err.Error()
			return rsp, nil
		}
		if existing == nil {
			// User not stored locally yet — still write a tombstone so it
			// propagates to any peer that does hold the record.
			existing = &clusteruser.ClusterUser{Username: username}
		}
		if existing.Deleted {
			continue
		}

		existing.Deleted = true
		existing.UpdatedAtUs = time.Now().UnixMicro()
		existing.OriginNode = s.Name
		existing.Hash = clusteruserhash.ComputeHash(existing)

		if err := s.clusterUserStore.Upsert(existing); err != nil {
			rsp.Code = 500
			rsp.Msg = err.Error()
			return rsp, nil
		}
	}

	return rsp, nil
}
