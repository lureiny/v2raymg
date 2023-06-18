package lego

import (
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/lego"
)

var certManager *lego.CertManager = nil

const certCheckCycle int64 = 5 // 5s

func InitCertManager() error {
	certManager := &lego.CertManager{
		Email:       config.GetString(common.ConfigCertEmail),
		Secrets:     config.GetStringMapString(common.ConfigCertSecrets),
		DnsProvider: config.GetString(common.ConfigCertDnsProvider),
		Path:        config.GetString(common.ConfigCertPath),
		Args:        config.GetStringSlice(common.ConfigCertArgs),
	}
	lego.CheckAndFullCertManager(certManager)
	certManager.AutoRenewCert(certCheckCycle)
	return nil
}

func GetCertManager() *lego.CertManager {
	return certManager
}
