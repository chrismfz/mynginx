package sqlite

import (
	"database/sql"
	"fmt"
)

func migrate(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	// schema_migrations
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations(
			version INTEGER PRIMARY KEY
		);
	`); err != nil {
		return err
	}

	var v int
	err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&v)
	if err != nil {
		return err
	}

	if v >= 1 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Users
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS users(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			home_dir TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		);
	`); err != nil {
		return err
	}

	// Sites
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS sites(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			domain TEXT NOT NULL UNIQUE,
			mode TEXT NOT NULL DEFAULT 'php',
			webroot TEXT NOT NULL,
			php_version TEXT NOT NULL DEFAULT '',
			enable_http3 INTEGER NOT NULL DEFAULT 1,
			enabled INTEGER NOT NULL DEFAULT 1,

			last_render_hash TEXT NOT NULL DEFAULT '',
			last_applied_at TEXT,
			last_apply_status TEXT NOT NULL DEFAULT '',
			last_apply_error TEXT NOT NULL DEFAULT '',

			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		);
	`); err != nil {
		return err
	}

	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_sites_user_id ON sites(user_id);`); err != nil {
		return err
	}

	// Apply runs (audit-ish)
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS apply_runs(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			site_id INTEGER,
			action TEXT NOT NULL,
			status TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE SET NULL
		);
	`); err != nil {
		return err
	}

	if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES (1);`); err != nil {
		return err
	}

	return tx.Commit()
}
