package lego

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v2"
)

const (
	runCmd      = "run"
	renewCmd    = "renew"
	subCertPath = "certificates"
)

type CertManager struct {
	Email       string                  `json:"email"`
	Secrets     map[string]string       `json:"secrets"`
	DnsProvider string                  `json:"dns_provider"`
	Path        string                  `json:"path,omitempty"`
	Certs       map[string]*Certificate `json:"certs,omitempty"`
	certMutex   sync.Mutex
}

var execRootPath = "./"
var defaultCertPath = "./cert"
var legoPath = ".lego"

type Certificate struct {
	Domain          string
	CertificateFile string
	KeyFile         string
	ExpireTime      time.Time
}

func checkAndMakeDefaultPath() {
	if _, err := os.Stat(defaultCertPath); err != nil {
		os.Mkdir(defaultCertPath, os.ModeDir)
	}
}

func CheckAndFullCertManager(certManager *CertManager) {
	certManager.Certs = map[string]*Certificate{}

	if certManager.Path == "" {
		certManager.Path = defaultCertPath
		checkAndMakeDefaultPath()
	}
	certManager.certMutex = sync.Mutex{}
	if err := certManager.LoadCertificates(); err != nil {
		fmt.Printf("load certificate err > %v", err)
	}
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

func (certManager *CertManager) checkConfig() error {
	if certManager.Email == "" {
		return fmt.Errorf("email can't be empty")
	}
	if certManager.DnsProvider == "" {
		return fmt.Errorf("invalid dns provider: %s", certManager.DnsProvider)
	}
	return nil
}

func (certManager *CertManager) checkCertFile() {
	for _, cert := range certManager.Certs {
		certFileName := strings.ReplaceAll(cert.Domain, "*", "_") + ".crt"
		keyFileName := strings.ReplaceAll(cert.Domain, "*", "_") + ".key"
		if _, err := os.Stat(cert.KeyFile); err != nil {
			copyFile(filepath.Join(legoPath, subCertPath, keyFileName), cert.KeyFile)
		}
		if _, err := os.Stat(cert.CertificateFile); err != nil {
			copyFile(filepath.Join(legoPath, subCertPath, certFileName), cert.CertificateFile)
		}
	}
}

func (certManager *CertManager) LoadCertificates() error {
	entires, err := os.ReadDir(filepath.Join(legoPath, subCertPath))
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
	certManager.checkCertFile()
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

func copyFile(srcName, dstName string) (int64, error) {
	src, err := os.Open(srcName)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstName, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return 0, err
	}
	defer dst.Close()
	return io.Copy(dst, src)
}

func (certManager *CertManager) ObtainNewCert(domain string) error {
	certManager.certMutex.Lock()
	defer certManager.certMutex.Unlock()
	if _, ok := certManager.Certs[domain]; ok {
		return nil
	}
	if err := certManager.checkConfig(); err != nil {
		return err
	}
	if domain == "" {
		return fmt.Errorf("domian can't be empty")
	}
	SetEnvs(certManager.Secrets)
	args := []string{"lego", "--accept-tos", "--email", certManager.Email, "--domains", domain, "--dns", certManager.DnsProvider, runCmd}
	if err := ObtainNewCertWithDNS(args); err != nil {
		return err
	}
	if err := certManager.copyCertAndKeyFile(domain); err != nil {
		return err
	}
	certFileName := strings.ReplaceAll(domain, "*", "_") + ".crt"
	keyFileName := strings.ReplaceAll(domain, "*", "_") + ".key"
	cert := &Certificate{
		CertificateFile: filepath.Join(certManager.Path, certFileName),
		KeyFile:         filepath.Join(certManager.Path, keyFileName),
		Domain:          domain,
	}
	if err := fullCertExpireTime(cert); err != nil {
		return fmt.Errorf("full cert expire time err > %v", err)
	}
	certManager.Certs[domain] = cert
	return nil
}

func (certManager *CertManager) copyCertAndKeyFile(domain string) error {
	certFileName := strings.ReplaceAll(domain, "*", "_") + ".crt"
	keyFileName := strings.ReplaceAll(domain, "*", "_") + ".key"
	if _, err := copyFile(filepath.Join(legoPath, subCertPath, certFileName), filepath.Join(certManager.Path, certFileName)); err != nil {
		return err
	}
	if _, err := copyFile(filepath.Join(legoPath, subCertPath, keyFileName), filepath.Join(certManager.Path, keyFileName)); err != nil {
		return err
	}
	return nil
}

func (certManager *CertManager) RenewCert(domain string) error {
	if err := certManager.checkConfig(); err != nil {
		return err
	}
	if domain == "" {
		return fmt.Errorf("domian can't be empty")
	}
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
	if err := certManager.copyCertAndKeyFile(domain); err != nil {
		return err
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
	app.Flags = CreateFlags(legoPath)
	app.Before = Before
	app.Commands = CreateCommands()
	return app.Run(args)
}

func initApp() {
	app = cli.NewApp()
	app.Name = "lego"
	app.HelpName = "lego"
	app.Usage = "Let's Encrypt client written in Go"
	app.EnableBashCompletion = true

	app.Version = ""
	cli.VersionPrinter = func(c *cli.Context) {
		fmt.Printf("lego version %s %s/%s\n", c.App.Version, runtime.GOOS, runtime.GOARCH)
	}
}

func initCwd() {
	ex, err := os.Executable()
	if err != nil {
		panic("can't get exec path")
	}
	execRootPath = filepath.Dir(ex)
	defaultCertPath = filepath.Join(execRootPath, defaultCertPath)
	legoPath = filepath.Join(execRootPath, legoPath)
	os.Chdir(execRootPath)
}

func init() {
	initApp()
	initCwd()
}
