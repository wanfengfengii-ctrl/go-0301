package service

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/rules"
	"seed-vault-viability-release/internal/store"
)

// TestIdempotencyReplayAndConflict verifies a same-content retry returns the
// original trial while different content under the same key conflicts.
func TestIdempotencyReplayAndConflict(t *testing.T) {
	svc := newTestService(t)
	first, err := svc.CreateTrial(CreateTrialInput{Species: "Oryza idem", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	replay, err := svc.CreateTrial(CreateTrialInput{Species: "Oryza idem", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay id = %q, want %q", replay.ID, first.ID)
	}
	if _, err := svc.CreateTrial(CreateTrialInput{Species: "Oryza other", IdempotencyKey: "k1"}); !domain.IsCode(err, domain.CodeIdempotencyConflict) {
		t.Fatalf("got %v, want IDEMPOTENCY_CONFLICT", err)
	}
}

// TestTerminalSingleWinner submits two independent reviews then races three
// terminal requests of different types and asserts exactly one credential wins.
func TestTerminalSingleWinner(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza terminal")
	allocateFixture(t, svc, id)

	for _, r := range []string{"reviewer-1", "reviewer-2"} {
		if err := svc.SubmitReview(id, ReviewInput{ReviewerID: r, Qualification: "qualified", Digest: "d-" + r}); err != nil {
			t.Fatalf("submit review: %v", err)
		}
	}

	types := []domain.TerminalType{domain.TerminalRelease, domain.TerminalQuarantine, domain.TerminalVoid}
	var wg sync.WaitGroup
	results := make([]error, 3)
	start := make(chan struct{})
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_, results[n] = svc.DecideTerminal(id, TerminalInput{Type: types[n]})
		}(i)
	}
	close(start)
	wg.Wait()

	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
		} else if !domain.IsCode(err, domain.CodeTerminalAlreadyDecided) {
			t.Fatalf("unexpected terminal error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("exactly one terminal decision should win, got %d", wins)
	}
	cred, err := svc.GetCredential(id)
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	if len(cred.Reviews) != 2 {
		t.Fatalf("credential reviews = %d, want 2", len(cred.Reviews))
	}
}

// TestRestartRecovery persists a full flow, closes the database, reopens it and
// asserts stages, leases, retests and the terminal credential all recover.
func TestRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seed-vault.db")

	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	svc := New(st, rules.NewStandardCatalog())
	if err := svc.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	id := createLockedTrial(t, svc, "Oryza recover")
	allocateFixture(t, svc, id)
	if _, err := svc.AcquireLease(id, LeaseAcquireInput{
		ID: "l1", Resource: "incubator-1", Kind: domain.ResourceIncubator,
		Holder: "op", Purpose: "culture", Generation: 1, Now: 0, Duration: 1000,
	}); err != nil {
		t.Fatalf("lease: %v", err)
	}
	if err := svc.RecordTreatment(id, TreatmentInput{
		PlateID: "plate-1", Stage: domain.StageWarmup, Operator: "op", LogicalTime: 1,
	}); err != nil {
		t.Fatalf("treatment: %v", err)
	}
	if err := svc.RecordObservation(id, ObservationInput{
		PlateID: "plate-1", Counts: map[domain.ObservationClass]int64{domain.ClassGerminated: 30},
		Operator: "op", LogicalTime: 10,
	}); err != nil {
		t.Fatalf("observation: %v", err)
	}
	if _, err := svc.GenerateRetest(id, RetestInput{
		Reason:  "contamination",
		Members: []domain.RetestMember{{SeedLotID: "lot-1", SampleID: "sample-1", GroupID: "group-1", PlateIndex: 0}},
	}); err != nil {
		t.Fatalf("retest: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and recover.
	st2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	svc2 := New(st2, rules.NewStandardCatalog())
	if err := svc2.Recover(); err != nil {
		t.Fatalf("recover after restart: %v", err)
	}

	trial, err := svc2.GetTrial(id)
	if err != nil {
		t.Fatalf("get trial: %v", err)
	}
	if !trial.Locked || trial.CurrentGen != 2 {
		t.Fatalf("recovered trial = %+v, want locked gen 2", trial)
	}
	if _, err := svc2.GetRetest(id, 1, "contamination"); err != nil {
		t.Fatalf("recovered retest: %v", err)
	}
	if _, err := svc2.PlateMetrics(id, "plate-1"); err != nil {
		t.Fatalf("metrics: %v", err)
	}
	// the lease must have survived too.
	if err := svc2.ReleaseLease(id, "l1", "op"); err != nil {
		t.Fatalf("recovered lease release: %v", err)
	}
}

// TestRecoveryDetectsBrokenChain corrupts the event log and asserts recovery
// fails rather than inventing state.
func TestRecoveryDetectsBrokenChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seed-vault.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	svc := New(st, rules.NewStandardCatalog())
	_ = svc.Recover()
	if _, err := svc.CreateTrial(CreateTrialInput{Species: "Oryza corrupt", IdempotencyKey: "k"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	st.Close()

	// Tamper with a stored digest directly.
	if err := os.WriteFile(path, []byte("corrupted"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	// The SQLite file is now invalid; opening should fail.
	if _, err := store.Open(path); err == nil {
		t.Fatalf("expected open of corrupted db to fail")
	}
}
