package lego

import (
	"crypto"
	"crypto/x509"
	"math/rand"
	"os"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"
)

const (
	renewEnvAccountEmail = "LEGO_ACCOUNT_EMAIL"
	renewEnvCertDomain   = "LEGO_CERT_DOMAIN"
	renewEnvCertPath     = "LEGO_CERT_PATH"
	renewEnvCertKeyPath  = "LEGO_CERT_KEY_PATH"
	renewEnvCertPEMPath  = "LEGO_CERT_PEM_PATH"
	renewEnvCertPFXPath  = "LEGO_CERT_PFX_PATH"
)

func createRenew() *cli.Command {
	return &cli.Command{
		Name:   "renew",
		Usage:  "Renew a certificate",
		Action: renew,
		Before: func(ctx *cli.Context) error {
			// we require either domains or csr, but not both
			hasDomains := len(ctx.StringSlice("domains")) > 0
			hasCsr := len(ctx.String("csr")) > 0
			if hasDomains && hasCsr {
				logger.Debug("Please specify either --domains/-d or --csr/-c, but not both")
			}
			if !hasDomains && !hasCsr {
				logger.Debug("Please specify --domains/-d (or --csr/-c if you already have a CSR)")
			}
			return nil
		},
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "days",
				Value: 30,
				Usage: "The number of days left on a certificate to renew it.",
			},
			&cli.BoolFlag{
				Name:  "reuse-key",
				Usage: "Used to indicate you want to reuse your current private key for the new certificate.",
			},
			&cli.BoolFlag{
				Name:  "no-bundle",
				Usage: "Do not create a certificate bundle by adding the issuers certificate to the new certificate.",
			},
			&cli.BoolFlag{
				Name: "must-staple",
				Usage: "Include the OCSP must staple TLS extension in the CSR and generated certificate." +
					" Only works if the CSR is generated by lego.",
			},
			&cli.StringFlag{
				Name:  "renew-hook",
				Usage: "Define a hook. The hook is executed only when the certificates are effectively renewed.",
			},
			&cli.StringFlag{
				Name: "preferred-chain",
				Usage: "If the CA offers multiple certificate chains, prefer the chain with an issuer matching this Subject Common Name." +
					" If no match, the default offered chain will be used.",
			},
			&cli.StringFlag{
				Name:  "always-deactivate-authorizations",
				Usage: "Force the authorizations to be relinquished even if the certificate request was successful.",
			},
			&cli.BoolFlag{
				Name: "no-random-sleep",
				Usage: "Do not add a random sleep before the renewal." +
					" We do not recommend using this flag if you are doing your renewals in an automated way.",
			},
		},
	}
}

func renew(ctx *cli.Context) error {
	account, client := setup(ctx, NewAccountsStorage(ctx))
	setupChallenges(ctx, client)

	if account.Registration == nil {
		logger.Debug("Account %s is not registered. Use 'run' to register a new account.", account.Email)
	}

	certsStorage := NewCertificatesStorage(ctx)

	bundle := !ctx.Bool("no-bundle")

	meta := map[string]string{renewEnvAccountEmail: account.Email}

	// CSR
	if ctx.IsSet("csr") {
		return renewForCSR(ctx, client, certsStorage, bundle, meta)
	}

	// Domains
	return renewForDomains(ctx, client, certsStorage, bundle, meta)
}

