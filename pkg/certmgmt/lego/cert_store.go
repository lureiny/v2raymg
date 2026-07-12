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

// stageTemp writes data to a uniquely-named temp file in the same directory as
// path (so the later rename stays on one filesystem) and returns the temp path.
// A unique name (not the old fixed path+".tmp") prevents two concurrent writers
// for the same domain from clobbering each other's temp file.
func stageTemp(path string, data []byte, perm os.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Chmod(perm)
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp)
		return "", werr
	}
	return tmp, nil
}

// SaveCert persists the certificate files and metadata. It stages all four
// files as temp files FIRST, then renames them into place, so a failure while
// writing (disk full, permissions) leaves the previous live cert/key/meta set
// completely untouched instead of a persisted half-update. Each rename is
// atomic; the only residual window is the sub-millisecond gap between the key
// and crt renames on a genuine key change (import / re-issue), which POSIX
// cannot close for two separately-read files — auto-renewal reuses the old key
// (see issuer Renew), so its crt/key pair always matches regardless.
func SaveCert(basePath string, record *domain.CertificateRecord, resource *domain.LegoResource) error {
	dir := filepath.Join(basePath, certsFolder)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("%w: mkdir certs: %v", domain.ErrStorageIO, err)
	}

	crtPath, keyPath, resPath, metaPath := certPaths(basePath, record.Domain)

	// Fill in file paths on the record before marshaling it.
	record.CertFile = crtPath
	record.KeyFile = keyPath
	record.ResourceFile = resPath

	resBytes, err := json.MarshalIndent(resource, "", "\t")
	if err != nil {
		return fmt.Errorf("%w: marshal resource: %v", domain.ErrStorageIO, err)
	}
	metaBytes, err := json.MarshalIndent(record, "", "\t")
	if err != nil {
		return fmt.Errorf("%w: marshal meta: %v", domain.ErrStorageIO, err)
	}

	// Phase 1: stage every file as a temp. If any staging fails, remove the
	// temps we already created and touch no live file.
	type staged struct{ tmp, dst string }
	plan := []struct {
		dst  string
		data []byte
	}{
		{keyPath, resource.PrivateKeyPEM},
		{crtPath, resource.CertificatePEM},
		{resPath, resBytes},
		{metaPath, metaBytes},
	}
	var staged1 []staged
	for _, p := range plan {
		tmp, err := stageTemp(p.dst, p.data, 0600)
		if err != nil {
			for _, s := range staged1 {
				os.Remove(s.tmp)
			}
			return fmt.Errorf("%w: stage %s: %v", domain.ErrStorageIO, p.dst, err)
		}
		staged1 = append(staged1, staged{tmp: tmp, dst: p.dst})
	}

	// Phase 2: rename staged temps into place (key before crt so a reader that
	// keys off the crt file sees a matching key already present).
	for _, s := range staged1 {
		if err := os.Rename(s.tmp, s.dst); err != nil {
			os.Remove(s.tmp)
			return fmt.Errorf("%w: commit %s: %v", domain.ErrStorageIO, s.dst, err)
		}
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
