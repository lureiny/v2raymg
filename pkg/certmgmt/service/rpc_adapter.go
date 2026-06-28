package certmgmtservice

import (
	"context"
	"fmt"
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
// The cert and key are persisted through SaveCert to the canonical per-domain paths under
// {Path}/certificates/ — the same on-disk layout as ACME-issued certs — so GetCertFiles
// returns those paths and a re-import overwrites in place. The expiry is parsed from the
// certificate so the record carries a real NotAfter (imported certs are skipped by
// auto-renew, but NotAfter keeps GetAllCert/expiry reporting accurate).
// Implements pkg/rpc/server.CertManager.
func (m *Manager) AddCertificates(d string, keyData, certData []byte) error {
	if len(certData) == 0 || len(keyData) == 0 {
		return fmt.Errorf("certmgmt: AddCertificates: empty cert or key data")
	}

	notAfter, err := certmgmtlego.ParseCertNotAfter(certData)
	if err != nil {
		return fmt.Errorf("certmgmt: AddCertificates: parse cert: %w", err)
	}

	record := &domain.CertificateRecord{
		Domain:     d,
		NotAfter:   notAfter,
		ObtainedBy: "imported",
		ObtainedAt: time.Now(),
	}
	resource := &domain.LegoResource{
		Domain:         d,
		CertificatePEM: certData,
		PrivateKeyPEM:  keyData,
	}
	// SaveCert writes cert/key/resource/meta and stamps record.CertFile/KeyFile to
	// the canonical {Path}/certificates/{domain}.crt|.key locations.
	return certmgmtlego.SaveCert(m.cfg.Path, record, resource)
}

// DeleteCert removes the certificate files for the given domain.
// Implements pkg/rpc/server.CertManager.
func (m *Manager) DeleteCert(d string) error {
	return certmgmtlego.DeleteCert(m.cfg.Path, d)
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
