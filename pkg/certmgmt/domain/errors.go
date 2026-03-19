package domain

import "errors"

var (
	ErrConfigInvalid            = errors.New("cert: invalid config")
	ErrChallengeSetup           = errors.New("cert: challenge setup failed")
	ErrPortBind                 = errors.New("cert: cannot bind HTTP port")
	ErrDNSProviderNotFound      = errors.New("cert: unknown DNS provider")
	ErrDNSProviderAuth          = errors.New("cert: DNS provider auth failed")
	ErrDNSPropagationTimeout    = errors.New("cert: DNS propagation timeout")
	ErrACMERateLimited          = errors.New("cert: ACME rate limited")
	ErrACMEAccountNotRegistered = errors.New("cert: ACME account not registered")
	ErrStorageIO                = errors.New("cert: storage I/O error")
	ErrCertResourceMissing      = errors.New("cert: resource JSON missing, cannot renew")
)
