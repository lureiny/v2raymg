package params

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ---------- fakes ----------

type fakeCertMgr struct {
	records map[string]*CertRecord
}

func (f *fakeCertMgr) GetCert(d string) *CertRecord {
	if f.records == nil {
		return nil
	}
	return f.records[d]
}

// ---------- credential fill ----------

func TestFillDefaults_VMess_FillsMissingUUID(t *testing.T) {
	p := map[string]any{}
	if err := FillDefaults(p, "vmess", nil, ""); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	got, _ := p["uuid"].(string)
	if _, err := uuid.Parse(got); err != nil {
		t.Fatalf("uuid not a valid UUIDv4 (%q): %v", got, err)
	}
}

func TestFillDefaults_VMess_KeepsExistingUUID(t *testing.T) {
	existing := uuid.NewString()
	p := map[string]any{"uuid": existing}
	if err := FillDefaults(p, "vmess", nil, ""); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	if p["uuid"] != existing {
		t.Fatalf("existing uuid overwritten: got %q want %q", p["uuid"], existing)
	}
}

func TestFillDefaults_Shadowsocks_DefaultsPasswordAndCipher(t *testing.T) {
	p := map[string]any{}
	if err := FillDefaults(p, "shadowsocks", nil, ""); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	pw, _ := p["password"].(string)
	if pw == "" {
		t.Fatalf("password not filled")
	}
	if p["cipher"] != "aes-256-gcm" {
		t.Fatalf("cipher default mismatch: got %v", p["cipher"])
	}
}

func TestFillDefaults_Shadowsocks_SS_Alias(t *testing.T) {
	p := map[string]any{}
	if err := FillDefaults(p, "ss", nil, ""); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	if p["cipher"] != "aes-256-gcm" {
		t.Fatalf("ss alias should default cipher; got %v", p["cipher"])
	}
}

func TestFillDefaults_VLess_NoFill(t *testing.T) {
	// vless is not in the MVP — fillCredentials must no-op, no error.
	p := map[string]any{}
	if err := FillDefaults(p, "vless", nil, ""); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	if _, ok := p["uuid"]; ok {
		t.Fatalf("vless should not get an auto-filled uuid in this layer")
	}
}

// ---------- trojan TLS gate ----------

func TestFillDefaults_Trojan_NoCertSource_Rejected(t *testing.T) {
	p := map[string]any{}
	err := FillDefaults(p, "trojan", nil, "")
	if err == nil {
		t.Fatalf("trojan without cert source must error")
	}
	// Password still fills even if TLS resolve fails? No — TLS check runs
	// after credential fill. Password should be present, then the error
	// originates from resolveCertSource. Assert the error mentions the
	// available sources so users know what to provide.
	if !strings.Contains(err.Error(), "cert_file") ||
		!strings.Contains(err.Error(), "domain") ||
		!strings.Contains(err.Error(), "self_signed") {
		t.Fatalf("error message should enumerate cert sources, got: %v", err)
	}
}

// ---------- cert source 1: explicit paths ----------

func TestFillDefaults_Trojan_CertFileKeyFile_PassThrough(t *testing.T) {
	p := map[string]any{
		"cert_file": "/abs/cert.pem",
		"key_file":  "/abs/key.pem",
	}
	if err := FillDefaults(p, "trojan", nil, ""); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	if p["cert_file"] != "/abs/cert.pem" || p["key_file"] != "/abs/key.pem" {
		t.Fatalf("explicit paths must pass through unchanged")
	}
}

func TestFillDefaults_Trojan_UnpairedCertFileRejected(t *testing.T) {
	p := map[string]any{"cert_file": "/abs/cert.pem"}
	if err := FillDefaults(p, "trojan", nil, ""); err == nil {
		t.Fatalf("cert_file alone must be rejected")
	}
}

// ---------- cert source 2: PEM content ----------

func TestFillDefaults_Trojan_PEMContent_WrittenToScratch(t *testing.T) {
	dir := t.TempDir()
	// Use a valid-looking PEM block so the file content round-trips —
	// FillDefaults doesn't validate the PEM, but using real-shaped data
	// catches "we accidentally wrote the wrong string" bugs.
	certPEM, keyPEM, err := generateSelfSignedPEM("unit-test-cn")
	if err != nil {
		t.Fatalf("generate test cert: %v", err)
	}
	p := map[string]any{
		"certificate": certPEM,
		"key":         keyPEM,
	}
	if err := FillDefaults(p, "trojan", nil, dir); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	cf, _ := p["cert_file"].(string)
	kf, _ := p["key_file"].(string)
	if cf == "" || kf == "" {
		t.Fatalf("cert_file/key_file not populated: %v", p)
	}
	if !strings.HasPrefix(cf, dir) || !strings.HasPrefix(kf, dir) {
		t.Fatalf("cert files not under scratchDir: cf=%s kf=%s dir=%s", cf, kf, dir)
	}
	got, err := os.ReadFile(cf)
	if err != nil {
		t.Fatalf("read cert file: %v", err)
	}
	if string(got) != certPEM {
		t.Fatalf("cert content mismatch")
	}
}

func TestFillDefaults_Trojan_PEM_Unpaired_Rejected(t *testing.T) {
	p := map[string]any{"certificate": "just the cert"}
	err := FillDefaults(p, "trojan", nil, t.TempDir())
	if err == nil {
		t.Fatalf("unpaired certificate must be rejected")
	}
	p = map[string]any{"key": "just the key"}
	err = FillDefaults(p, "trojan", nil, t.TempDir())
	if err == nil {
		t.Fatalf("unpaired key must be rejected")
	}
}

