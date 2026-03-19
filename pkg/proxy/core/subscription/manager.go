// Package subscription provides subscription aggregation across multiple containers.
package subscription

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// containerSource provides access to loaded container instances.
// Satisfied by *container.ContainerMgr.
type containerSource interface {
	Types() []contracts.ContainerType
	Get(kind contracts.ContainerType) (container.Container, bool)
}

// userGetter validates and retrieves users by username.
// Satisfied by *usermanager.UserManager.
type userGetter interface {
	GetUser(username string) (*contracts.User, error)
}

// Manager aggregates subscription specs from all loaded containers
// after validating the requesting user.
type Manager struct {
	containers containerSource
	users      userGetter
}

// NewManager creates a new subscription Manager.
func NewManager(containers *container.ContainerMgr, users *usermanager.UserManager) *Manager {
	return &Manager{containers: containers, users: users}
}

// GetSubscription validates the user and aggregates subscription specs from all containers.
// Returns an error if the user is not found, expired, or being deleted.
// Per-container errors are tolerated — failed containers are silently skipped.
// If req.ExcludeProtocols is set, specs matching those protocol names are filtered out.
func (m *Manager) GetSubscription(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	user, err := m.users.GetUser(req.User.Username)
	if err != nil {
		return nil, err
	}
	if user.IsDeleting() {
		return nil, fmt.Errorf("user %s is being deleted", req.User.Username)
	}

	var specs []contracts.SubscriptionSpec
	for _, kind := range m.containers.Types() {
		c, ok := m.containers.Get(kind)
		if !ok {
			continue
		}
		subs, err := c.GetUserSubscriptions(req)
		if err != nil {
			continue
		}
		specs = append(specs, subs...)
	}

	if len(req.ExcludeProtocols) > 0 {
		specs = filterSpecsByProtocol(specs, req.ExcludeProtocols)
	}

	return specs, nil
}

// filterSpecsByProtocol removes specs whose protocol matches any of the excluded names.
func filterSpecsByProtocol(specs []contracts.SubscriptionSpec, excludeProtocols []string) []contracts.SubscriptionSpec {
	excludeSet := make(map[string]struct{}, len(excludeProtocols))
	for _, p := range excludeProtocols {
		excludeSet[p] = struct{}{}
	}
	result := specs[:0]
	for _, s := range specs {
		if _, excluded := excludeSet[string(s.Protocol)]; !excluded {
			result = append(result, s)
		}
	}
	return result
}

// GetSubscriptionForClient validates the user, aggregates subscription specs,
// and converts them to the specified client format.
// If format is empty, FormatCommon is used as default.
func (m *Manager) GetSubscriptionForClient(req contracts.SubscriptionRequest, format ClientFormat) (string, error) {
	specs, err := m.GetSubscription(req)
	if err != nil {
		return "", err
	}

	if format == "" {
		format = FormatCommon
	}

	c := GetConverterOrDefault(format)
	if c == nil {
		return "", fmt.Errorf("no converter registered for format: %s", format)
	}

	return c.Convert(specs)
}
