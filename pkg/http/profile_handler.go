package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

// ProfileHandler GET /profile — returns info for the currently authenticated user.
// Requires Bearer JWT. X-Token is not accepted: it has no associated user record.
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
		// No JWT username in context (X-Token path or unauthenticated). Reject.
		c.JSON(401, gin.H{"code": 401, "msg": "JWT required for /profile"})
		return
	}
	username, _ := usernameVal.(string)

	user, err := handler.getHttpServer().userLister.GetUser(username)
	if err != nil {
		log.Warn("[Profile] user not found", "username", username)
		c.JSON(404, gin.H{"code": 404, "msg": "user not found"})
		return
	}

	role := user.Role
	if role == "" {
		role = "normal"
	}

	expiryStr := ""
	if !user.ExpiryTime.IsZero() {
		expiryStr = user.ExpiryTime.UTC().Format("2006-01-02T15:04:05Z")
	}

	var inbounds []profileInbound
	type ruleProvider interface {
		GetRulesByUser(username string) []*forward.ForwardRule
	}
	if rp, ok := handler.getHttpServer().userLister.(ruleProvider); ok {
		rules := rp.GetRulesByUser(username)
		inbounds = make([]profileInbound, 0, len(rules))
		nodeName := handler.getHttpServer().Name
		for _, r := range rules {
			inbounds = append(inbounds, profileInbound{
				Node:      nodeName,
				Tag:       r.InboundTag,
				Container: string(r.ContainerType),
				Port:      int(r.ListenPort),
			})
		}
	}

	c.JSON(200, gin.H{
		"username":              user.Username,
		"role":                  role,
		"expiry_time":           expiryStr,
		"traffic_limit":         user.TrafficLimit,
		"traffic_used_uplink":   user.TrafficTotalUplink,
		"traffic_used_downlink": user.TrafficTotalDownlink,
		"proxy_password":        user.Password,
		"inbounds":              inbounds,
	})
}

func (handler *ProfileHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ProfileHandler) getRelativePath() string { return "/profile" }

func (handler *ProfileHandler) help() string {
	return `/profile
	GET /profile
	Header: Authorization: Bearer <jwt-token>
	Returns current user's profile info. JWT required; X-Token is not accepted.`
}
