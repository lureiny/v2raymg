package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// UserStore is the persistence interface for users.
// A nil implementation means pure in-memory mode (no persistence).
type UserStore interface {
	// Load returns all persisted users. Called once at startup.
	Load() ([]*contracts.User, error)
	// Save upserts a user record (INSERT OR REPLACE).
	Save(user *contracts.User) error
	// Delete removes a user record by username.
	Delete(username string) error
	// Get returns a single user by username, including tombstones.
	// Returns (nil, nil) if the user does not exist.
	Get(username string) (*contracts.User, error)
	// ListByGroup returns all users with the given target_group.
	ListByGroup(group string) ([]*contracts.User, error)
	// Count returns the total number of user records.
	Count() (int, error)
}

// SQLiteUserStore implements UserStore using SQLite.
type SQLiteUserStore struct {
	db *DB
}

// NewSQLiteUserStore creates a new SQLiteUserStore backed by db.
// The users table must already exist (run Migrate with migrations.All).
func NewSQLiteUserStore(db *DB) *SQLiteUserStore {
	return &SQLiteUserStore{db: db}
}

// Load reads all user rows from the database and returns them as User objects.
// Fields not persisted (BindPorts, Extensions, Protocol, TTL) are left at zero values.
func (s *SQLiteUserStore) Load() ([]*contracts.User, error) {
	rows, err := s.db.DB().Query(`
		SELECT username, auth_token, level, expiry_time,
		       traffic_limit, upload_limit, download_limit,
		       bandwidth_upload_bps, bandwidth_download_bps,
		       max_clients, client_recycle_delay_sec, client_drain_sec,
		       port_mappings,
		       traffic_total_uplink, traffic_total_downlink,
		       role, login_password,
		       target_group, updated_at_us, origin_node, hash,
		       deletion_state
		FROM users`)
	if err != nil {
		return nil, fmt.Errorf("SQLiteUserStore.Load: %w", err)
	}
	defer rows.Close()

	var users []*contracts.User
	for rows.Next() {
		u := &contracts.User{}
		var expiryStr sql.NullString
		var portMappingsStr string

		if err := rows.Scan(
			&u.Username, &u.AuthToken, &u.Level, &expiryStr,
			&u.TrafficLimit, &u.UploadLimit, &u.DownloadLimit,
			&u.BandwidthUploadBps, &u.BandwidthDownloadBps,
			&u.MaxClients, &u.ClientRecycleDelaySec, &u.ClientDrainSec,
			&portMappingsStr,
			&u.TrafficTotalUplink, &u.TrafficTotalDownlink,
			&u.Role, &u.LoginPassword,
			&u.TargetGroup, &u.UpdatedAtUs, &u.OriginNode, &u.Hash,
			&u.DeletionState,
		); err != nil {
			return nil, fmt.Errorf("SQLiteUserStore.Load scan: %w", err)
		}

		if expiryStr.Valid && expiryStr.String != "" {
			t, parseErr := time.Parse(time.RFC3339, expiryStr.String)
			if parseErr != nil {
				// Fallback: SQLite datetime('now') format
				t, parseErr = time.Parse("2006-01-02 15:04:05", expiryStr.String)
				if parseErr != nil {
					return nil, fmt.Errorf("SQLiteUserStore.Load parse expiry_time %q: %w", expiryStr.String, parseErr)
				}
			}
			u.ExpiryTime = t
		}

		// Parse port_mappings JSON
		if portMappingsStr != "" && portMappingsStr != "{}" {
			if err := json.Unmarshal([]byte(portMappingsStr), &u.PortMappings); err != nil {
				// Log warning but don't fail
				log.Printf("SQLiteUserStore.Load: failed to parse port_mappings for user %s: %v", u.Username, err)
			}
		}

		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SQLiteUserStore.Load rows: %w", err)
	}
	return users, nil
}

