package certmgmtlego

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// LegoIssuer implements domain.Issuer using the lego library.
type LegoIssuer struct {
	basePath string
}

// NewLegoIssuer creates a LegoIssuer that stores accounts and certificates under basePath.
func NewLegoIssuer(basePath string) *LegoIssuer {
	return &LegoIssuer{basePath: basePath}
}

// Issue obtains a new certificate for the domains specified in req.
func (li *LegoIssuer) Issue(ctx context.Context, req domain.IssueRequest) (*domain.CertificateRecord, error) {
	if err := validateIssueRequest(req); err != nil {
		return nil, err
	}

	caURL := effectiveCAURL(req.CAURL)

	// 1. Get or create the ACME account private key.
	key, err := GetOrCreateAccountKey(li.basePath, caURL, req.Email, keyTypeFromString(req.KeyType))
	if err != nil {
		return nil, fmt.Errorf("account key: %w", err)
	}

	// 2. Load existing account registration.
	accountData, err := LoadAccount(li.basePath, caURL, req.Email)
	if err != nil {
		return nil, err
	}
	var reg *registration.Resource
	if accountData != nil {
		reg = accountData.Registration
	}

	// 3. Register the account if not yet registered.
	//    Registration must happen before certificate issuance so that the KID
	//    (account URL) is available when building the lego client.
	if reg == nil {
		regClient, regErr := NewClient(req, key, nil)
		if regErr != nil {
			return nil, regErr
		}
		reg, regErr = regClient.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if regErr != nil {
			return nil, fmt.Errorf("ACME registration: %w", regErr)
		}
		keyPEM := certcrypto.PEMEncode(key)
		_ = SaveAccount(li.basePath, caURL, req.Email, &domain.AccountData{
			Email:         req.Email,
			Registration:  reg,
			PrivateKeyPEM: keyPEM,
		})
	}

	// 4. Build the client with the registered account.
	client, err := NewClient(req, key, reg)
	if err != nil {
		return nil, err
	}

	// 5. Set up challenge provider and obtain the certificate.
	//    DNS-01: hold the global env lock during provider setup + obtain.
	//    HTTP-01: set provider then obtain.
	var certRes *certificate.Resource
	if req.Challenge.Type == domain.ChallengeDNS01 && req.Challenge.DNS != nil {
		err = WithDNSCredentials(req.Challenge.DNS.Credentials, func() error {
			p, opts, provErr := NewDNS01Provider(req.Challenge.DNS)
			if provErr != nil {
				return provErr
			}
			if setErr := client.Challenge.SetDNS01Provider(p, opts...); setErr != nil {
				return fmt.Errorf("%w: %v", domain.ErrChallengeSetup, setErr)
			}
			certRes, provErr = obtainCert(client, req)
			return provErr
		})
	} else {
		if setupErr := setupHTTPChallenge(client, req); setupErr != nil {
			return nil, setupErr
		}
		certRes, err = obtainCert(client, req)
	}
	if err != nil {
		return nil, err
	}

	return li.saveCertResult(certRes, req)
}

// Renew renews an existing certificate if it is close to expiry.
// Returns nil, nil if renewal is not yet needed.
func (li *LegoIssuer) Renew(ctx context.Context, record *domain.CertificateRecord, req domain.IssueRequest) (*domain.CertificateRecord, error) {
	renewBefore := req.RenewBeforeDays
	if renewBefore <= 0 {
		renewBefore = 30
	}
	if time.Until(record.NotAfter) > time.Duration(renewBefore)*24*time.Hour {
		return nil, nil // not yet due
	}

	// Load the lego resource required for renewal.
	_, legoRes, err := LoadCert(li.basePath, record.Domain)
	if err != nil {
		return nil, err
	}
	if legoRes == nil {
		return nil, domain.ErrCertResourceMissing
	}

	caURL := effectiveCAURL(req.CAURL)

	key, err := GetOrCreateAccountKey(li.basePath, caURL, req.Email, keyTypeFromString(req.KeyType))
	if err != nil {
		return nil, fmt.Errorf("account key: %w", err)
	}

	accountData, err := LoadAccount(li.basePath, caURL, req.Email)
	if err != nil {
		return nil, err
	}
	var reg *registration.Resource
	if accountData != nil {
		reg = accountData.Registration
	}

	client, err := NewClient(req, key, reg)
	if err != nil {
		return nil, err
	}

	// Reconstruct lego certificate.Resource for the renew call.
	existingRes := certificate.Resource{
		Domain:            legoRes.Domain,
		CertURL:           legoRes.CertURL,
		CertStableURL:     legoRes.CertStableURL,
		PrivateKey:        legoRes.PrivateKeyPEM,
		Certificate:       legoRes.CertificatePEM,
		IssuerCertificate: legoRes.IssuerCertPEM,
		CSR:               legoRes.CSRPEM,
	}

	var newCertRes *certificate.Resource
	if req.Challenge.Type == domain.ChallengeDNS01 && req.Challenge.DNS != nil {
		err = WithDNSCredentials(req.Challenge.DNS.Credentials, func() error {
			p, opts, provErr := NewDNS01Provider(req.Challenge.DNS)
			if provErr != nil {
				return provErr
			}
			if setErr := client.Challenge.SetDNS01Provider(p, opts...); setErr != nil {
				return fmt.Errorf("%w: %v", domain.ErrChallengeSetup, setErr)
			}
			newCertRes, provErr = client.Certificate.Renew(existingRes, req.Bundle, false, req.PreferredChain)
			return provErr
		})
	} else {
		if setupErr := setupHTTPChallenge(client, req); setupErr != nil {
			return nil, setupErr
		}
		newCertRes, err = client.Certificate.Renew(existingRes, req.Bundle, false, req.PreferredChain)
	}
	if err != nil {
		return nil, fmt.Errorf("renew certificate: %w", err)
	}

	notAfter, err := parseCertNotAfter(newCertRes.Certificate)
	if err != nil {
		return nil, err
	}

	newRecord := &domain.CertificateRecord{
		Domain:     record.Domain,
		NotAfter:   notAfter,
		ObtainedBy: record.ObtainedBy,
		ObtainedAt: time.Now(),
	}
	newLegoRes := legoResourceFromCertRes(newCertRes)

	if err := SaveCert(li.basePath, newRecord, newLegoRes); err != nil {
		return nil, err
	}
	return newRecord, nil
}

