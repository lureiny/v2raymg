package certmgmtservice_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	certmgmtservice "github.com/lureiny/v2raymg/pkg/certmgmt/service"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// writeMeta writes a CertificateRecord .meta.json file into basePath/certificates/.
func writeMeta(t *testing.T, basePath string, record *domain.CertificateRecord) {
	t.Helper()
	dir := filepath.Join(basePath, "certificates")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	name := record.Domain + ".meta.json"
	raw, err := json.MarshalIndent(record, "", "\t")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
		t.Fatalf("write meta: %v", err)
	}
}

// trackingIssuer counts Renew calls per domain.
type trackingIssuer struct {
	mu      context.Context // unused but satisfies interface
	renewed map[string]int
}

func newTrackingIssuer() *trackingIssuer {
	return &trackingIssuer{renewed: make(map[string]int)}
}

func (ti *trackingIssuer) Issue(ctx context.Context, req domain.IssueRequest) (*domain.CertificateRecord, error) {
	return nil, nil
}

func (ti *trackingIssuer) Renew(ctx context.Context, record *domain.CertificateRecord, req domain.IssueRequest) (*domain.CertificateRecord, error) {
	ti.renewed[record.Domain]++
	return record, nil
}

func (ti *trackingIssuer) Revoke(ctx context.Context, record *domain.CertificateRecord) error {
	return nil
}

func TestRenewScheduler_TriggerCondition(t *testing.T) {
	basePath := t.TempDir()

	// Certificate expiring in 10 days → should be renewed (< 30-day threshold).
	soonDomain := "soon.example.com"
	writeMeta(t, basePath, &domain.CertificateRecord{
		Domain:   soonDomain,
		NotAfter: time.Now().Add(10 * 24 * time.Hour),
	})

	// Certificate expiring in 60 days → should NOT be renewed (> 30-day threshold).
	laterDomain := "later.example.com"
	writeMeta(t, basePath, &domain.CertificateRecord{
		Domain:   laterDomain,
		NotAfter: time.Now().Add(60 * 24 * time.Hour),
	})

	tracker := newTrackingIssuer()
	cfg := certmgmtservice.Config{
		Email:           "test@example.com",
		Path:            basePath,
		RenewBeforeDays: 30,
		Challenge: certmgmtservice.ChallengeConfig{
			Type: "http01",
		},
	}
	mgr := certmgmtservice.NewManagerWithIssuer(cfg, tracker)

	// Run exactly one renew cycle.
	ctx := context.Background()
	mgr.RunRenewCycleForTest(ctx)

	if tracker.renewed[soonDomain] < 1 {
		t.Errorf("%q should have been renewed (expiring in 10 days), but Renew was not called", soonDomain)
	}
	if tracker.renewed[laterDomain] != 0 {
		t.Errorf("%q should NOT have been renewed (expiring in 60 days), but Renew was called %d time(s)",
			laterDomain, tracker.renewed[laterDomain])
	}
}
