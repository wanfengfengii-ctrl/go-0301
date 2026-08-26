package store_test

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/store"
)

// TestAppendAndVerifyChain appends events and asserts the hash chain verifies
// and the events replay in sequence order.
func TestAppendAndVerifyChain(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	if err := st.AppendMany([]store.EventDraft{
		{TrialID: "t1", Type: "trial.created", Payload: map[string]string{"id": "t1"}},
		{TrialID: "t1", Type: "trial.locked", Payload: map[string]string{"id": "t1"}},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	if broken, err := st.VerifyChain(); err != nil || broken != 0 {
		t.Fatalf("verify chain: broken=%d err=%v", broken, err)
	}
	events, err := st.EventsSince(0)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("unexpected sequence numbers: %+v", events)
	}
}

// TestIdempotencyPersisted verifies idempotency binding, replay and conflict.
func TestIdempotencyPersisted(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	rec, created, err := st.PutIdempotency("k1", "digest-a", "result-1", 1)
	if err != nil || !created || rec.Result != "result-1" {
		t.Fatalf("first put: %+v created=%v err=%v", rec, created, err)
	}
	// same content: replay, no new binding.
	rec, created, err = st.PutIdempotency("k1", "digest-a", "result-1", 2)
	if err != nil || created || rec.Result != "result-1" {
		t.Fatalf("replay: %+v created=%v err=%v", rec, created, err)
	}
	// different content: stable conflict.
	if _, _, err := st.PutIdempotency("k1", "digest-b", "result-2", 3); !domain.IsCode(err, domain.CodeIdempotencyConflict) {
		t.Fatalf("got %v, want IDEMPOTENCY_CONFLICT", err)
	}
}
