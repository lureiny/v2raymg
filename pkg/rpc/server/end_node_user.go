package server

import (
	context "context"
	"fmt"
	"strings"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

func (s *EndNodeServer) GetProfile(ctx context.Context, req *proto.GetProfileReq) (*proto.GetProfileRsp, error) {
	user, err := s.userMgr.GetUser(req.GetUsername())
	if err != nil {
		return &proto.GetProfileRsp{Code: 404, Msg: "user not found"}, nil
	}
	role := user.Role
	if role == "" {
		role = "normal"
	}
	var expireTime int64
	if !user.ExpiryTime.IsZero() {
		expireTime = user.ExpiryTime.Unix()
	}
	rsp := &proto.GetProfileRsp{
		Code:          0,
		Username:      user.Username,
		Role:          role,
		ExpireTime:    expireTime,
		TrafficLimit:  user.TrafficLimit,
		Uplink:        user.TrafficTotalUplink,
		Downlink:      user.TrafficTotalDownlink,
		ProxyPassword: user.AuthToken,
	}
	if fm := s.userMgr.GetForwardManager(); fm != nil {
		rules := fm.GetRulesByUser(user.Username)
		for _, r := range rules {
			rsp.Inbounds = append(rsp.Inbounds, &proto.ProfileInbound{
				Tag:       r.InboundTag,
				Container: string(r.ContainerType),
				Port:      int32(r.ListenPort),
			})
		}
	}
	return rsp, nil
}

func (s *EndNodeServer) GetUsers(ctx context.Context, getUsersReq *proto.GetUsersReq) (*proto.GetUsersRsp, error) {
	getUsersRsp := &proto.GetUsersRsp{Code: 0}
	var users []*contracts.User
	if getUsersReq.GetIncludeAll() {
		users = s.userMgr.ListAllUsers()
	} else {
		users = s.userMgr.ListUsers()
	}
	for _, u := range users {
		pu := userToProtoUser(u)
		getUsersRsp.Users = append(getUsersRsp.Users, pu)
	}
	return getUsersRsp, nil
}

func (s *EndNodeServer) AddUsers(ctx context.Context, addUsersReq *proto.UserOpReq) (*proto.UserOpRsp, error) {
	addUsersRsp := &proto.UserOpRsp{Code: 0}
	for _, user := range addUsersReq.GetUsers() {
		log.Infof("AddUsers: name=%s expire_time=%d", user.Name, user.ExpireTime)

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
			continue
		}
		// Apply optional fields after user creation.
		if user.Role != "" {
			if err := s.userMgr.SetUserRole(user.Name, user.Role); err != nil {
				addUsersRsp.Msg += fmt.Sprintf("user: %s set role failed: %s|", user.Name, err.Error())
			}
		}
		if user.Group != "" {
			if err := s.userMgr.SetUserGroup(user.Name, user.Group); err != nil {
				addUsersRsp.Msg += fmt.Sprintf("user: %s set group failed: %s|", user.Name, err.Error())
			}
		}
		if user.UploadBps != 0 {
			bps := user.UploadBps
			if bps < 0 {
				bps = 0
			}
			if err := s.userMgr.SetUserBandwidthLimit(user.Name, forward.BandwidthUpload, bps); err != nil {
				addUsersRsp.Msg += fmt.Sprintf("user: %s set upload bw failed: %s|", user.Name, err.Error())
			}
		}
		if user.DownloadBps != 0 {
			bps := user.DownloadBps
			if bps < 0 {
				bps = 0
			}
			if err := s.userMgr.SetUserBandwidthLimit(user.Name, forward.BandwidthDownload, bps); err != nil {
				addUsersRsp.Msg += fmt.Sprintf("user: %s set download bw failed: %s|", user.Name, err.Error())
			}
		}
		if user.MaxClients != 0 {
			mc := int(user.MaxClients)
			if mc < 0 {
				mc = 0
			}
			if err := s.userMgr.SetUserClientLimit(user.Name, mc, int(user.ClientRecycleDelaySec), int(user.ClientDrainSec)); err != nil {
				addUsersRsp.Msg += fmt.Sprintf("user: %s set client limit failed: %s|", user.Name, err.Error())
			}
		}
	}
	if len(addUsersRsp.Msg) > 0 {
		addUsersRsp.Code = 200
	}
	return addUsersRsp, nil
}

