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
	Certs       map[string]*Certificate `json:"certs,omitempty"`
}

var defaultPath string = "./"

type Certificate struct {
	Domain          string
	CertificateFile string
	KeyFile         string
	ExpireTime      time.Time
}

func NewCertManager(data []byte) (*CertManager, error) {
	acmeManager := &CertManager{
		Certs: map[string]*Certificate{},
	}
	err := json.Unmarshal(data, acmeManager)
	return acmeManager, err
}

func SetEnv(envs map[string]string) {
	for k, v := range envs {
		os.Setenv(strings.ToUpper(k), v)
	}
}

func (certManager *CertManager) ObtainNewCert(domain, path string) error {
	if path == "" {
		path = defaultPath
	}
	args := []string{"lego", "--accept-tos", "--email", certManager.Email, "--domains", domain, "--dns", certManager.DnsProvider, "--path", path, runCmd}
	if err := ObtainNewCertWithDNS(args); err != nil {
		return err
	}
	cert := &Certificate{
		CertificateFile: filepath.Join(path, strings.ReplaceAll(domain, "*", "_")+".crt"),
		KeyFile:         filepath.Join(path, strings.ReplaceAll(domain, "*", "_")+".key"),
		Domain:          domain,
	}
	if err := fullCertExpireTime(cert); err != nil {
		return fmt.Errorf("full cert expire time err > %v", err)
	}
	certManager.Certs[domain] = cert
	return nil
}

func (certManager *CertManager) RenewCert(domain string) error {
	cert, ok := certManager.Certs[domain]
	if !ok {
		return fmt.Errorf("no cert of domain[%s], should Obtain new cert first", domain)
	}
	if time.Now().Before(cert.ExpireTime) {
		return nil
	}
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
}
