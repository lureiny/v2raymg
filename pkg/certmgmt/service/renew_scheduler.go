package certmgmtservice

import (
	"context"
	"log"
	"time"
)

// StartAutoRenew starts a background goroutine that checks and renews certificates
// every cycleSeconds seconds. It exits when ctx is cancelled.
func (m *Manager) StartAutoRenew(ctx context.Context, cycleSeconds int64) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			m.runRenewCycle(ctx)

			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(cycleSeconds) * time.Second):
			}
		}
	}()
}

// RunRenewCycleForTest is an exported wrapper for runRenewCycle, for use in tests.
func (m *Manager) RunRenewCycleForTest(ctx context.Context) {
	m.runRenewCycle(ctx)
}

// runRenewCycle iterates over all stored certificates and renews those close to expiry.
func (m *Manager) runRenewCycle(ctx context.Context) {
	records := m.ListCerts()
	for _, record := range records {
		if ctx.Err() != nil {
			return
		}
		_, err := m.RenewDomain(ctx, record.Domain)
		if err != nil {
			log.Printf("certmgmt: auto-renew %q: %v", record.Domain, err)
		}
	}
}
