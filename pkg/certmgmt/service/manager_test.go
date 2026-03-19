package certmgmtservice_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	certmgmtservice "github.com/lureiny/v2raymg/pkg/certmgmt/service"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// mockIssuer is a test double that records calls and enforces that Issue/Renew
// are never invoked concurrently for the same domain.
type mockIssuer struct {
	mu        sync.Mutex
	inFlight  map[string]*int32 // domain -> concurrent call count
}

func newMockIssuer() *mockIssuer {
	return &mockIssuer{inFlight: make(map[string]*int32)}
}

func (m *mockIssuer) counter(d string) *int32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.inFlight[d]; !ok {
		var v int32
		m.inFlight[d] = &v
	}
	return m.inFlight[d]
}

func (m *mockIssuer) Issue(ctx context.Context, req domain.IssueRequest) (*domain.CertificateRecord, error) {
	if len(req.Domains) == 0 {
		return nil, domain.ErrConfigInvalid
	}
	d := req.Domains[0]
	cnt := m.counter(d)
	if concurrent := atomic.AddInt32(cnt, 1); concurrent > 1 {
		atomic.AddInt32(cnt, -1)
		return nil, domain.ErrChallengeSetup // signal concurrency violation
	}
	time.Sleep(20 * time.Millisecond) // simulate work
	atomic.AddInt32(cnt, -1)
	return &domain.CertificateRecord{Domain: d, NotAfter: time.Now().Add(90 * 24 * time.Hour)}, nil
}

func (m *mockIssuer) Renew(ctx context.Context, record *domain.CertificateRecord, req domain.IssueRequest) (*domain.CertificateRecord, error) {
	return record, nil
}

func (m *mockIssuer) Revoke(ctx context.Context, record *domain.CertificateRecord) error {
	return nil
}

// TestManager_PerDomainMutex verifies that concurrent Issue calls for the same
// domain are serialised by the per-domain mutex in Manager.
func TestManager_PerDomainMutex(t *testing.T) {
	cfg := certmgmtservice.Config{
		Email: "test@example.com",
		Path:  t.TempDir(),
		Challenge: certmgmtservice.ChallengeConfig{
			Type: "http01",
		},
	}
	mgr := certmgmtservice.NewManagerWithIssuer(cfg, newMockIssuer())

	const goroutines = 5
	errCh := make(chan error, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := mgr.Issue(context.Background(), []string{"example.com"})
			errCh <- err
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("concurrent Issue returned error (possible concurrency violation): %v", err)
		}
	}
}
