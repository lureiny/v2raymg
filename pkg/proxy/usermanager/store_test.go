package usermanager

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(db, migrations.All); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	return db
}

func openTestStoreManager(t *testing.T) *store.StoreManager {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	storeMgr, err := store.NewStoreManager(dsn, migrations.All)
	if err != nil {
		t.Fatalf("store.NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = storeMgr.Close() })
	return storeMgr
}

func TestSQLiteUserStore_SaveAndLoad(t *testing.T) {
	s := NewSQLiteUserStore(openTestDB(t))

	user := &contracts.User{
		Username:              "alice",
		AuthToken:             "secret",
		Level:                 1,
		TrafficLimit:          1000,
		UploadLimit:           500,
		DownloadLimit:         500,
		BandwidthUploadBps:    1024,
		BandwidthDownloadBps:  2048,
		MaxClients:            10,
		ClientRecycleDelaySec: 30,
		ClientDrainSec:        5,
	}

	if err := s.Save(user); err != nil {
		t.Fatalf("Save: %v", err)
	}

	users, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}

	got := users[0]
	if got.Username != user.Username {
		t.Errorf("Username: got %q, want %q", got.Username, user.Username)
	}
	if got.AuthToken != user.AuthToken {
		t.Errorf("AuthToken: got %q, want %q", got.AuthToken, user.AuthToken)
	}
	if got.Level != user.Level {
		t.Errorf("Level: got %d, want %d", got.Level, user.Level)
	}
	if got.TrafficLimit != user.TrafficLimit {
		t.Errorf("TrafficLimit: got %d, want %d", got.TrafficLimit, user.TrafficLimit)
	}
	if got.UploadLimit != user.UploadLimit {
		t.Errorf("UploadLimit: got %d, want %d", got.UploadLimit, user.UploadLimit)
	}
	if got.DownloadLimit != user.DownloadLimit {
		t.Errorf("DownloadLimit: got %d, want %d", got.DownloadLimit, user.DownloadLimit)
	}
	if got.BandwidthUploadBps != user.BandwidthUploadBps {
		t.Errorf("BandwidthUploadBps: got %d, want %d", got.BandwidthUploadBps, user.BandwidthUploadBps)
	}
	if got.BandwidthDownloadBps != user.BandwidthDownloadBps {
		t.Errorf("BandwidthDownloadBps: got %d, want %d", got.BandwidthDownloadBps, user.BandwidthDownloadBps)
	}
	if got.MaxClients != user.MaxClients {
		t.Errorf("MaxClients: got %d, want %d", got.MaxClients, user.MaxClients)
	}
	if got.ClientRecycleDelaySec != user.ClientRecycleDelaySec {
		t.Errorf("ClientRecycleDelaySec: got %d, want %d", got.ClientRecycleDelaySec, user.ClientRecycleDelaySec)
	}
	if got.ClientDrainSec != user.ClientDrainSec {
		t.Errorf("ClientDrainSec: got %d, want %d", got.ClientDrainSec, user.ClientDrainSec)
	}
}

func TestSQLiteUserStore_Upsert(t *testing.T) {
	s := NewSQLiteUserStore(openTestDB(t))

	u1 := &contracts.User{Username: "bob", AuthToken: "pass1"}
	if err := s.Save(u1); err != nil {
		t.Fatalf("Save 1: %v", err)
	}

	u2 := &contracts.User{Username: "bob", AuthToken: "pass2"}
	if err := s.Save(u2); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	users, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].AuthToken != "pass2" {
		t.Errorf("expected auth_token=pass2, got %q", users[0].AuthToken)
	}
}

func TestSQLiteUserStore_Delete(t *testing.T) {
	s := NewSQLiteUserStore(openTestDB(t))

	user := &contracts.User{Username: "carol", AuthToken: "pass"}
	if err := s.Save(user); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Delete("carol"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	users, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users after delete, got %d", len(users))
	}
}

func TestSQLiteUserStore_ExpiryTime(t *testing.T) {
	s := NewSQLiteUserStore(openTestDB(t))

	expiry := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	user := &contracts.User{Username: "dave", AuthToken: "pass", ExpiryTime: expiry}
	if err := s.Save(user); err != nil {
		t.Fatalf("Save: %v", err)
	}

	users, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}

	loaded := users[0].ExpiryTime.UTC().Truncate(time.Second)
	if !loaded.Equal(expiry) {
		t.Errorf("ExpiryTime: got %v, want %v", loaded, expiry)
	}
}

func TestSQLiteUserStore_ZeroExpiryTime(t *testing.T) {
	s := NewSQLiteUserStore(openTestDB(t))

	user := &contracts.User{Username: "eve", AuthToken: "pass"}
	if err := s.Save(user); err != nil {
		t.Fatalf("Save: %v", err)
	}

	users, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if !users[0].ExpiryTime.IsZero() {
		t.Errorf("expected zero ExpiryTime, got %v", users[0].ExpiryTime)
	}
}