func TestFillDefaults_Trojan_PEM_Idempotent(t *testing.T) {
	// Same PEM content → same file names (hashShort-based), so a second
	// fill doesn't multiply scratch files. This matters when FastAddInbound
	// is replayed during Restore.
	dir := t.TempDir()
	certPEM, keyPEM, _ := generateSelfSignedPEM("idem")
	p1 := map[string]any{"certificate": certPEM, "key": keyPEM}
	p2 := map[string]any{"certificate": certPEM, "key": keyPEM}
	if err := FillDefaults(p1, "trojan", nil, dir); err != nil {
		t.Fatal(err)
	}
	if err := FillDefaults(p2, "trojan", nil, dir); err != nil {
		t.Fatal(err)
	}
	if p1["cert_file"] != p2["cert_file"] {
		t.Fatalf("same PEM should hash to same cert_file: %v vs %v", p1["cert_file"], p2["cert_file"])
	}
}

// ---------- cert source 3: domain via CertManager ----------

func TestFillDefaults_Trojan_Domain_Resolved(t *testing.T) {
	mgr := &fakeCertMgr{records: map[string]*CertRecord{
		"example.com": {CertFile: "/cm/example.com/cert.pem", KeyFile: "/cm/example.com/key.pem"},
	}}
	p := map[string]any{"domain": "example.com"}
	if err := FillDefaults(p, "trojan", mgr, ""); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	if p["cert_file"] != "/cm/example.com/cert.pem" {
		t.Fatalf("cert_file not from cert manager: %v", p["cert_file"])
	}
	if p["key_file"] != "/cm/example.com/key.pem" {
		t.Fatalf("key_file not from cert manager: %v", p["key_file"])
	}
}

func TestFillDefaults_Trojan_Domain_NoCertMgr_Rejected(t *testing.T) {
	p := map[string]any{"domain": "example.com"}
	err := FillDefaults(p, "trojan", nil, "")
	if err == nil || !strings.Contains(err.Error(), "CertManager") {
		t.Fatalf("domain without CertManager must produce a clear error; got: %v", err)
	}
}

func TestFillDefaults_Trojan_Domain_NotFound_Rejected(t *testing.T) {
	mgr := &fakeCertMgr{records: map[string]*CertRecord{}}
	p := map[string]any{"domain": "missing.example.com"}
	err := FillDefaults(p, "trojan", mgr, "")
	if err == nil || !strings.Contains(err.Error(), "no certificate found") {
		t.Fatalf("missing domain must error with clear message; got: %v", err)
	}
}

// ---------- cert source 4: self-signed ----------

func TestFillDefaults_Trojan_SelfSigned_GeneratesFiles(t *testing.T) {
	dir := t.TempDir()
	p := map[string]any{"self_signed": true, "server_name": "unit.example"}
	if err := FillDefaults(p, "trojan", nil, dir); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	cf, _ := p["cert_file"].(string)
	kf, _ := p["key_file"].(string)
	if cf == "" || kf == "" {
		t.Fatalf("self-signed did not populate cert_file/key_file")
	}
	if filepath.Dir(cf) != dir {
		t.Fatalf("self-signed cert not under scratchDir")
	}
	// Files must actually exist and be non-empty.
	if info, err := os.Stat(cf); err != nil || info.Size() == 0 {
		t.Fatalf("self-signed cert missing/empty: %v", err)
	}
	if info, err := os.Stat(kf); err != nil || info.Size() == 0 {
		t.Fatalf("self-signed key missing/empty: %v", err)
	}
}

func TestFillDefaults_Trojan_SelfSigned_DefaultCN(t *testing.T) {
	dir := t.TempDir()
	p := map[string]any{"self_signed": true}
	if err := FillDefaults(p, "trojan", nil, dir); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	cf, _ := p["cert_file"].(string)
	if !strings.Contains(cf, "localhost") {
		t.Fatalf("default CN should land in file name: %s", cf)
	}
}

// ---------- cert source priority ----------

func TestFillDefaults_Trojan_PathsWinOverPEM(t *testing.T) {
	dir := t.TempDir()
	// Caller gave both paths and PEM — paths win, PEM untouched and no
	// scratch file written.
	p := map[string]any{
		"cert_file":   "/trusted/cert.pem",
		"key_file":    "/trusted/key.pem",
		"certificate": "IGNORED",
		"key":         "IGNORED",
	}
	if err := FillDefaults(p, "trojan", nil, dir); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	if p["cert_file"] != "/trusted/cert.pem" {
		t.Fatalf("paths should take priority, got %v", p["cert_file"])
	}
	// scratchDir should be empty since we didn't materialise anything.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("no scratch files should be written when paths win; got %d", len(entries))
	}
}

// ---------- non-TLS protocols ----------

func TestFillDefaults_VMess_NoCertRequired(t *testing.T) {
	// vmess is TLS-optional at the v2raymg layer (mihomo listener type
	// vmess doesn't require cert); resolveCertSource must not fire.
	p := map[string]any{}
	if err := FillDefaults(p, "vmess", nil, ""); err != nil {
		t.Fatalf("vmess should not require a cert source; got: %v", err)
	}
}

func TestFillDefaults_Shadowsocks_NoCertRequired(t *testing.T) {
	p := map[string]any{}
	if err := FillDefaults(p, "shadowsocks", nil, ""); err != nil {
		t.Fatalf("shadowsocks should not require a cert source; got: %v", err)
	}
}

func TestFillDefaults_NilParamsRejected(t *testing.T) {
	if err := FillDefaults(nil, "vmess", nil, ""); err == nil {
		t.Fatalf("nil params must error")
	}
}
