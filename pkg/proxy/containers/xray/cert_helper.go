// Package xray provides Xray container implementation.
package xray

import (
	"context"
	"fmt"

	certmgmtdomain "github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// CertManagerGetter abstracts the certificate manager dependency.
// It is satisfied by certmgmtservice.Manager and any compatible implementation.
type CertManagerGetter interface {
	// GetCert returns the certificate record for the given domain.
	// Returns nil if no certificate exists for the domain.
	GetCert(domain string) *certmgmtdomain.CertificateRecord
}

// CertIssuer extends CertManagerGetter with the ability to issue new certificates.
// Required by AddInboundWithCert when the certificate is not yet present.
type CertIssuer interface {
	CertManagerGetter
	Issue(ctx context.Context, domains []string) (*certmgmtdomain.CertificateRecord, error)
}

// AddInboundWithCert is a convenience wrapper around FastAddInbound that handles
// certificate acquisition automatically.
//
// Layer 1 - atomic operations (independent):
//
//	certmgmt.Manager.Issue(ctx, domains)  — obtains a certificate (may be slow)
//	Executor.FastAddInbound(tag, params)  — adds an inbound (requires cert to exist)
//
// Layer 2 - orchestration (this function):
//
//	Executor.AddInboundWithCert(ctx, tag, domain, params)
//	  → looks up or issues a certificate, then calls FastAddInbound
//
// The caller must have set a CertManagerGetter via SetCertManager before calling this.
// The certManager must also implement CertIssuer (see above).
//
// params must include at least "protocol". The "domain", "certificateFile", and
// "keyFile" keys are set automatically; any existing values are overwritten.
// If the certificate already exists, Issue is NOT called again.
func (e *Executor) AddInboundWithCert(ctx context.Context, tag, domain string, params map[string]any) error {
	if e.certManager == nil {
		return fmt.Errorf("certManager is not set; call SetCertManager before AddInboundWithCert")
	}

	issuer, ok := e.certManager.(CertIssuer)
	if !ok {
		return fmt.Errorf("certManager does not implement CertIssuer; cannot issue new certificates")
	}

	record := e.certManager.GetCert(domain)
	if record == nil {
		var err error
		record, err = issuer.Issue(ctx, []string{domain})
		if err != nil {
			return fmt.Errorf("failed to issue certificate for domain %q: %w", domain, err)
		}
	}

	params["certificateFile"] = record.CertFile
	params["keyFile"] = record.KeyFile
	params["domain"] = domain

	return e.FastAddInbound(tag, params)
}