func (s *EndNodeServer) DeleteUsers(ctx context.Context, deleteUsersReq *proto.UserOpReq) (*proto.UserOpRsp, error) {
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
	updateUsersRsp := &proto.UserOpRsp{Code: 0}
	var errMsgs []string
	for _, user := range updateUsersReq.GetUsers() {
		// Handle role update if specified.
		if user.Role != "" {
			if err := s.userMgr.SetUserRole(user.Name, user.Role); err != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("set role for %s: %s", user.Name, err.Error()))
			}
		}
		// Handle password and/or expiry update if specified.
		if user.Passwd != "" || user.ExpireTime > 0 {
			if err := s.userMgr.UpdateUser(user.Name, user.Passwd, user.ExpireTime); err != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("update %s: %s", user.Name, err.Error()))
			}
		}
		// Handle bandwidth limit updates. Sentinel: 0=no change, -1=remove, >0=set.
		if user.UploadBps != 0 {
			bps := user.UploadBps
			if bps < 0 {
				bps = 0
			}
			if err := s.userMgr.SetUserBandwidthLimit(user.Name, forward.BandwidthUpload, bps); err != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("set upload bw for %s: %s", user.Name, err.Error()))
			}
		}
		if user.DownloadBps != 0 {
			bps := user.DownloadBps
			if bps < 0 {
				bps = 0
			}
			if err := s.userMgr.SetUserBandwidthLimit(user.Name, forward.BandwidthDownload, bps); err != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("set download bw for %s: %s", user.Name, err.Error()))
			}
		}
		// Handle group update if specified.
		if user.Group != "" {
			if err := s.userMgr.SetUserGroup(user.Name, user.Group); err != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("set group for %s: %s", user.Name, err.Error()))
			}
		}
		// Handle client limit update. Sentinel: 0=no change, -1=remove, >0=set.
		// When removing the limit (mc→0), recycle/drain are also zeroed — this is
		// intentional because those fields are meaningless without a client cap.
		if user.MaxClients != 0 {
			mc := int(user.MaxClients)
			if mc < 0 {
				mc = 0
			}
			if err := s.userMgr.SetUserClientLimit(user.Name, mc, int(user.ClientRecycleDelaySec), int(user.ClientDrainSec)); err != nil {
				errMsgs = append(errMsgs, fmt.Sprintf("set client limit for %s: %s", user.Name, err.Error()))
			}
		}
	}
	if len(errMsgs) > 0 {
		updateUsersRsp.Msg = strings.Join(errMsgs, "|")
	}
	return updateUsersRsp, nil
}

func (s *EndNodeServer) ResetAuthToken(ctx context.Context, req *proto.ResetAuthTokenReq) (*proto.ResetAuthTokenRsp, error) {
	rsp := &proto.ResetAuthTokenRsp{Code: 0}
	if req.GetUsername() == "" {
		rsp.Code = 300
		rsp.Msg = "username is required"
		return rsp, nil
	}
	newToken, err := s.userMgr.ResetAuthToken(req.GetUsername(), req.GetNewToken())
	if err != nil {
		log.Error("ResetAuthToken failed", "user", req.GetUsername(), "err", err)
		rsp.Code = 300
		rsp.Msg = err.Error()
		return rsp, nil
	}
	rsp.AuthToken = newToken
	return rsp, nil
}

