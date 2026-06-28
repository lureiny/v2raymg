package certmgmtservice

import (
	"context"
	"log"
	"time"
)

// renewCheckInterval is how often auto-renew scans stored certificates for
// expiry. The scan is purely local (read NotAfter + compare), so it is cheap
// and intentionally NOT configurable. The renewal *lead time* (how long before
// expiry to start renewing) is the configurable knob — see Config.RenewBeforeHours.
const renewCheckInterval = time.Minute

// StartAutoRenew starts a background goroutine that scans certificates for
// expiry and renews those inside the renewal window. It returns immediately and
// exits when ctx is cancelled. The scan interval defaults to renewCheckInterval
// (1 minute); cycleSeconds>0 overrides it (used by tests).
func (m *Manager) StartAutoRenew(ctx context.Context, cycleSeconds int64) {
	interval := renewCheckInterval
	if cycleSeconds > 0 {
		interval = time.Duration(cycleSeconds) * time.Second
	}
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
			case <-time.After(interval):
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
		// Imported certs are managed externally (no ACME resource/account to renew
		// against); skip them so auto-renew doesn't fail on them every cycle.
		if record.ObtainedBy == "imported" {
			continue
		}
		renewed, err := m.RenewDomain(ctx, record.Domain)
		if err != nil {
			log.Printf("certmgmt: auto-renew %q: %v", record.Domain, err)
			continue
		}
		// renewed != nil only when a certificate was actually replaced this cycle
		// (nil,nil means "not yet due"). Files are overwritten in place at the same
		// path, so proxy cores pick up the new cert via their own file hot-reload;
		// no container restart is issued here by design.
		if renewed != nil {
			log.Printf("certmgmt: auto-renew %q: renewed in place, valid until %s",
				renewed.Domain, renewed.NotAfter.Format(time.RFC3339))
		}
	}
}
