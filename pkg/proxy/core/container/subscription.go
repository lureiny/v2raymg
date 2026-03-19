package container

import "github.com/lureiny/v2raymg/pkg/proxy/core/contracts"

// SubscriptionProvider is an optional interface that containers can implement
// to provide subscription link generation.
type SubscriptionProvider interface {
	// GetUserSubscriptions generates subscription specs for a user across all inbounds.
	// Each inbound that the user has valid credentials for produces one SubscriptionSpec.
	GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error)
}