func renewForDomains(ctx *cli.Context, client *lego.Client, certsStorage *CertificatesStorage, bundle bool, meta map[string]string) error {
	domains := ctx.StringSlice("domains")
	domain := domains[0]

	// load the cert resource from files.
	// We store the certificate, private key and metadata in different files
	// as web servers would not be able to work with a combined file.
	certificates, err := certsStorage.ReadCertificate(domain, ".crt")
	if err != nil {
		logger.Error("Error while loading the certificate for domain %s\n\t%v", domain, err)
	}

	cert := certificates[0]

	if !needRenewal(cert, domain, ctx.Int("days")) {
		return nil
	}

	// This is just meant to be informal for the user.
	timeLeft := cert.NotAfter.Sub(time.Now().UTC())
	logger.Debug("[%s] acme: Trying renewal with %d hours remaining", domain, int(timeLeft.Hours()))

	certDomains := certcrypto.ExtractDomains(cert)

	var privateKey crypto.PrivateKey
	if ctx.Bool("reuse-key") {
		keyBytes, errR := certsStorage.ReadFile(domain, ".key")
		if errR != nil {
			logger.Error("Error while loading the private key for domain %s\n\t%v", domain, errR)
		}

		privateKey, errR = certcrypto.ParsePEMPrivateKey(keyBytes)
		if errR != nil {
			return errR
		}
	}

	// https://github.com/go-acme/lego/issues/1656
	// https://github.com/certbot/certbot/blob/284023a1b7672be2bd4018dd7623b3b92197d4b0/certbot/certbot/_internal/renewal.py#L435-L440
	if !isatty.IsTerminal(os.Stdout.Fd()) && !ctx.Bool("no-random-sleep") {
		// https://github.com/certbot/certbot/blob/284023a1b7672be2bd4018dd7623b3b92197d4b0/certbot/certbot/_internal/renewal.py#L472
		const jitter = 8 * time.Minute
		rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
		sleepTime := time.Duration(rnd.Int63n(int64(jitter)))

		logger.Info("renewal: random delay of %s", sleepTime)
		time.Sleep(sleepTime)
	}

	request := certificate.ObtainRequest{
		Domains:                        merge(certDomains, domains),
		Bundle:                         bundle,
		PrivateKey:                     privateKey,
		MustStaple:                     ctx.Bool("must-staple"),
		PreferredChain:                 ctx.String("preferred-chain"),
		AlwaysDeactivateAuthorizations: ctx.Bool("always-deactivate-authorizations"),
	}
	certRes, err := client.Certificate.Obtain(request)
	if err != nil {
		logger.Error("%v", err)
		return err
	}

	certsStorage.SaveResource(certRes)

	meta[renewEnvCertDomain] = domain
	meta[renewEnvCertPath] = certsStorage.GetFileName(domain, ".crt")
	meta[renewEnvCertKeyPath] = certsStorage.GetFileName(domain, ".key")
	meta[renewEnvCertPEMPath] = certsStorage.GetFileName(domain, ".pem")
	meta[renewEnvCertPFXPath] = certsStorage.GetFileName(domain, ".pfx")

	return launchHook(ctx.String("renew-hook"), meta)
}

func renewForCSR(ctx *cli.Context, client *lego.Client, certsStorage *CertificatesStorage, bundle bool, meta map[string]string) error {
	csr, err := readCSRFile(ctx.String("csr"))
	if err != nil {
		logger.Error("%v", err)
		return err
	}

	domain := csr.Subject.CommonName

	// load the cert resource from files.
	// We store the certificate, private key and metadata in different files
	// as web servers would not be able to work with a combined file.
	certificates, err := certsStorage.ReadCertificate(domain, ".crt")
	if err != nil {
		logger.Error("Error while loading the certificate for domain %s\n\t%v", domain, err)
	}

	cert := certificates[0]

	if !needRenewal(cert, domain, ctx.Int("days")) {
		return nil
	}

	// This is just meant to be informal for the user.
	timeLeft := cert.NotAfter.Sub(time.Now().UTC())
	logger.Info("[%s] acme: Trying renewal with %d hours remaining", domain, int(timeLeft.Hours()))

	certRes, err := client.Certificate.ObtainForCSR(certificate.ObtainForCSRRequest{
		CSR:                            csr,
		Bundle:                         bundle,
		PreferredChain:                 ctx.String("preferred-chain"),
		AlwaysDeactivateAuthorizations: ctx.Bool("always-deactivate-authorizations"),
	})
	if err != nil {
		logger.Error("%v", err)
	}

	certsStorage.SaveResource(certRes)

	meta[renewEnvCertDomain] = domain
	meta[renewEnvCertPath] = certsStorage.GetFileName(domain, ".crt")
	meta[renewEnvCertKeyPath] = certsStorage.GetFileName(domain, ".key")

	return launchHook(ctx.String("renew-hook"), meta)
}

func needRenewal(x509Cert *x509.Certificate, domain string, days int) bool {
	if x509Cert.IsCA {
		logger.Debug("[%s] Certificate bundle starts with a CA certificate", domain)
	}

	if days >= 0 {
		notAfter := int(time.Until(x509Cert.NotAfter).Hours() / 24.0)
		if notAfter > days {
			logger.Debug("[%s] The certificate expires in %d days, the number of days defined to perform the renewal is %d: no renewal.",
				domain, notAfter, days)
			return false
		}
	}

	return true
}

func merge(prevDomains, nextDomains []string) []string {
	for _, next := range nextDomains {
		var found bool
		for _, prev := range prevDomains {
			if prev == next {
				found = true
				break
			}
		}
		if !found {
			prevDomains = append(prevDomains, next)
		}
	}
	return prevDomains
}