func (s *EndNodeServer) ResetUser(ctx context.Context, resetUserReq *proto.UserOpReq) (*proto.UserOpRsp, error) {
	rsp := &proto.UserOpRsp{Code: 0}
	for _, u := range resetUserReq.GetUsers() {
		username := u.GetName()
		// Reset auth token.
		if _, err := s.userMgr.ResetAuthToken(username, ""); err != nil {
			errMsg := fmt.Sprintf("reset auth token for user %s: %v", username, err)
			log.Error("ResetUser failed", "user", username, "err", errMsg)
			rsp.Code = 300
			rsp.Msg = errMsg
			return rsp, nil
		}
		// Rotate ports (make-before-break for all inbound).
		if _, err := s.userMgr.RotateAllUserPorts(username); err != nil {
			errMsg := fmt.Sprintf("rotate port for user %s: %v", username, err)
			log.Error("ResetUser failed", "user", username, "err", errMsg)
			rsp.Code = 300
			rsp.Msg = errMsg
			return rsp, nil
		}
		log.Info("ResetUser: token and port rotated", "user", username)
	}
	return rsp, nil
}

func (s *EndNodeServer) GetSub(ctx context.Context, getSubReq *proto.GetSubReq) (*proto.GetSubRsp, error) {
	getSubRsp := &proto.GetSubRsp{Code: 0}
	excludeProtocols := getSubReq.GetExcludeProtocols()

	// No auth here — authentication is done at the HTTP handler layer.
	// This RPC is protected by cluster token. Only username is needed.
	user := getSubReq.GetUser()
	if user == nil || user.Name == "" {
		log.Error("get sub failed", "err", "missing username")
		getSubRsp.Msg = "missing username"
		getSubRsp.Code = 300
		return getSubRsp, nil
	}
	localUser, err := s.userMgr.GetUser(user.Name)
	if err != nil {
		errMsg := fmt.Sprintf("get sub err > %v", err)
		log.Error("get sub failed", "err", errMsg, "user", user.Name)
		getSubRsp.Msg = errMsg
		getSubRsp.Code = 300
		return getSubRsp, nil
	}

	req := contracts.SubscriptionRequest{
		User:             contracts.UserSpec{Username: localUser.Username, AuthToken: localUser.AuthToken},
		Host:             s.cfg.ProxyHost,
		NodeName:         s.cfg.Name,
		ExcludeProtocols: excludeProtocols,
	}

	specs, err := s.subMgr.GetSubscription(req)
	if err != nil {
		errMsg := fmt.Sprintf("get sub err > %v", err)
		log.Error("get sub failed", "err", errMsg, "user", localUser.Username)
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
		log.Error("get sub failed", "err", "no subscription found", "user", localUser.Username)
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

func (s *EndNodeServer) RotateInboundPort(ctx context.Context, req *proto.RotateInboundPortReq) (*proto.RotateInboundPortRsp, error) {
	rsp := &proto.RotateInboundPortRsp{Code: 0}
	newPort, err := s.userMgr.RotateUserPortForInbound(req.GetUsername(), contracts.ContainerType(req.GetContainer()), req.GetInbound(), req.GetPort())
	if err != nil {
		log.Error("RotateInboundPort failed", "user", req.GetUsername(), "container", req.GetContainer(), "inbound", req.GetInbound(), "err", err)
		rsp.Code = 300
		rsp.Msg = err.Error()
		return rsp, nil
	}
	rsp.Port = newPort
	return rsp, nil
}

func (s *EndNodeServer) RotateAllPorts(ctx context.Context, req *proto.RotateAllPortsReq) (*proto.RotateAllPortsRsp, error) {
	rsp := &proto.RotateAllPortsRsp{Code: 0}
	ports, err := s.userMgr.RotateAllUserPorts(req.GetUsername())
	if err != nil {
		log.Error("RotateAllPorts failed", "user", req.GetUsername(), "err", err)
		if len(ports) > 0 {
			// 301 = partial success: some inbounds rotated, others failed.
			rsp.Code = 301
		} else {
			// 300 = full failure: no inbound was rotated.
			rsp.Code = 300
		}
		rsp.Msg = err.Error()
		rsp.Ports = ports
		return rsp, nil
	}
	rsp.Ports = ports
	return rsp, nil
}
