package server

import "github.com/lureiny/v2raymg/pkg/rpc/proto"

// CertManager is the interface required by EndNodeServer for certificate operations.
// It is satisfied by *lego.CertManager (old) and can be implemented by pkg/certmgmt adapters.
type CertManager interface {
	// ObtainNewCert issues or renews a certificate for the given domain.
	ObtainNewCert(domain string) error
	// AddCertificates imports an externally provided certificate and key.
	AddCertificates(domain string, keyData, certData []byte) error
	// GetCertInfo returns certificate metadata for the given domain, or nil if not found.
	// Returns interface{} so implementations can return any cert record type.
	GetCertInfo(domain string) interface{}
	// GetAllCert returns all managed certificates in proto format.
	GetAllCert() []*proto.Cert
}
