package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// genesisHash is the chain head before any event is appended.
const genesisHash = "genesis"

// Event is a single immutable, hash-chained mutation. The payload is the JSON
// encoding of the domain mutation; the digest binds the previous digest, the
// sequence number, the event type and the payload, so any tampering or partial
// write breaks the chain.
type Event struct {
	Seq     int64
	TrialID string
	Type    string
	Payload string
	Digest  string
}

// Tx is a unit of work over the store. Every mutation inside the closure is
// committed atomically, or rolled back in full if the closure returns an error.
func (s *Store) Tx(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Append writes a single event with the next sequence number and chain digest,
// and advances the chain head, all within the caller's transaction. It must be
// called inside a Tx closure.
func Append(tx *sql.Tx, trialID, typ string, payload any) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal event payload: %w", err)
	}
	return AppendRaw(tx, trialID, typ, string(raw))
}

// AppendRaw is Append with an already-serialised payload string.
func AppendRaw(tx *sql.Tx, trialID, typ, payload string) (int64, error) {
	var lastSeq int64
	var lastHash string
	if err := tx.QueryRow(`SELECT last_seq, last_hash FROM chain WHERE id = 1`).Scan(&lastSeq, &lastHash); err != nil {
		return 0, fmt.Errorf("read chain head: %w", err)
	}
	nextSeq := lastSeq + 1
	digest := digestEvent(lastHash, nextSeq, typ, payload)

	if _, err := tx.Exec(
		`INSERT INTO events(seq, trial_id, type, payload, digest) VALUES (?, ?, ?, ?, ?)`,
		nextSeq, trialID, typ, payload, digest,
	); err != nil {
		return 0, fmt.Errorf("insert event: %w", err)
	}
	if _, err := tx.Exec(`UPDATE chain SET last_seq = ?, last_hash = ? WHERE id = 1`, nextSeq, digest); err != nil {
		return 0, fmt.Errorf("advance chain: %w", err)
	}
	return nextSeq, nil
}

// digestEvent computes the hash-chain digest for a single event.
func digestEvent(prev string, seq int64, typ, payload string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s:%d:%s:%s", prev, seq, typ, payload)
	return hex.EncodeToString(h.Sum(nil))
}

// LastSeq returns the sequence number of the most recently appended event.
func (s *Store) LastSeq() (int64, error) {
	var seq int64
	if err := s.db.QueryRow(`SELECT last_seq FROM chain WHERE id = 1`).Scan(&seq); err != nil {
		return 0, fmt.Errorf("read chain head: %w", err)
	}
	return seq, nil
}

// EventsSince returns every event with a sequence number strictly greater than
// seq, ordered by sequence number.
func (s *Store) EventsSince(seq int64) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT seq, trial_id, type, payload, digest FROM events WHERE seq > ? ORDER BY seq ASC`, seq)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Seq, &e.TrialID, &e.Type, &e.Payload, &e.Digest); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// VerifyChain re-checks every event digest from genesis to the chain head. It
// returns the first broken sequence number (or 0 when the chain is intact).
func (s *Store) VerifyChain() (int64, error) {
	rows, err := s.db.Query(`SELECT seq, type, payload, digest FROM events ORDER BY seq ASC`)
	if err != nil {
		return 0, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	prev := genesisHash
	var seq int64
	for rows.Next() {
		var typ, payload, digest string
		if err := rows.Scan(&seq, &typ, &payload, &digest); err != nil {
			return 0, fmt.Errorf("scan event: %w", err)
		}
		if want := digestEvent(prev, seq, typ, payload); want != digest {
			return seq, nil
		}
		prev = digest
	}
	return 0, rows.Err()
}

// EventDraft is a not-yet-persisted event: a trial id, a type and a JSON-marshalled
// payload. AppendMany writes a batch of drafts atomically, so multi-event
// mutations (for example a sample allocation that also acquires leases) commit
// or roll back as one unit.
type EventDraft struct {
	TrialID string
	Type    string
	Payload any
}

// AppendMany appends every draft in one transaction, advancing the hash chain
// once per event. All-or-nothing: a failure rolls the whole batch back.
func (s *Store) AppendMany(drafts []EventDraft) error {
	return s.Tx(func(tx *sql.Tx) error {
		for _, d := range drafts {
			if _, err := Append(tx, d.TrialID, d.Type, d.Payload); err != nil {
				return err
			}
		}
		return nil
	})
}
