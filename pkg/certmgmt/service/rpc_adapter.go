package certmgmtservice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	certmgmtlego "github.com/lureiny/v2raymg/pkg/certmgmt/lego"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// GetCertFiles returns the cert and key file paths for the given domain.
// Implements pkg/http.CertReader.
func (m *Manager) GetCertFiles(domain string) (certFile, keyFile string, ok bool) {
	r := m.GetCert(domain)
	if r == nil {
		return "", "", false
	}
	return r.CertFile, r.KeyFile, true
}

// ObtainNewCert issues (or renews) a certificate for the given domain.
// Implements pkg/rpc/server.CertManager.
func (m *Manager) ObtainNewCert(d string) error {
	_, err := m.Issue(context.Background(), []string{d})
	return err
}

// AddCertificates imports an externally provided certificate and key for the given domain.
// The cert and key are written to the configured cert path and registered in the store.
// Implements pkg/rpc/server.CertManager.
func (m *Manager) AddCertificates(d string, keyData, certData []byte) error {
	if len(certData) == 0 || len(keyData) == 0 {
		return fmt.Errorf("certmgmt: AddCertificates: empty cert or key data")
	}

	certDir := filepath.Join(m.cfg.Path, d)
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return fmt.Errorf("certmgmt: AddCertificates: mkdir %s: %w", certDir, err)
	}

	certFile := filepath.Join(certDir, "cert.pem")
	keyFile := filepath.Join(certDir, "key.pem")

	if err := os.WriteFile(certFile, certData, 0600); err != nil {
		return fmt.Errorf("certmgmt: AddCertificates: write cert: %w", err)
	}
	if err := os.WriteFile(keyFile, keyData, 0600); err != nil {
		return fmt.Errorf("certmgmt: AddCertificates: write key: %w", err)
	}

	record := &domain.CertificateRecord{
		Domain:      d,
		CertFile:    certFile,
		KeyFile:     keyFile,
		ObtainedBy:  "imported",
		ObtainedAt:  time.Now(),
	}
	resource := &domain.LegoResource{
		Domain:         d,
		CertificatePEM: certData,
		PrivateKeyPEM:  keyData,
	}
	return certmgmtlego.SaveCert(m.cfg.Path, record, resource)
}

// GetCert returns the certificate record for the given domain, or nil if not found.
// The return type is interface{} to satisfy the CertManager interface without
// importing the server package. Callers only need a nil check.
// Implements pkg/rpc/server.CertManager.
func (m *Manager) GetCertInfo(d string) interface{} {
	return m.GetCert(d)
}

// GetAllCert returns all managed certificates in proto format.
// Implements pkg/rpc/server.CertManager.
func (m *Manager) GetAllCert() []*proto.Cert {
	records := m.ListCerts()
	certs := make([]*proto.Cert, 0, len(records))
	for _, r := range records {
		certs = append(certs, &proto.Cert{
			Domain:     r.Domain,
			KeyFile:    r.KeyFile,
			CertFile:   r.CertFile,
			ExpireTime: r.NotAfter.Format(time.RFC3339),
		})
	}
	return certs
}
