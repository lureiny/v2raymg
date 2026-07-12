package certmgmtservice

import (
	"context"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// blockingIssuer blocks inside Issue until released, holding the Manager's
// per-domain lock so a concurrent AddCertificates can be observed waiting.
type blockingIssuer struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockingIssuer) Issue(ctx context.Context, req domain.IssueRequest) (*domain.CertificateRecord, error) {
	close(b.entered)
	<-b.release
	return &domain.CertificateRecord{Domain: req.Domains[0], NotAfter: time.Now().Add(90 * 24 * time.Hour)}, nil
}
func (b *blockingIssuer) Renew(ctx context.Context, r *domain.CertificateRecord, req domain.IssueRequest) (*domain.CertificateRecord, error) {
	return r, nil
}
func (b *blockingIssuer) Revoke(ctx context.Context, r *domain.CertificateRecord) error { return nil }

// TestAddCertificates_SerializedWithDomainLock covers finding #2: AddCertificates
// must take the same per-domain lock as Issue/RenewDomain. With Issue blocking
// while it holds the lock, a concurrent AddCertificates for the same domain must
// block until Issue completes.
func TestAddCertificates_SerializedWithDomainLock(t *testing.T) {
	bi := &blockingIssuer{entered: make(chan struct{}), release: make(chan struct{})}
	mgr := NewManagerWithIssuer(Config{
		Email:     "test@example.com",
		Path:      t.TempDir(),
		Challenge: ChallengeConfig{Type: "http01"},
	}, bi)

	go func() { _, _ = mgr.Issue(context.Background(), []string{"example.com"}) }()
	<-bi.entered // Issue now holds the example.com lock

	addDone := make(chan error, 1)
	go func() {
		addDone <- mgr.AddCertificates("example.com", []byte("KEYDATA"), []byte("CERTDATA"))
	}()

	// AddCertificates must be blocked on the domain lock, not complete.
	select {
	case <-addDone:
		t.Fatal("AddCertificates completed while Issue held the domain lock (not serialized)")
	case <-time.After(150 * time.Millisecond):
	}

	close(bi.release) // let Issue finish and drop the lock
	select {
	case err := <-addDone:
		// It may still fail parsing the fake cert; we only care that it UNBLOCKED.
		_ = err
	case <-time.After(2 * time.Second):
		t.Fatal("AddCertificates did not proceed after the lock was released")
	}
}