// Revoke revokes the certificate and removes local files.
// Note: revocation is signed by the account key. If the account key is unavailable
// (e.g. basePath or email mismatch), the ACME revocation will fail but local files
// are still removed.
func (li *LegoIssuer) Revoke(ctx context.Context, record *domain.CertificateRecord) error {
	_, legoRes, err := LoadCert(li.basePath, record.Domain)
	if err != nil {
		return err
	}
	if legoRes == nil {
		return domain.ErrCertResourceMissing
	}

	// Generate a temporary key — in production this should use the stored account key.
	// ACME allows revocation signed by the cert's own private key (RFC 8555 §7.6).
	key, err := certcrypto.ParsePEMPrivateKey(legoRes.PrivateKeyPEM)
	if err != nil {
		// Fallback: generate ephemeral key
		key, err = certcrypto.GeneratePrivateKey(certcrypto.EC256)
		if err != nil {
			return fmt.Errorf("revoke key: %w", err)
		}
	}

	req := domain.IssueRequest{CAURL: lego.LEDirectoryProduction}
	client, err := NewClient(req, key, nil)
	if err != nil {
		return err
	}

	if revokeErr := client.Certificate.Revoke(legoRes.CertificatePEM); revokeErr != nil {
		return fmt.Errorf("revoke: %w", revokeErr)
	}

	// Remove local files.
	crtPath, keyPath, resPath, metaPath := certPaths(li.basePath, record.Domain)
	for _, p := range []string{crtPath, keyPath, resPath, metaPath} {
		_ = os.Remove(p)
	}
	return nil
}

// --- helpers ---

func effectiveCAURL(caURL string) string {
	if caURL == "" {
		return lego.LEDirectoryProduction
	}
	return caURL
}

func validateIssueRequest(req domain.IssueRequest) error {
	if len(req.Domains) == 0 {
		return fmt.Errorf("%w: domains required", domain.ErrConfigInvalid)
	}
	if req.Email == "" {
		return fmt.Errorf("%w: email required", domain.ErrConfigInvalid)
	}
	if req.Challenge.Type == "" {
		return fmt.Errorf("%w: challenge type required", domain.ErrConfigInvalid)
	}
	return nil
}

func setupHTTPChallenge(client *lego.Client, req domain.IssueRequest) error {
	cfg := req.Challenge.HTTP
	if cfg == nil {
		cfg = &domain.HTTPChallengeConfig{Mode: "server", ListenAddr: ":80"}
	}
	p, err := NewHTTP01Provider(cfg)
	if err != nil {
		return err
	}
	if err := client.Challenge.SetHTTP01Provider(p); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrChallengeSetup, err)
	}
	return nil
}

func obtainCert(client *lego.Client, req domain.IssueRequest) (*certificate.Resource, error) {
	certRes, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains:        req.Domains,
		Bundle:         req.Bundle,
		PreferredChain: req.PreferredChain,
	})
	if err != nil {
		return nil, fmt.Errorf("obtain certificate: %w", err)
	}
	return certRes, nil
}

func (li *LegoIssuer) saveCertResult(certRes *certificate.Resource, req domain.IssueRequest) (*domain.CertificateRecord, error) {
	notAfter, err := parseCertNotAfter(certRes.Certificate)
	if err != nil {
		return nil, err
	}
	record := &domain.CertificateRecord{
		Domain:     req.Domains[0],
		NotAfter:   notAfter,
		ObtainedBy: string(req.Challenge.Type),
		ObtainedAt: time.Now(),
	}
	legoRes := legoResourceFromCertRes(certRes)
	if err := SaveCert(li.basePath, record, legoRes); err != nil {
		return nil, err
	}
	return record, nil
}

// parseCertNotAfter parses the NotAfter timestamp from the first certificate in a PEM bundle.
func parseCertNotAfter(certPEM []byte) (time.Time, error) {
	certs, err := certcrypto.ParsePEMBundle(certPEM)
	if err != nil || len(certs) == 0 {
		return time.Time{}, fmt.Errorf("parse certificate: %w", err)
	}
	return certs[0].NotAfter, nil
}

// legoResourceFromCertRes converts a *certificate.Resource to a *domain.LegoResource.
func legoResourceFromCertRes(res *certificate.Resource) *domain.LegoResource {
	return &domain.LegoResource{
		Domain:         res.Domain,
		CertURL:        res.CertURL,
		CertStableURL:  res.CertStableURL,
		PrivateKeyPEM:  res.PrivateKey,
		CertificatePEM: res.Certificate,
		IssuerCertPEM:  res.IssuerCertificate,
		CSRPEM:         res.CSR,
	}
}
