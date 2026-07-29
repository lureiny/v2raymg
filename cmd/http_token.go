package cmd

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	"github.com/lureiny/v2raymg/pkg/store"
)

// httpTokenBytes is the entropy behind an auto-generated management token,
// matching jwt_secret's 32 bytes.
const httpTokenBytes = 32

// resolveHTTPToken returns the management API's static token, generating and
// persisting one when the config leaves it empty.
//
// An empty token is not a neutral default. getAuthHandlerFunc compares the
// X-Token header against it with no empty-value guard, so a blank token means a
// request with no header at all compares equal and /api/metrics answers
// unauthenticated. Filling it in closes that by construction; the guard itself
// is fixed separately, because a fix that only works when someone remembers to
// set a config key is not a fix.
//
// The token is stored in the database rather than regenerated per boot, unlike
// jwt_secret. A fresh jwt_secret merely forces users to log in again; a fresh
// management token would break every operator's CLI config on every restart of
// every node — and since it is per-node local auth (nothing ever compares it
// across the cluster), each node minting its own stable value is exactly right.
func resolveHTTPToken(storeMgr *store.StoreManager, cfg *appconfig.AppConfig) (string, error) {
	// An explicitly configured token always wins. Anything already in
	// local_secrets stays there, dormant, so removing the config key later
	// returns the previous generated value rather than minting a third one.
	if cfg.EndNode.HttpToken != "" {
		return cfg.EndNode.HttpToken, nil
	}

	secrets := store.NewLocalSecretStore(storeMgr.DB())
	token, created, err := secrets.GetOrCreate(store.SecretKeyHTTPToken, func() (string, error) {
		return appconfig.GenerateRandomSecret(httpTokenBytes)
	})
	if err != nil {
		return "", fmt.Errorf("http token: %w", err)
	}

	if created {
		// The only place this repo prints a credential, and deliberately so: a
		// generated token nobody can read is a management API nobody can use.
		// Printed once, on the boot that minted it — later boots read the same
		// value from the database and say nothing, so it does not accumulate in
		// log storage or scroll past unnoticed on every restart.
		log.Warn("end_node.http_token was empty; generated one and stored it in the database",
			"http_token", token,
			"hint", "record it now — it is printed only this once; set end_node.http_token to use your own")
	} else {
		log.Info("using the stored auto-generated end_node.http_token",
			"hint", "set end_node.http_token in the config to override it")
	}
	return token, nil
}
