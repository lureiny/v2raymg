package store_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

func openInboundStore(t *testing.T) *store.SQLiteInboundStore {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(db, migrations.All); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store.NewSQLiteInboundStore(db)
}

func TestInboundStore_SaveLoadGetDelete(t *testing.T) {
	s := openInboundStore(t)

	// Get nonexistent returns (nil, nil)
	gotMissing, err := s.Get("does-not-exist")
	if err != nil {
		t.Fatalf("Get missing: %v", err)
	}
	if gotMissing != nil {
		t.Fatalf("expected nil for missing inbound, got %+v", gotMissing)
	}

	// Default InboundType when empty -> "direct"
	directRec := &store.InboundRecord{
		Tag:           "vless-direct",
		ContainerType: "xray",
		CertSource:    "none",
		NativeJSON:    []byte(`{"tag":"vless-direct","port":"10001"}`),
	}
	if err := s.Save(directRec); err != nil {
		t.Fatalf("Save direct: %v", err)
	}

	chainRec := &store.InboundRecord{
		Tag:              "chain-sh",
		ContainerType:    "mihomo",
		CertSource:       "self_signed",
		CertDomain:       "example.com",
		NativeJSON:       []byte(`{"tag":"chain-sh","port":10002}`),
		InboundType:      "chain",
		AccessTags:       []string{"home-sh", "vip"},
		ChainOutboundURI: "hy2://user:pass@home.example:28443?sni=bing.com",
		Remark:           "SH Home",
	}
	if err := s.Save(chainRec); err != nil {
		t.Fatalf("Save chain: %v", err)
	}

	// Load verifies both records
	all, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}

	gotDirect, err := s.Get("vless-direct")
	if err != nil {
		t.Fatalf("Get direct: %v", err)
	}
	if gotDirect == nil {
		t.Fatal("expected vless-direct record")
	}
	if gotDirect.InboundType != "direct" {
		t.Errorf("direct InboundType = %q, want direct", gotDirect.InboundType)
	}
	if len(gotDirect.AccessTags) != 0 {
		t.Errorf("direct AccessTags = %v, want empty", gotDirect.AccessTags)
	}

	gotChain, err := s.Get("chain-sh")
	if err != nil {
		t.Fatalf("Get chain: %v", err)
	}
	if gotChain == nil {
		t.Fatal("expected chain-sh record")
	}
	if gotChain.InboundType != "chain" {
		t.Errorf("InboundType = %q, want chain", gotChain.InboundType)
	}
	if !reflect.DeepEqual(gotChain.AccessTags, []string{"home-sh", "vip"}) {
		t.Errorf("AccessTags = %v, want [home-sh vip]", gotChain.AccessTags)
	}
	if gotChain.ChainOutboundURI != chainRec.ChainOutboundURI {
		t.Errorf("ChainOutboundURI = %q, want %q", gotChain.ChainOutboundURI, chainRec.ChainOutboundURI)
	}
	if gotChain.Remark != "SH Home" {
		t.Errorf("Remark = %q, want SH Home", gotChain.Remark)
	}

	// Upsert updates access_tags back to empty
	chainRec.AccessTags = nil
	if err := s.Save(chainRec); err != nil {
		t.Fatalf("Save update: %v", err)
	}
	gotUpdated, err := s.Get("chain-sh")
	if err != nil {
		t.Fatalf("Get updated: %v", err)
	}
	if len(gotUpdated.AccessTags) != 0 {
		t.Errorf("expected empty AccessTags after clear, got %v", gotUpdated.AccessTags)
	}

	// Delete
	if err := s.Delete("chain-sh"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	gotDeleted, err := s.Get("chain-sh")
	if err != nil {
		t.Fatalf("Get after Delete: %v", err)
	}
	if gotDeleted != nil {
		t.Errorf("expected nil after Delete, got %+v", gotDeleted)
	}
}

func TestInboundStore_CorruptAccessTagsFailClosed(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "corrupt.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(db, migrations.All); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s := store.NewSQLiteInboundStore(db)

	if err := s.Save(&store.InboundRecord{
		Tag:           "private-in",
		ContainerType: "xray",
		NativeJSON:    []byte(`{"tag":"private-in"}`),
		AccessTags:    []string{"vip"},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	for _, badJSON := range []string{"{not-json", "123", `"vip"`, "null", `["vip"`} {
		if _, err := db.DB().Exec(`UPDATE inbounds SET access_tags = ? WHERE tag = 'private-in'`, badJSON); err != nil {
			t.Fatalf("UPDATE access_tags=%q: %v", badJSON, err)
		}
		if rec, err := s.Get("private-in"); err == nil || rec != nil {
			t.Errorf("Get with corrupt access_tags=%q expected fail-closed error, got rec=%+v err=%v", badJSON, rec, err)
		}
		if recs, err := s.Load(); err == nil || len(recs) > 0 {
			t.Errorf("Load with corrupt access_tags=%q expected fail-closed error, got recs=%d err=%v", badJSON, len(recs), err)
		}
	}
}
