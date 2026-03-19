// Package xray provides Xray container implementation.
package xray

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

// openTestDB opens a temporary SQLite DB and applies all migrations.
func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.Migrate(db, migrations.All); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLiteInboundStore_SaveAndLoad(t *testing.T) {
	db := openTestDB(t)
	s := NewSQLiteInboundStore(db)

	raw := []byte(`{"tag":"test","protocol":"vmess","port":10000}`)
	rec := &InboundRecord{
		Tag:           "test",
		ContainerType: "xray",
		CertSource:    "none",
		CertDomain:    "",
		NativeJSON:    raw,
	}

	if err := s.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	records, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	got := records[0]
	if got.Tag != "test" {
		t.Errorf("Tag = %q, want %q", got.Tag, "test")
	}
	if got.ContainerType != "xray" {
		t.Errorf("ContainerType = %q, want %q", got.ContainerType, "xray")
	}
	if got.CertSource != "none" {
		t.Errorf("CertSource = %q, want %q", got.CertSource, "none")
	}
	if got.CertDomain != "" {
		t.Errorf("CertDomain = %q, want %q", got.CertDomain, "")
	}

	// Verify native_json is formatted JSON (contains newlines)
	if !bytes.Contains(got.NativeJSON, []byte("\n")) {
		t.Errorf("native_json should be pretty-printed (contain newlines), got: %s", got.NativeJSON)
	}
}

func TestSQLiteInboundStore_Upsert(t *testing.T) {
	db := openTestDB(t)
	s := NewSQLiteInboundStore(db)

	rec1 := &InboundRecord{
		Tag:           "upsert-tag",
		ContainerType: "xray",
		CertSource:    "none",
		NativeJSON:    []byte(`{"tag":"upsert-tag","port":1000}`),
	}
	rec2 := &InboundRecord{
		Tag:           "upsert-tag",
		ContainerType: "xray",
		CertSource:    "file",
		NativeJSON:    []byte(`{"tag":"upsert-tag","port":2000}`),
	}

	if err := s.Save(rec1); err != nil {
		t.Fatalf("Save (1st): %v", err)
	}
	if err := s.Save(rec2); err != nil {
		t.Fatalf("Save (2nd): %v", err)
	}

	records, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record after upsert, got %d", len(records))
	}
	// Should reflect the latest save
	if records[0].CertSource != "file" {
		t.Errorf("CertSource = %q, want %q", records[0].CertSource, "file")
	}
	if !bytes.Contains(records[0].NativeJSON, []byte("2000")) {
		t.Errorf("NativeJSON should contain updated port 2000, got: %s", records[0].NativeJSON)
	}
}

func TestSQLiteInboundStore_Delete(t *testing.T) {
	db := openTestDB(t)
	s := NewSQLiteInboundStore(db)

	rec := &InboundRecord{
		Tag:           "delete-me",
		ContainerType: "xray",
		CertSource:    "none",
		NativeJSON:    []byte(`{"tag":"delete-me"}`),
	}

	if err := s.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Delete("delete-me"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	records, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records after delete, got %d", len(records))
	}
}

func TestSQLiteInboundStore_CertSource(t *testing.T) {
	db := openTestDB(t)
	s := NewSQLiteInboundStore(db)

	records := []*InboundRecord{
		{
			Tag:           "domain-inbound",
			ContainerType: "xray",
			CertSource:    "domain",
			CertDomain:    "example.com",
			NativeJSON:    []byte(`{"tag":"domain-inbound","port":443}`),
		},
		{
			Tag:           "selfsigned-inbound",
			ContainerType: "xray",
			CertSource:    "self_signed",
			CertDomain:    "",
			NativeJSON:    []byte(`{"tag":"selfsigned-inbound","port":8443}`),
		},
	}

	for _, rec := range records {
		if err := s.Save(rec); err != nil {
			t.Fatalf("Save %s: %v", rec.Tag, err)
		}
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 records, got %d", len(loaded))
	}

	// Build a map for easy lookup
	byTag := make(map[string]*InboundRecord, len(loaded))
	for _, r := range loaded {
		byTag[r.Tag] = r
	}

	if r := byTag["domain-inbound"]; r == nil {
		t.Error("domain-inbound not found")
	} else {
		if r.CertSource != "domain" {
			t.Errorf("domain-inbound CertSource = %q, want %q", r.CertSource, "domain")
		}
		if r.CertDomain != "example.com" {
			t.Errorf("domain-inbound CertDomain = %q, want %q", r.CertDomain, "example.com")
		}
	}

	if r := byTag["selfsigned-inbound"]; r == nil {
		t.Error("selfsigned-inbound not found")
	} else {
		if r.CertSource != "self_signed" {
			t.Errorf("selfsigned-inbound CertSource = %q, want %q", r.CertSource, "self_signed")
		}
		if r.CertDomain != "" {
			t.Errorf("selfsigned-inbound CertDomain = %q, want %q", r.CertDomain, "")
		}
	}
}
