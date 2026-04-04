package server

import (
	context "context"
	"fmt"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

func (s *EndNodeServer) GetUsers(ctx context.Context, getUsersReq *proto.GetUsersReq) (*proto.GetUsersRsp, error) {
	if s.clusterUserEnabled {
		return &proto.GetUsersRsp{Code: 400, Msg: "local user API disabled: cluster_user is enabled, use cluster user API instead"}, nil
	}
	getUsersRsp := &proto.GetUsersRsp{Code: 0}
	users := s.userMgr.ListUsers()
	for _, u := range users {
		protoUser := &proto.User{
			Name:   u.Username,
			Passwd: u.Password,
		}
		if stats, ok := s.userMgr.GetUserTrafficStats(u.Username); ok {
			protoUser.Downlink = stats.TotalDownlink
			protoUser.Uplink = stats.TotalUplink
		}
		getUsersRsp.Users = append(getUsersRsp.Users, protoUser)
	}
	return getUsersRsp, nil
}

func (s *EndNodeServer) AddUsers(ctx context.Context, addUsersReq *proto.UserOpReq) (*proto.UserOpRsp, error) {
	if s.clusterUserEnabled {
		return &proto.UserOpRsp{Code: 400, Msg: "local user API disabled: cluster_user is enabled, use cluster user API instead"}, nil
	}
	addUsersRsp := &proto.UserOpRsp{Code: 0}
	for _, user := range addUsersReq.GetUsers() {
		log.Infof("AddUsers: name=%s passwd=%q expire_time=%d tags=%v", user.Name, user.Passwd, user.ExpireTime, user.Tags)

		var ttl time.Duration
		if user.ExpireTime > 0 {
			ttl = time.Until(time.Unix(user.ExpireTime, 0))
		}
		err := s.userMgr.AddUser(usermanager.AddUserRequest{
			Username: user.Name,
			Password: user.Passwd,
			TTL:      ttl,
		})
		if err != nil {
			log.Error("add user failed", "err", err.Error(), "user", user.Name)
			addUsersRsp.Msg += fmt.Sprintf("user: %s add failed: %s|", user.Name, err.Error())
		}
	}
	if len(addUsersRsp.Msg) > 0 {
		addUsersRsp.Code = 200
	}
	return addUsersRsp, nil
}

func (s *EndNodeServer) DeleteUsers(ctx context.Context, deleteUsersReq *proto.UserOpReq) (*proto.UserOpRsp, error) {
	if s.clusterUserEnabled {
		return &proto.UserOpRsp{Code: 400, Msg: "local user API disabled: cluster_user is enabled, use cluster user API instead"}, nil
	}
	deleteUsersRsp := &proto.UserOpRsp{Code: 0}
	for _, user := range deleteUsersReq.GetUsers() {
		err := s.userMgr.RemoveUser(usermanager.RemoveUserRequest{Username: user.Name})
		if err != nil {
			log.Error("delete user failed", "err", err.Error(), "user", user.Name)
			deleteUsersRsp.Msg += fmt.Sprintf("user: %s delete failed, %s\n", user.Name, err.Error())
		}
	}
	if len(deleteUsersRsp.Msg) > 0 {
		deleteUsersRsp.Code = 201
	}
	return deleteUsersRsp, nil
}

func (s *EndNodeServer) UpdateUsers(ctx context.Context, updateUsersReq *proto.UserOpReq) (*proto.UserOpRsp, error) {
	if s.clusterUserEnabled {
		return &proto.UserOpRsp{Code: 400, Msg: "local user API disabled: cluster_user is enabled, use cluster user API instead"}, nil
	}
	updateUsersRsp := &proto.UserOpRsp{Code: 0}
	var err error
	for _, user := range updateUsersReq.GetUsers() {
		err = s.userMgr.UpdateUser(user.Name, user.Passwd, user.ExpireTime)
	}
	if err != nil {
		updateUsersRsp.Msg = err.Error()
	}
	return updateUsersRsp, nil
}

func (s *EndNodeServer) ResetUser(ctx context.Context, resetUserReq *proto.UserOpReq) (*proto.UserOpRsp, error) {
	if s.clusterUserEnabled {
		return &proto.UserOpRsp{Code: 400, Msg: "local user API disabled: cluster_user is enabled, use cluster user API instead"}, nil
	}
	rsp := &proto.UserOpRsp{Code: 0}
	for _, u := range resetUserReq.GetUsers() {
		username := u.GetName()
		if err := s.userMgr.RotateUserPort(username); err != nil {
			errMsg := fmt.Sprintf("rotate port for user %s: %v", username, err)
			log.Error("ResetUser failed", "user", username, "err", errMsg)
			rsp.Code = 300
			rsp.Msg = errMsg
			return rsp, nil
		}
		log.Info("ResetUser: port rotated", "user", username)
	}
	return rsp, nil
}

func (s *EndNodeServer) GetSub(ctx context.Context, getSubReq *proto.GetSubReq) (*proto.GetSubRsp, error) {
	getSubRsp := &proto.GetSubRsp{Code: 0}
	user := getSubReq.GetUser()
	excludeProtocols := getSubReq.GetExcludeProtocols()

	localUser, err := s.userMgr.GetUser(user.Name)
	if err != nil {
		errMsg := fmt.Sprintf("get sub err > %v", err)
		log.Error("get sub failed", "err", errMsg, "user", user.Name)
		getSubRsp.Msg = errMsg
		getSubRsp.Code = 300
		return getSubRsp, nil
	}
	if localUser.Password != user.Passwd {
		log.Error("get sub failed", "err", "invalid password", "user", user.Name)
		getSubRsp.Msg = "invalid password"
		getSubRsp.Code = 300
		return getSubRsp, nil
	}

	req := contracts.SubscriptionRequest{
		User:             contracts.UserSpec{Username: user.Name, Password: localUser.Password},
		Host:             s.cfg.ProxyHost,
		NodeName:         s.cfg.Name,
		ExcludeProtocols: excludeProtocols,
	}

	specs, err := s.subMgr.GetSubscription(req)
	if err != nil {
		errMsg := fmt.Sprintf("get sub err > %v", err)
		log.Error("get sub failed", "err", errMsg, "user", user.Name)
		getSubRsp.Msg = errMsg
		getSubRsp.Code = 300
		return getSubRsp, nil
	}

	var uris []string
	for _, spec := range specs {
		if spec.URI != "" {
			uris = append(uris, spec.URI)
		}
	}
	if len(uris) == 0 {
		log.Error("get sub failed", "err", "no subscription found", "user", user.Name)
		getSubRsp.Msg = "no subscription found"
		getSubRsp.Code = 300
		return getSubRsp, nil
	}
	getSubRsp.Uris = uris

	return getSubRsp, nil
}

func (s *EndNodeServer) GetBandWidthStats(ctx context.Context, getBandwidthStatsReq *proto.GetBandwidthStatsReq) (*proto.GetBandwidthStatsRsp, error) {
	getBandWidthStatsRsp := &proto.GetBandwidthStatsRsp{Code: 0}
	if s.statsCollector != nil {
		getBandWidthStatsRsp.Stats = s.statsCollector.DrainStats()
	}
	return getBandWidthStatsRsp, nil
}
