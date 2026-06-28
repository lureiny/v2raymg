package certmgmtservice_test

import (
	"context"
	"testing"
	"time"

	certmgmtservice "github.com/lureiny/v2raymg/pkg/certmgmt/service"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// TestRenewWindow_HoursPrecedenceAndDefault exercises the Manager's renewal gate
// (renewBeforeDuration) end-to-end through RunRenewCycle: a cert is renewed iff
// its remaining lifetime is within the effective window.
func TestRenewWindow_HoursPrecedenceAndDefault(t *testing.T) {
	cases := []struct {
		name      string
		hours     int
		days      int
		expiresIn time.Duration
		wantRenew bool
	}{
		{"hours wins, outside window", 1, 30, 10 * 24 * time.Hour, false}, // 1h window, expires in 10d
		{"hours wins, inside window", 48, 0, 24 * time.Hour, true},        // 48h window, expires in 24h
		{"days fallback when hours unset", 0, 30, 10 * 24 * time.Hour, true},
		{"default 24h, outside window", 0, 0, 48 * time.Hour, false},
		{"default 24h, inside window", 0, 0, 12 * time.Hour, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			basePath := t.TempDir()
			const d = "example.com"
			writeMeta(t, basePath, &domain.CertificateRecord{
				Domain:   d,
				NotAfter: time.Now().Add(tc.expiresIn),
			})

			tracker := newTrackingIssuer()
			cfg := certmgmtservice.Config{
				Email:            "t@example.com",
				Path:             basePath,
				RenewBeforeHours: tc.hours,
				RenewBeforeDays:  tc.days,
				Challenge:        certmgmtservice.ChallengeConfig{Type: "http01"},
			}
			mgr := certmgmtservice.NewManagerWithIssuer(cfg, tracker)
			mgr.RunRenewCycleForTest(context.Background())

			got := tracker.renewed[d] > 0
			if got != tc.wantRenew {
				t.Errorf("renew=%v, want %v (hours=%d days=%d expiresIn=%s)",
					got, tc.wantRenew, tc.hours, tc.days, tc.expiresIn)
			}
		})
	}
}

// TestRenewCycle_SkipsImportedCerts ensures auto-renew never tries to ACME-renew
// an imported cert (which has no ACME resource/account to renew against).
func TestRenewCycle_SkipsImportedCerts(t *testing.T) {
	basePath := t.TempDir()
	const d = "imported.example.com"
	writeMeta(t, basePath, &domain.CertificateRecord{
		Domain:     d,
		NotAfter:   time.Now().Add(1 * time.Hour), // well inside any window
		ObtainedBy: "imported",
	})

	tracker := newTrackingIssuer()
	cfg := certmgmtservice.Config{
		Email:     "t@example.com",
		Path:      basePath,
		Challenge: certmgmtservice.ChallengeConfig{Type: "http01"},
	}
	mgr := certmgmtservice.NewManagerWithIssuer(cfg, tracker)
	mgr.RunRenewCycleForTest(context.Background())

	if tracker.renewed[d] != 0 {
		t.Errorf("imported cert %q should be skipped by auto-renew, but Renew was called %d time(s)",
			d, tracker.renewed[d])
	}
}
