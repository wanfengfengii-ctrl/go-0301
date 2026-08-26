// Package store is the durable, relational persistence layer for the
// viability-release service. It is an append-only event store backed by SQLite:
// every business mutation is written as an immutable, hash-chained event row.
// Projections are rebuilt in memory by the service package by replaying the
// event log; a snapshot table shortens recovery, and idempotency records are
// persisted so retries survive a restart.
//
// The event log is the single source of truth. A broken hash chain (for
// example after an interrupted or corrupted write) is detected at recovery time
// and turns the affected trial read-only, never silently inventing state.
package store

import (
	"database/sql"
	"fmt"
	"sync/atomic"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo), amd64/arm64 safe
)

// memSeq assigns each in-memory store a process-unique database name. A shared
// in-memory SQLite database is keyed by the name in its DSN, so a fixed name
// would alias every :memory: store to the same database; a unique name gives
// each store a clean, isolated copy that is dropped when its connection closes.
var memSeq uint64

// Store wraps a single SQLite database file (or an in-memory database when
// path is ":memory:"). It serializes writes through a single connection so
// that SQLite's single-writer model and our logical clock stay consistent.
type Store struct {
	db *sql.DB
}

// Open opens the store at path, creating the schema if necessary, and returns
// a ready-to-use Store. Pass ":memory:" for a throwaway database.
//
// Each ":memory:" open gets its own private in-memory database: a shared-cache
// in-memory database is identified by the name in its DSN, so a fixed name
// would alias every throwaway store to the same data. A per-open unique name
// (plus a single connection per store) keeps each store isolated, and the
// database is released when its sole connection closes.
func Open(path string) (*Store, error) {
	dsn := path
	if path == "" || path == ":memory:" {
		name := fmt.Sprintf("seed-vault-mem-%d", atomic.AddUint64(&memSeq, 1))
		dsn = fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite serializes writers; a single connection keeps our transaction
	// semantics and logical clock simple and deterministic.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// migrate creates the schema idempotently.
func (s *Store) migrate() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS events (
			seq       INTEGER PRIMARY KEY AUTOINCREMENT,
			trial_id  TEXT NOT NULL,
			type      TEXT NOT NULL,
			payload   TEXT NOT NULL,
			digest    TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_trial ON events(trial_id, seq)`,
		`CREATE TABLE IF NOT EXISTS idempotency (
			key        TEXT PRIMARY KEY,
			digest     TEXT NOT NULL,
			result     TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS chain (
			id        INTEGER PRIMARY KEY CHECK (id = 1),
			last_seq  INTEGER NOT NULL,
			last_hash TEXT NOT NULL
		)`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// Seed the chain head if absent.
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chain`).Scan(&n); err != nil {
		return fmt.Errorf("seed chain: %w", err)
	}
	if n == 0 {
		if _, err := s.db.Exec(`INSERT INTO chain(id, last_seq, last_hash) VALUES (1, 0, ?)`, genesisHash); err != nil {
			return fmt.Errorf("seed chain: %w", err)
		}
	}
	return nil
}
