package http

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// ProfileHandler GET /profile — returns info for the currently authenticated user.
// Requires Bearer JWT. X-Token is not accepted: it has no associated user record.
//
// Query params:
//
//	target — node name to query (default: local node). Use "all" to query all nodes.
type ProfileHandler struct{ HttpHandlerImp }

type profileInbound struct {
	Node      string `json:"node"`
	Tag       string `json:"tag"`
	Container string `json:"container"`
	Port      int    `json:"port"`
}

func (handler *ProfileHandler) handlerFunc(c *gin.Context) {
	usernameVal, exists := c.Get(auth.ContextKeyUsername)
	if !exists {
		c.JSON(401, gin.H{"code": 401, "msg": "JWT required for /profile"})
		return
	}
	username, _ := usernameVal.(string)

	target := c.DefaultQuery("target", handler.getHttpServer().Name)

	handler.serveRemote(c, username, target)
}

// serveRemote queries node(s) via RPC GetProfile and returns the full profile.
func (handler *ProfileHandler) serveRemote(c *gin.Context, username, target string) {
	nodes := handler.getHttpServer().GetTargetNodes(target)
	if len(nodes) == 0 {
		c.JSON(404, gin.H{"code": 404, "msg": "no available node for target: " + target})
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, _, err := rpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(),
		client.GetProfileReqType,
		&proto.GetProfileReq{Username: username},
		handler.getHttpServer().GetClusterToken(),
	)
	if err != nil {
		log.Warn("[Profile] RPC GetProfile failed", "target", target, "err", err)
		c.JSON(502, gin.H{"code": 502, "msg": "failed to query remote node: " + err.Error()})
		return
	}

	for nodeName, result := range succList {
		rsp, ok := result.(*proto.GetProfileRsp)
		if !ok || rsp.GetUsername() == "" {
			continue
		}
		expiryStr := ""
		if rsp.GetExpireTime() > 0 {
			expiryStr = time.Unix(rsp.GetExpireTime(), 0).UTC().Format("2006-01-02T15:04:05Z")
		}
		var inbounds []profileInbound
		for _, ib := range rsp.GetInbounds() {
			inbounds = append(inbounds, profileInbound{
				Node:      nodeName,
				Tag:       ib.GetTag(),
				Container: ib.GetContainer(),
				Port:      int(ib.GetPort()),
			})
		}
		c.JSON(200, gin.H{
			"node":                  nodeName,
			"username":              rsp.GetUsername(),
			"role":                  rsp.GetRole(),
			"expiry_time":           expiryStr,
			"traffic_limit":         rsp.GetTrafficLimit(),
			"traffic_used_uplink":   rsp.GetUplink(),
			"traffic_used_downlink": rsp.GetDownlink(),
			"auth_token":            rsp.GetProxyPassword(),
			"inbounds":              inbounds,
		})
		return
	}

	c.JSON(404, gin.H{"code": 404, "msg": "user not found on target node(s)"})
}

func (handler *ProfileHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ProfileHandler) getRelativePath() string { return "/profile" }

func (handler *ProfileHandler) help() string {
	return `/profile
	GET /profile
	Header: Authorization: Bearer <jwt-token>
	Query: target (optional, default: local node)
	Returns current user's profile info. JWT required; X-Token is not accepted.`
}
