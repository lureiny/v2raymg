package certmgmtlego

import (
	"crypto"
	"fmt"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// acmeUser implements registration.User for a lego client.
type acmeUser struct {
	email        string
	registration *registration.Resource
	key          crypto.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.registration }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

// keyTypeFromString maps the req.KeyType string to a certcrypto.KeyType constant.
func keyTypeFromString(s string) certcrypto.KeyType {
	switch s {
	case "rsa2048":
		return certcrypto.RSA2048
	case "rsa4096":
		return certcrypto.RSA4096
	case "rsa8192":
		return certcrypto.RSA8192
	case "ec384":
		return certcrypto.EC384
	default: // "ec256" or empty
		return certcrypto.EC256
	}
}

// NewClient constructs a lego.Client from an IssueRequest and an existing private key / registration.
func NewClient(req domain.IssueRequest, privateKey crypto.PrivateKey, reg *registration.Resource) (*lego.Client, error) {
	user := &acmeUser{
		email:        req.Email,
		registration: reg,
		key:          privateKey,
	}

	caURL := req.CAURL
	if caURL == "" {
		caURL = lego.LEDirectoryProduction
	}

	cfg := lego.NewConfig(user)
	cfg.CADirURL = caURL
	cfg.Certificate.KeyType = keyTypeFromString(req.KeyType)
	cfg.Certificate.Timeout = 30 * time.Second

	client, err := lego.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create lego client: %w", err)
	}
	return client, nil
}
