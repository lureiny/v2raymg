package manager

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/cmd"
	"github.com/urfave/cli/v2"
)

const (
	runCmd   = "run"
	renewCmd = "renew"
)

type CertManager struct {
	Email       string                  `json:"email"`
	Secrets     map[string]string       `json:"secrets"`
	DnsProvider string                  `json:"dns_provider"`
	Path        string                  `json:"path,omitempty"`
	Certs       map[string]*Certificate `json:"certs,omitempty"`
	certMutex   sync.Mutex
}

var defaultPath string = "./"
var subCertPath = "certificates"

type Certificate struct {
	Domain          string
	CertificateFile string
	KeyFile         string
	ExpireTime      time.Time
}

func NewCertManager(data []byte) (*CertManager, error) {
	certManager := &CertManager{
		Certs: map[string]*Certificate{},
	}
	if err := json.Unmarshal(data, certManager); err != nil {
		return nil, err
	}
	if certManager.Path == "" {
		certManager.Path = defaultPath
	}
	certManager.certMutex = sync.Mutex{}
	return certManager, nil
}

func paraseDomainCertFile(path, fileName string) *Certificate {
	if len(fileName) < 4 {
		return nil
	}
	domain := fileName[0 : len(fileName)-4]
	if strings.HasSuffix(fileName, ".crt") && !strings.HasSuffix(fileName, ".issuer.crt") {
		return &Certificate{
			Domain:          domain,
			CertificateFile: filepath.Join(path, subCertPath, fileName),
		}
	} else if strings.HasSuffix(fileName, ".key") {
		return &Certificate{
			Domain:  domain,
			KeyFile: filepath.Join(path, subCertPath, fileName),
		}
	}
	return nil
}

func mergeCertificate(cert1, cert2 *Certificate) (*Certificate, error) {
	if cert1.Domain != cert2.Domain {
		return nil, fmt.Errorf("domain is different: [%s] and [%s]", cert1.Domain, cert2.Domain)
	}
	if cert1.CertificateFile != "" && cert2.CertificateFile != "" && cert1.CertificateFile != cert2.CertificateFile {
		return nil, fmt.Errorf("get two certificate file: [%s] and [%s]", cert1.KeyFile, cert2.KeyFile)
	}
	if cert1.KeyFile != "" && cert2.KeyFile != "" && cert1.KeyFile != cert2.KeyFile {
		return nil, fmt.Errorf("get two key file: [%s] and [%s]", cert1.KeyFile, cert2.KeyFile)
	}
	certificate := &Certificate{
		Domain: cert1.Domain,
	}
	if cert1.KeyFile != "" {
		certificate.KeyFile = cert1.KeyFile
	} else {
		certificate.KeyFile = cert2.KeyFile
	}
	if cert1.CertificateFile != "" {
		certificate.CertificateFile = cert1.CertificateFile
	} else {
		certificate.CertificateFile = cert2.CertificateFile
	}
	if err := fullCertExpireTime(certificate); err != nil {
		return nil, err
	}
	return certificate, nil
}

func (certManager *CertManager) LoadCertificates() error {
	entires, err := os.ReadDir(filepath.Join(certManager.Path, subCertPath))
	if err != nil {
		return err
	}
	errMsg := ""
	for _, entry := range entires {
		cert1 := paraseDomainCertFile(certManager.Path, entry.Name())
		if cert1 == nil {
			continue
		}
		if cert2, ok := certManager.Certs[cert1.Domain]; ok {
			cert3, err := mergeCertificate(cert1, cert2)
			if err != nil {
				errMsg = fmt.Sprintf("%s\nLoad domain certificate err > %v", errMsg, err)
				continue
			}
			certManager.Certs[cert1.Domain] = cert3
		} else {
			certManager.Certs[cert1.Domain] = cert1
		}
	}
	if errMsg != "" {
		return fmt.Errorf("%v", err)
	}
	return nil
}

func SetEnvs(envs map[string]string) {
	for k, v := range envs {
		os.Setenv(strings.ToUpper(k), v)
	}
}

func (certManager *CertManager) ObtainNewCert(domain string) error {
	SetEnvs(certManager.Secrets)
	args := []string{"lego", "--accept-tos", "--email", certManager.Email, "--domains", domain, "--dns", certManager.DnsProvider, "--path", certManager.Path, runCmd}
	if err := ObtainNewCertWithDNS(args); err != nil {
		return err
	}
	cert := &Certificate{
		CertificateFile: filepath.Join(certManager.Path, subCertPath, strings.ReplaceAll(domain, "*", "_")+".crt"),
		KeyFile:         filepath.Join(certManager.Path, subCertPath, strings.ReplaceAll(domain, "*", "_")+".key"),
		Domain:          domain,
	}
	if err := fullCertExpireTime(cert); err != nil {
		return fmt.Errorf("full cert expire time err > %v", err)
	}
	certManager.certMutex.Lock()
	defer certManager.certMutex.Unlock()
	certManager.Certs[domain] = cert
	return nil
}

func (certManager *CertManager) RenewCert(domain string) error {
	certManager.certMutex.Lock()
	defer certManager.certMutex.Unlock()
	cert, ok := certManager.Certs[domain]
	if !ok {
		return fmt.Errorf("no cert of domain[%s], should Obtain new cert first", domain)
	}
	if time.Now().Before(cert.ExpireTime) {
		return nil
	}
	SetEnvs(certManager.Secrets)
	args := []string{"lego", "--email", certManager.Email, "--domains", domain, "--dns", certManager.DnsProvider, renewCmd}
	if err := RenewCert(args); err != nil {
		return err
	}
	if err := fullCertExpireTime(cert); err != nil {
		return fmt.Errorf("full cert expire time err > %v", err)
	}
	fmt.Printf("Cert of domain[%s] has been renew, new expire time is: %v", domain, cert.ExpireTime)
	return nil
}

// GetCert...
func (certManager *CertManager) GetCert(domain string) *Certificate {
	return certManager.Certs[domain]
}

// AutoRenewCert... 根据指定时间周期定时renew
func (certManager *CertManager) AutoRenewCert(cycle int64) {
	go func() {
		for {
			time.Sleep(time.Second * time.Duration(cycle))
			for domain, _ := range certManager.Certs {
				certManager.RenewCert(domain)
			}
		}
	}()
}

func RenewCert(args []string) error {
	return app.Run(args)
}

func fullCertExpireTime(ca *Certificate) error {
	cert, err := tls.LoadX509KeyPair(ca.CertificateFile, ca.KeyFile)
	if err != nil {
		return err
	}
	if c, err := x509.ParseCertificate(cert.Certificate[0]); err != nil {
		return err
	} else {
		ca.ExpireTime = c.NotAfter
	}
	return nil
}

var app *cli.App = nil

func ObtainNewCertWithDNS(args []string) error {
	return app.Run(args)
}

func initApp() {
	app := cli.NewApp()
	app.Name = "lego"
	app.HelpName = "lego"
	app.Usage = "Let's Encrypt client written in Go"
	app.EnableBashCompletion = true

	app.Version = ""
	cli.VersionPrinter = func(c *cli.Context) {
		fmt.Printf("lego version %s %s/%s\n", c.App.Version, runtime.GOOS, runtime.GOARCH)
	}

	app.Before = cmd.Before
	app.Commands = cmd.CreateCommands()
}

func initDefaultPath() {
	cwd, err := os.Getwd()
	if err == nil {
		defaultPath = filepath.Join(cwd, ".lego")
	}
}

func init() {
	initApp()
	initDefaultPath()
}
