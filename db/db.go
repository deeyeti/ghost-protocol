package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the SQLite connection and provides typed helpers.
type DB struct {
	conn *sql.DB
}

// Open opens (or creates) the SQLite database at the given file path.
func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	d := &DB{conn: conn}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// migrate creates all required tables if they do not exist.
func (d *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS request_log (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			ts          DATETIME DEFAULT CURRENT_TIMESTAMP,
			prompt_hash TEXT NOT NULL,
			routed_to   TEXT NOT NULL,    -- 'local' | 'cloud' | 'cache'
			latency_ms  INTEGER,
			tokens_in   INTEGER,
			tokens_out  INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS cache_entries (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			ts          DATETIME DEFAULT CURRENT_TIMESTAMP,
			prompt_hash TEXT NOT NULL UNIQUE,
			embedding   BLOB NOT NULL,    -- JSON-encoded []float64
			response    TEXT NOT NULL
		);`,
	}
	for _, s := range stmts {
		if _, err := d.conn.Exec(s); err != nil {
			return fmt.Errorf("exec migration: %w", err)
		}
	}
	return nil
}

// LogRequest records a single proxied request.
func (d *DB) LogRequest(promptHash, routedTo string, latencyMs, tokensIn, tokensOut int) error {
	_, err := d.conn.Exec(
		`INSERT INTO request_log (prompt_hash, routed_to, latency_ms, tokens_in, tokens_out)
		 VALUES (?, ?, ?, ?, ?)`,
		promptHash, routedTo, latencyMs, tokensIn, tokensOut,
	)
	return err
}

// SaveCacheEntry stores a prompt embedding and its associated response.
func (d *DB) SaveCacheEntry(promptHash string, embeddingJSON []byte, response string) error {
	_, err := d.conn.Exec(
		`INSERT OR REPLACE INTO cache_entries (prompt_hash, embedding, response)
		 VALUES (?, ?, ?)`,
		promptHash, embeddingJSON, response,
	)
	return err
}

// AllCacheEntries returns every stored embedding and response for similarity search.
type CacheRow struct {
	PromptHash    string
	EmbeddingJSON []byte
	Response      string
}

func (d *DB) AllCacheEntries() ([]CacheRow, error) {
	rows, err := d.conn.Query(`SELECT prompt_hash, embedding, response FROM cache_entries`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CacheRow
	for rows.Next() {
		var r CacheRow
		if err := rows.Scan(&r.PromptHash, &r.EmbeddingJSON, &r.Response); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
