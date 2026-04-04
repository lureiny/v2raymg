package certmgmtlego

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

const (
	certsFolder = "certificates"
)

// domainToFilename converts a domain name to a safe filename component.
// Wildcard '*' is replaced with '_'.
func domainToFilename(d string) string {
	return strings.ReplaceAll(d, "*", "_")
}

// certPaths returns the file paths for a given domain under basePath.
func certPaths(basePath, d string) (crt, key, res, meta string) {
	name := domainToFilename(d)
	dir := filepath.Join(basePath, certsFolder)
	crt = filepath.Join(dir, name+".crt")
	key = filepath.Join(dir, name+".key")
	res = filepath.Join(dir, name+".json")
	meta = filepath.Join(dir, name+".meta.json")
	return
}

// atomicWrite writes data to path atomically via a temp file + rename.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SaveCert atomically persists certificate files and metadata.
func SaveCert(basePath string, record *domain.CertificateRecord, resource *domain.LegoResource) error {
	dir := filepath.Join(basePath, certsFolder)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("%w: mkdir certs: %v", domain.ErrStorageIO, err)
	}

	crtPath, keyPath, resPath, metaPath := certPaths(basePath, record.Domain)

	// Write certificate PEM
	if err := atomicWrite(crtPath, resource.CertificatePEM, 0600); err != nil {
		return fmt.Errorf("%w: write crt: %v", domain.ErrStorageIO, err)
	}

	// Write private key PEM
	if err := atomicWrite(keyPath, resource.PrivateKeyPEM, 0600); err != nil {
		return fmt.Errorf("%w: write key: %v", domain.ErrStorageIO, err)
	}

	// Write LegoResource JSON (needed for renewal)
	resBytes, err := json.MarshalIndent(resource, "", "\t")
	if err != nil {
		return fmt.Errorf("%w: marshal resource: %v", domain.ErrStorageIO, err)
	}
	if err := atomicWrite(resPath, resBytes, 0600); err != nil {
		return fmt.Errorf("%w: write resource json: %v", domain.ErrStorageIO, err)
	}

	// Fill in file paths on the record before persisting
	record.CertFile = crtPath
	record.KeyFile = keyPath
	record.ResourceFile = resPath

	// Write CertificateRecord meta JSON
	metaBytes, err := json.MarshalIndent(record, "", "\t")
	if err != nil {
		return fmt.Errorf("%w: marshal meta: %v", domain.ErrStorageIO, err)
	}
	if err := atomicWrite(metaPath, metaBytes, 0600); err != nil {
		return fmt.Errorf("%w: write meta json: %v", domain.ErrStorageIO, err)
	}

	return nil
}

// LoadCert reads the certificate record and lego resource for a domain.
func LoadCert(basePath, d string) (*domain.CertificateRecord, *domain.LegoResource, error) {
	_, _, resPath, metaPath := certPaths(basePath, d)

	metaRaw, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("%w: read meta: %v", domain.ErrStorageIO, err)
	}
	var record domain.CertificateRecord
	if err := json.Unmarshal(metaRaw, &record); err != nil {
		return nil, nil, fmt.Errorf("%w: unmarshal meta: %v", domain.ErrStorageIO, err)
	}

	resRaw, err := os.ReadFile(resPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &record, nil, nil
		}
		return nil, nil, fmt.Errorf("%w: read resource: %v", domain.ErrStorageIO, err)
	}
	var resource domain.LegoResource
	if err := json.Unmarshal(resRaw, &resource); err != nil {
		return nil, nil, fmt.Errorf("%w: unmarshal resource: %v", domain.ErrStorageIO, err)
	}

	return &record, &resource, nil
}

// DeleteCert removes all certificate files (.crt, .key, .json, .meta.json) for the given domain.
func DeleteCert(basePath, d string) error {
	crt, key, res, meta := certPaths(basePath, d)
	for _, p := range []string{crt, key, res, meta} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("%w: remove %s: %v", domain.ErrStorageIO, p, err)
		}
	}
	return nil
}

// ListCerts scans basePath/certificates/ for .meta.json files and returns all records.
func ListCerts(basePath string) ([]*domain.CertificateRecord, error) {
	dir := filepath.Join(basePath, certsFolder)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: readdir: %v", domain.ErrStorageIO, err)
	}

	var records []*domain.CertificateRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".meta.json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r domain.CertificateRecord
		if err := json.Unmarshal(raw, &r); err != nil {
			continue
		}
		records = append(records, &r)
	}
	return records, nil
}