// Save upserts a user record. updated_at is set to the current time.
// ExpiryTime zero value is stored as NULL.
// An empty Role is normalised to "normal" before persisting.
func (s *SQLiteUserStore) Save(user *contracts.User) error {
	var expiryArg interface{}
	if !user.ExpiryTime.IsZero() {
		expiryArg = user.ExpiryTime.UTC().Format(time.RFC3339)
	}

	// Normalize role: empty string → "normal" (the documented default).
	role := user.Role
	if role == "" {
		role = "normal"
	}

	// Serialize port_mappings to JSON
	portMappingsJSON := "{}"
	if user.PortMappings != nil && len(user.PortMappings) > 0 {
		if data, err := json.Marshal(user.PortMappings); err == nil {
			portMappingsJSON = string(data)
		}
	}

	_, err := s.db.DB().Exec(`
		INSERT OR REPLACE INTO users (
			username, auth_token, level, expiry_time,
			traffic_limit, upload_limit, download_limit,
			bandwidth_upload_bps, bandwidth_download_bps,
			max_clients, client_recycle_delay_sec, client_drain_sec,
			port_mappings,
			traffic_total_uplink, traffic_total_downlink,
			role, login_password,
			target_group, updated_at_us, origin_node, hash,
			deletion_state,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		user.Username, user.AuthToken, user.Level, expiryArg,
		user.TrafficLimit, user.UploadLimit, user.DownloadLimit,
		user.BandwidthUploadBps, user.BandwidthDownloadBps,
		user.MaxClients, user.ClientRecycleDelaySec, user.ClientDrainSec,
		portMappingsJSON,
		user.TrafficTotalUplink, user.TrafficTotalDownlink,
		role, user.LoginPassword,
		user.TargetGroup, user.UpdatedAtUs, user.OriginNode, user.Hash,
		user.DeletionState,
	)
	if err != nil {
		return fmt.Errorf("SQLiteUserStore.Save: %w", err)
	}
	return nil
}

// Delete removes the user row identified by username.
func (s *SQLiteUserStore) Delete(username string) error {
	_, err := s.db.DB().Exec(`DELETE FROM users WHERE username = ?`, username)
	if err != nil {
		return fmt.Errorf("SQLiteUserStore.Delete: %w", err)
	}
	return nil
}

// Get returns a single user by username, including tombstones (DeletionState != "").
// Returns (nil, nil) if the user does not exist.
func (s *SQLiteUserStore) Get(username string) (*contracts.User, error) {
	row := s.db.DB().QueryRow(`
		SELECT username, auth_token, level, expiry_time,
		       traffic_limit, upload_limit, download_limit,
		       bandwidth_upload_bps, bandwidth_download_bps,
		       max_clients, client_recycle_delay_sec, client_drain_sec,
		       port_mappings,
		       traffic_total_uplink, traffic_total_downlink,
		       role, login_password,
		       target_group, updated_at_us, origin_node, hash,
		       deletion_state
		FROM users WHERE username = ?`, username)

	u := &contracts.User{}
	var expiryStr sql.NullString
	var portMappingsStr string

	if err := row.Scan(
		&u.Username, &u.AuthToken, &u.Level, &expiryStr,
		&u.TrafficLimit, &u.UploadLimit, &u.DownloadLimit,
		&u.BandwidthUploadBps, &u.BandwidthDownloadBps,
		&u.MaxClients, &u.ClientRecycleDelaySec, &u.ClientDrainSec,
		&portMappingsStr,
		&u.TrafficTotalUplink, &u.TrafficTotalDownlink,
		&u.Role, &u.LoginPassword,
		&u.TargetGroup, &u.UpdatedAtUs, &u.OriginNode, &u.Hash,
		&u.DeletionState,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("SQLiteUserStore.Get: %w", err)
	}

	if expiryStr.Valid && expiryStr.String != "" {
		t, parseErr := time.Parse(time.RFC3339, expiryStr.String)
		if parseErr != nil {
			t, parseErr = time.Parse("2006-01-02 15:04:05", expiryStr.String)
			if parseErr != nil {
				return nil, fmt.Errorf("SQLiteUserStore.Get parse expiry_time %q: %w", expiryStr.String, parseErr)
			}
		}
		u.ExpiryTime = t
	}

	if portMappingsStr != "" && portMappingsStr != "{}" {
		if err := json.Unmarshal([]byte(portMappingsStr), &u.PortMappings); err != nil {
			log.Printf("SQLiteUserStore.Get: failed to parse port_mappings for user %s: %v", u.Username, err)
		}
	}

	return u, nil
}

// ListByGroup returns all users with the given target_group.
func (s *SQLiteUserStore) ListByGroup(group string) ([]*contracts.User, error) {
	rows, err := s.db.DB().Query(`
		SELECT username, auth_token, level, expiry_time,
		       traffic_limit, upload_limit, download_limit,
		       bandwidth_upload_bps, bandwidth_download_bps,
		       max_clients, client_recycle_delay_sec, client_drain_sec,
		       port_mappings,
		       traffic_total_uplink, traffic_total_downlink,
		       role, login_password,
		       target_group, updated_at_us, origin_node, hash,
		       deletion_state
		FROM users WHERE target_group = ?
		ORDER BY username`, group)
	if err != nil {
		return nil, fmt.Errorf("SQLiteUserStore.ListByGroup: %w", err)
	}
	defer rows.Close()

	var users []*contracts.User
	for rows.Next() {
		u := &contracts.User{}
		var expiryStr sql.NullString
		var portMappingsStr string

		if err := rows.Scan(
			&u.Username, &u.AuthToken, &u.Level, &expiryStr,
			&u.TrafficLimit, &u.UploadLimit, &u.DownloadLimit,
			&u.BandwidthUploadBps, &u.BandwidthDownloadBps,
			&u.MaxClients, &u.ClientRecycleDelaySec, &u.ClientDrainSec,
			&portMappingsStr,
			&u.TrafficTotalUplink, &u.TrafficTotalDownlink,
			&u.Role, &u.LoginPassword,
			&u.TargetGroup, &u.UpdatedAtUs, &u.OriginNode, &u.Hash,
			&u.DeletionState,
		); err != nil {
			return nil, fmt.Errorf("SQLiteUserStore.ListByGroup scan: %w", err)
		}

		if expiryStr.Valid && expiryStr.String != "" {
			t, parseErr := time.Parse(time.RFC3339, expiryStr.String)
			if parseErr != nil {
				t, parseErr = time.Parse("2006-01-02 15:04:05", expiryStr.String)
				if parseErr != nil {
					return nil, fmt.Errorf("SQLiteUserStore.ListByGroup parse expiry_time %q: %w", expiryStr.String, parseErr)
				}
			}
			u.ExpiryTime = t
		}

		if portMappingsStr != "" && portMappingsStr != "{}" {
			if err := json.Unmarshal([]byte(portMappingsStr), &u.PortMappings); err != nil {
				log.Printf("SQLiteUserStore.ListByGroup: failed to parse port_mappings for user %s: %v", u.Username, err)
			}
		}

		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SQLiteUserStore.ListByGroup rows: %w", err)
	}
	return users, nil
}

// Count returns the total number of user records in the database.
func (s *SQLiteUserStore) Count() (int, error) {
	var count int
	if err := s.db.DB().QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("SQLiteUserStore.Count: %w", err)
	}
	return count, nil
}
