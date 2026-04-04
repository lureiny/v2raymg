package migrations

import "github.com/lureiny/v2raymg/pkg/store"

// All contains all schema migrations in version order.
// Version 1: users table for persistent user storage.
// Version 2: inbounds table for persistent inbound storage.
// Version 3: add port_mappings column to users table for port persistence.
// Version 4: drop enable column from users table (user state is active or deleting only).
// Version 5: add lifetime cumulative traffic columns to users table.
var All = []store.Migration{
	{
		Version: 1,
		SQL: `CREATE TABLE IF NOT EXISTS users (
            username                 TEXT PRIMARY KEY,
            password                 TEXT NOT NULL,
            level                    INTEGER NOT NULL DEFAULT 0,
            enable                   INTEGER NOT NULL DEFAULT 1,
            expiry_time              DATETIME,
            traffic_limit            INTEGER NOT NULL DEFAULT 0,
            upload_limit             INTEGER NOT NULL DEFAULT 0,
            download_limit           INTEGER NOT NULL DEFAULT 0,
            bandwidth_upload_bps     INTEGER NOT NULL DEFAULT 0,
            bandwidth_download_bps   INTEGER NOT NULL DEFAULT 0,
            max_clients              INTEGER NOT NULL DEFAULT 0,
            client_recycle_delay_sec INTEGER NOT NULL DEFAULT 0,
            client_drain_sec         INTEGER NOT NULL DEFAULT 0,
            created_at               DATETIME NOT NULL DEFAULT (datetime('now')),
            updated_at               DATETIME NOT NULL DEFAULT (datetime('now'))
        )`,
	},
	{
		Version: 2,
		SQL: `CREATE TABLE IF NOT EXISTS inbounds (
            tag            TEXT PRIMARY KEY,
            container_type TEXT NOT NULL,
            cert_source    TEXT NOT NULL DEFAULT 'none',
            cert_domain    TEXT NOT NULL DEFAULT '',
            native_json    TEXT NOT NULL,
            created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
            updated_at     DATETIME NOT NULL DEFAULT (datetime('now'))
        )`,
	},
	{
		Version: 3,
		SQL: `ALTER TABLE users ADD COLUMN port_mappings TEXT NOT NULL DEFAULT '{}'`,
	},
	{
		Version: 4,
		SQL: `CREATE TABLE users_new (
            username                 TEXT PRIMARY KEY,
            password                 TEXT NOT NULL,
            level                    INTEGER NOT NULL DEFAULT 0,
            expiry_time              DATETIME,
            traffic_limit            INTEGER NOT NULL DEFAULT 0,
            upload_limit             INTEGER NOT NULL DEFAULT 0,
            download_limit           INTEGER NOT NULL DEFAULT 0,
            bandwidth_upload_bps     INTEGER NOT NULL DEFAULT 0,
            bandwidth_download_bps   INTEGER NOT NULL DEFAULT 0,
            max_clients              INTEGER NOT NULL DEFAULT 0,
            client_recycle_delay_sec INTEGER NOT NULL DEFAULT 0,
            client_drain_sec         INTEGER NOT NULL DEFAULT 0,
            port_mappings            TEXT NOT NULL DEFAULT '{}',
            created_at               DATETIME NOT NULL DEFAULT (datetime('now')),
            updated_at               DATETIME NOT NULL DEFAULT (datetime('now'))
        );
        INSERT INTO users_new SELECT username, password, level, expiry_time,
            traffic_limit, upload_limit, download_limit,
            bandwidth_upload_bps, bandwidth_download_bps,
            max_clients, client_recycle_delay_sec, client_drain_sec,
            port_mappings, created_at, updated_at
        FROM users;
        DROP TABLE users;
        ALTER TABLE users_new RENAME TO users;`,
	},
	{
		Version: 5,
		SQL: `ALTER TABLE users ADD COLUMN traffic_total_uplink INTEGER NOT NULL DEFAULT 0;
        ALTER TABLE users ADD COLUMN traffic_total_downlink INTEGER NOT NULL DEFAULT 0;`,
	},
	{
		Version: 6,
		SQL: `ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'normal';
ALTER TABLE users ADD COLUMN login_password TEXT NOT NULL DEFAULT '';`,
	},
	{
		Version: 7,
		SQL: `CREATE TABLE IF NOT EXISTS cluster_users (
    username TEXT PRIMARY KEY,
    password TEXT NOT NULL,
    expire INTEGER NOT NULL DEFAULT 0,
    role TEXT NOT NULL DEFAULT 'normal',
    target_group TEXT NOT NULL DEFAULT 'default',
    deleted INTEGER NOT NULL DEFAULT 0,
    updated_at_us INTEGER NOT NULL,
    origin_node TEXT NOT NULL,
    hash TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
)`,
	},
	{
		Version: 8,
		SQL: `CREATE INDEX IF NOT EXISTS idx_cluster_users_target_group ON cluster_users(target_group)`,
	},
	{
		Version: 9,
		SQL: `CREATE INDEX IF NOT EXISTS idx_cluster_users_updated_at_us ON cluster_users(updated_at_us)`,
	},
	{
		Version: 10,
		SQL: `CREATE INDEX IF NOT EXISTS idx_cluster_users_group_deleted ON cluster_users(target_group, deleted)`,
	},
	{
		Version: 11,
		SQL: `CREATE TABLE IF NOT EXISTS local_node_groups (
    group_name TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
)`,
	},
}
