// Package registry owns mydb's master database: one SQLite file (mode
// 0600) holding every saved connection, query history, and job history.
package registry

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// migrations run in order; schema_migrations records the applied set.
// Never edit an entry after release — append a new one.
var migrations = []string{
	`CREATE TABLE connections (
		id           INTEGER PRIMARY KEY,
		name         TEXT NOT NULL UNIQUE,
		engine       TEXT NOT NULL CHECK (engine IN ('sqlite','postgres')),
		locality     TEXT NOT NULL CHECK (locality IN ('local','remote')),
		path         TEXT NOT NULL DEFAULT '',
		host         TEXT NOT NULL DEFAULT '',
		port         INTEGER NOT NULL DEFAULT 0,
		dbname       TEXT NOT NULL DEFAULT '',
		username     TEXT NOT NULL DEFAULT '',
		secret_ref   TEXT NOT NULL DEFAULT '',
		options      TEXT NOT NULL DEFAULT '{}',
		created_at   TEXT NOT NULL,
		last_used_at TEXT
	);
	CREATE TABLE query_history (
		id            INTEGER PRIMARY KEY,
		connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
		sql           TEXT NOT NULL,
		started_at    TEXT NOT NULL,
		duration_ms   INTEGER,
		rows          INTEGER,
		ok            INTEGER,
		error         TEXT
	);
	CREATE TABLE jobs_history (
		id            INTEGER PRIMARY KEY,
		connection_id INTEGER,
		kind          TEXT,
		target        TEXT,
		started_at    TEXT,
		finished_at   TEXT,
		ok            INTEGER,
		detail        TEXT
	);
	CREATE TABLE settings ( key TEXT PRIMARY KEY, value TEXT );`,
}

// Connection is one saved connection (a row of the connections table).
// Secret is the resolved password; in v1 secret_ref is always
// "plain:<password>" — the tagged form leaves room for keychain/prompt/
// pgpass resolvers later without a schema change.
type Connection struct {
	ID         int64
	Name       string
	Engine     string // "sqlite" | "postgres"
	Locality   string // "local" | "remote"
	Path       string
	Host       string
	Port       int
	DBName     string
	Username   string
	Secret     string
	Options    string
	CreatedAt  string
	LastUsedAt string
}

const plainPrefix = "plain:"

type Registry struct {
	db   *sql.DB
	path string
}

// Open opens (creating if needed) the registry at path and applies
// pending migrations. The file and its directory are private to the
// user: passwords live in here.
func Open(path string) (*Registry, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	created := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		created = true
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	r := &Registry{db: db, path: path}
	if err := r.migrate(); err != nil {
		db.Close()
		if created {
			os.Remove(path)
		}
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

func (r *Registry) Close() error { return r.db.Close() }

// Path returns the registry file's location (shown in the UI, and
// openable as a regular SQLite connection).
func (r *Registry) Path() string { return r.path }

func (r *Registry) migrate() error {
	if _, err := r.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	var current int
	if err := r.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	for i := current; i < len(migrations); i++ {
		tx, err := r.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// Connections lists every saved connection, locals first, then by name.
func (r *Registry) Connections() ([]Connection, error) {
	rows, err := r.db.Query(`
		SELECT id, name, engine, locality, path, host, port, dbname, username,
		       secret_ref, options, created_at, COALESCE(last_used_at, '')
		FROM connections
		ORDER BY locality = 'remote', name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		var c Connection
		var secretRef string
		if err := rows.Scan(&c.ID, &c.Name, &c.Engine, &c.Locality, &c.Path, &c.Host,
			&c.Port, &c.DBName, &c.Username, &secretRef, &c.Options,
			&c.CreatedAt, &c.LastUsedAt); err != nil {
			return nil, err
		}
		if len(secretRef) >= len(plainPrefix) && secretRef[:len(plainPrefix)] == plainPrefix {
			c.Secret = secretRef[len(plainPrefix):]
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Create saves a new connection and returns its id.
func (r *Registry) Create(c Connection) (int64, error) {
	res, err := r.db.Exec(`
		INSERT INTO connections (name, engine, locality, path, host, port, dbname,
		                         username, secret_ref, options, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.Engine, c.Locality, c.Path, c.Host, c.Port, c.DBName,
		c.Username, plainPrefix+c.Secret, orJSON(c.Options), now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Update rewrites a saved connection in place.
func (r *Registry) Update(c Connection) error {
	_, err := r.db.Exec(`
		UPDATE connections
		SET name=?, engine=?, locality=?, path=?, host=?, port=?, dbname=?,
		    username=?, secret_ref=?, options=?
		WHERE id=?`,
		c.Name, c.Engine, c.Locality, c.Path, c.Host, c.Port, c.DBName,
		c.Username, plainPrefix+c.Secret, orJSON(c.Options), c.ID)
	return err
}

// Delete removes a connection; its history rows cascade away.
func (r *Registry) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM connections WHERE id=?`, id)
	return err
}

// TouchUsed stamps last_used_at after a successful connect.
func (r *Registry) TouchUsed(id int64) error {
	_, err := r.db.Exec(`UPDATE connections SET last_used_at=? WHERE id=?`, now(), id)
	return err
}

func orJSON(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}
