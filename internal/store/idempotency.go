package store

import (
	"database/sql"
	"fmt"

	"seed-vault-viability-release/internal/domain"
)

// IdempotencyRecord is the persisted binding of an idempotency key.
type IdempotencyRecord struct {
	Key       string
	Digest    string
	Result    string
	CreatedAt int64
}

// PutIdempotency binds key to digest on first use, returning the stored record
// and whether this call created the binding. A retry with the same digest
// returns the original result; a different digest yields IDEMPOTENCY_CONFLICT
// and leaves the binding untouched. It is transactional and survives restart.
func (s *Store) PutIdempotency(key, digest, result string, at int64) (domain.IdempotencyRecord, bool, error) {
	var rec domain.IdempotencyRecord
	created := false
	err := s.Tx(func(tx *sql.Tx) error {
		var d, r string
		err := tx.QueryRow(`SELECT digest, result FROM idempotency WHERE key = ?`, key).Scan(&d, &r)
		switch {
		case err == sql.ErrNoRows:
			if _, err := tx.Exec(
				`INSERT INTO idempotency(key, digest, result, created_at) VALUES (?, ?, ?, ?)`,
				key, digest, result, at,
			); err != nil {
				return fmt.Errorf("insert idempotency: %w", err)
			}
			rec = domain.IdempotencyRecord{Key: key, Digest: digest, Result: result, CreatedAt: at}
			created = true
			return nil
		case err != nil:
			return fmt.Errorf("lookup idempotency: %w", err)
		case d != digest:
			return domain.New(domain.CodeIdempotencyConflict,
				"key %q already bound to a different command", key)
		default:
			rec = domain.IdempotencyRecord{Key: key, Digest: d, Result: r}
			return nil
		}
	})
	return rec, created, err
}

// GetIdempotency returns the persisted record for a key, if any.
func (s *Store) GetIdempotency(key string) (domain.IdempotencyRecord, bool, error) {
	var rec domain.IdempotencyRecord
	err := s.db.QueryRow(
		`SELECT key, digest, result, created_at FROM idempotency WHERE key = ?`, key,
	).Scan(&rec.Key, &rec.Digest, &rec.Result, &rec.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return domain.IdempotencyRecord{}, false, fmt.Errorf("get idempotency: %w", err)
	}
	return rec, true, nil
}
