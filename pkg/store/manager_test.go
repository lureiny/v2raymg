package store_test

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

func openTempManager(t *testing.T) *store.StoreManager {
	t.Helper()
	mgr, err := store.NewStoreManager(t.TempDir()+"/test.db", migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

func insertUserRaw(t *testing.T, mgr *store.StoreManager, username, authToken, loginPassword string) {
	t.Helper()
	_, err := mgr.DB().DB().Exec(
		`INSERT INTO users (username, auth_token, login_password) VALUES (?, ?, ?)`,
		username, authToken, loginPassword,
	)
	if err != nil {
		t.Fatalf("insertUserRaw %q: %v", username, err)
	}
}

func TestInitLoginPasswords_EmptyGetsHashed(t *testing.T) {
	mgr := openTempManager(t)
	insertUserRaw(t, mgr, "alice", "plainpass", "")

	if err := mgr.InitLoginPasswords(auth.HashLoginPassword); err != nil {
		t.Fatalf("InitLoginPasswords: %v", err)
	}

	var lp string
	row := mgr.DB().DB().QueryRow(`SELECT login_password FROM users WHERE username = 'alice'`)
	if err := row.Scan(&lp); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if lp == "" {
		t.Fatal("expected login_password to be set after init")
	}
	if !auth.VerifyLoginPassword(lp, "plainpass") {
		t.Fatal("expected original password to verify against the stored hash")
	}
}

func TestInitLoginPasswords_NonEmptySkipped(t *testing.T) {
	mgr := openTempManager(t)
	existingHash, _ := auth.HashLoginPassword("old-pass")
	insertUserRaw(t, mgr, "bob", "newpass", existingHash)

	if err := mgr.InitLoginPasswords(auth.HashLoginPassword); err != nil {
		t.Fatalf("InitLoginPasswords: %v", err)
	}

	var lp string
	mgr.DB().DB().QueryRow(`SELECT login_password FROM users WHERE username = 'bob'`).Scan(&lp)
	if !auth.VerifyLoginPassword(lp, "old-pass") {
		t.Fatal("expected pre-existing login_password to be preserved (not overwritten)")
	}
}

func TestInitLoginPasswords_Idempotent(t *testing.T) {
	mgr := openTempManager(t)
	insertUserRaw(t, mgr, "charlie", "mypassword", "")

	if err := mgr.InitLoginPasswords(auth.HashLoginPassword); err != nil {
		t.Fatalf("first InitLoginPasswords: %v", err)
	}
	var lp1 string
	mgr.DB().DB().QueryRow(`SELECT login_password FROM users WHERE username = 'charlie'`).Scan(&lp1)

	if err := mgr.InitLoginPasswords(auth.HashLoginPassword); err != nil {
		t.Fatalf("second InitLoginPasswords: %v", err)
	}
	var lp2 string
	mgr.DB().DB().QueryRow(`SELECT login_password FROM users WHERE username = 'charlie'`).Scan(&lp2)

	if lp1 != lp2 {
		t.Fatal("expected login_password unchanged on second call (idempotent)")
	}
	if !auth.VerifyLoginPassword(lp2, "mypassword") {
		t.Fatal("expected password to verify after idempotent call")
	}
}

func TestInitLoginPasswords_VerifyWithOriginal(t *testing.T) {
	mgr := openTempManager(t)
	insertUserRaw(t, mgr, "dave", "original-password", "")

	if err := mgr.InitLoginPasswords(auth.HashLoginPassword); err != nil {
		t.Fatalf("InitLoginPasswords: %v", err)
	}

	var lp string
	mgr.DB().DB().QueryRow(`SELECT login_password FROM users WHERE username = 'dave'`).Scan(&lp)

	if !auth.VerifyLoginPassword(lp, "original-password") {
		t.Fatal("expected original proxy password to verify against initialized login_password")
	}
	if auth.VerifyLoginPassword(lp, "wrong-password") {
		t.Fatal("expected wrong password to fail")
	}
}
