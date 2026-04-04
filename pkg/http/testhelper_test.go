package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	proxyerrors "github.com/lureiny/v2raymg/pkg/proxy/errors"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockUserLister is a test double for UserLister.
type mockUserLister struct {
	users map[string]*contracts.User
	// optional overrides for interface extensions
	roleSetErrors        map[string]error
	rotatePortErrs       map[string]error
	rotateInboundPortErr map[string]error // keyed by username
	rotateAllPortsErr    map[string]error // keyed by username
}

func newMockUserLister(users ...*contracts.User) *mockUserLister {
	m := &mockUserLister{
		users:                make(map[string]*contracts.User),
		roleSetErrors:        make(map[string]error),
		rotatePortErrs:       make(map[string]error),
		rotateInboundPortErr: make(map[string]error),
		rotateAllPortsErr:    make(map[string]error),
	}
	for _, u := range users {
		m.users[u.Username] = u
	}
	return m
}

func (m *mockUserLister) ListUsersWithPasswd() map[string]string {
	out := make(map[string]string, len(m.users))
	for k, u := range m.users {
		out[k] = u.AuthToken
	}
	return out
}

func (m *mockUserLister) GetUser(username string) (*contracts.User, error) {
	u, ok := m.users[username]
	if !ok {
		return nil, proxyerrors.New(proxyerrors.ErrUserNotFound, fmt.Sprintf("user %s not found", username))
	}
	return u, nil
}

// SetUserRole implements the roleSetter interface used by SetUserRoleHandler.
func (m *mockUserLister) SetUserRole(username, role string) error {
	if err, ok := m.roleSetErrors[username]; ok {
		return err
	}
	u, ok := m.users[username]
	if !ok {
		return fmt.Errorf("user %s not found", username)
	}
	u.Role = role
	return nil
}

// RotateUserPort implements the portRotator interface used by RotatePortHandler.
func (m *mockUserLister) RotateUserPort(username string) error {
	if err, ok := m.rotatePortErrs[username]; ok {
		return err
	}
	return nil
}

// RotateUserPortForInbound implements the inboundPortRotator interface.
func (m *mockUserLister) RotateUserPortForInbound(username string, containerType contracts.ContainerType, inboundTag string, preferredPort uint32) (uint32, error) {
	if err, ok := m.rotateInboundPortErr[username]; ok {
		return 0, err
	}
	return 12345, nil
}

// RotateAllUserPorts implements the allPortRotator interface.
func (m *mockUserLister) RotateAllUserPorts(username string) (map[string]uint32, error) {
	if err, ok := m.rotateAllPortsErr[username]; ok {
		return nil, err
	}
	return map[string]uint32{"vless-tcp": 23456}, nil
}

// newTestHttpServer creates a minimal HttpServer for handler unit tests.
func newTestHttpServer(userLister UserLister) *HttpServer {
	return &HttpServer{
		RestfulServer:  gin.New(),
		token:          "admin-token",
		jwtSecret:      "test-jwt-secret-handler",
		jwtExpireHours: 1,
		handlersMap:    make(map[string]HttpHandlerInterface),
		userLister:     userLister,
		clusterNodes:   &stubTestClusterNodes{},
	}
}

// stubTestClusterNodes is a minimal ClusterNodes for test helpers.
type stubTestClusterNodes struct{}

func (s *stubTestClusterNodes) GetNodesWithFilter(f cluster.NodeFilter) []*cluster.Node {
	return nil
}
func (s *stubTestClusterNodes) GetClusterToken() string { return "" }
