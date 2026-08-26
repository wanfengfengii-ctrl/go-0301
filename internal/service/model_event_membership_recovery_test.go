package service

import (
	"database/sql"
	"path/filepath"
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/lineage"
	"seed-vault-viability-release/internal/rules"
	"seed-vault-viability-release/internal/store"
)

func TestModel_EventMembershipIsAuthenticatedDuringRecovery(t *testing.T) {
	tests := []struct {
		name           string
		redirectLease  bool
		wantRecoverErr bool
	}{
		{name: "intact ordered log restores complete trial", redirectLease: false, wantRecoverErr: false},
		{name: "redirected event is rejected", redirectLease: true, wantRecoverErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "seed-vault.db")
			st, err := store.Open(path)
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			svc := New(st, rules.NewStandardCatalog())
			if err := svc.Recover(); err != nil {
				t.Fatalf("initial recover: %v", err)
			}

			source, err := svc.CreateTrial(CreateTrialInput{Species: "Oryza membership source", IdempotencyKey: "source-key"})
			if err != nil {
				t.Fatalf("create source trial: %v", err)
			}
			if _, err := svc.LockTrial(source.ID, LockTrialInput{Version: "v1"}); err != nil {
				t.Fatalf("lock source trial: %v", err)
			}
			destination, err := svc.CreateTrial(CreateTrialInput{Species: "Oryza membership destination", IdempotencyKey: "destination-key"})
			if err != nil {
				t.Fatalf("create destination trial: %v", err)
			}
			if _, err := svc.LockTrial(destination.ID, LockTrialInput{Version: "v1"}); err != nil {
				t.Fatalf("lock destination trial: %v", err)
			}

			if err := svc.AllocateSamples(source.ID, AllocateInput{
				SampleID:   "sample-1",
				Allocation: lineage.Allocation{Source: 100, Culture: 60, Retain: 20, Measurement: 10, Quarantine: 5, Loss: 5},
				SeedLots:   []domain.SeedLot{{ID: "lot-1", ParentID: "collection-1", Species: "Oryza sativa", Location: "cold-1", Count: 500}},
				Samples:    []domain.SampleUnit{{ID: "sample-1", SeedLotID: "lot-1", Count: 100, Moisture: 8}},
				Groups:     []domain.CultureGroup{{ID: "group-1", SampleID: "sample-1", SeedLotID: "lot-1", Generation: 1, Count: 60}},
				Plates:     []domain.Plate{{ID: "plate-1", GroupID: "group-1", Position: 0, Generation: 1, Sown: 60}},
			}); err != nil {
				t.Fatalf("allocate source samples: %v", err)
			}
			if _, err := svc.AcquireLease(source.ID, LeaseAcquireInput{
				ID: "lease-1", Resource: "incubator-1", Kind: domain.ResourceIncubator,
				Holder: "operator", Purpose: "culture", Generation: 1, Now: 0, Duration: 1000,
			}); err != nil {
				t.Fatalf("acquire lease: %v", err)
			}
			if err := svc.RecordTreatment(source.ID, TreatmentInput{
				PlateID: "plate-1", Stage: domain.StageWarmup, Operator: "operator", LogicalTime: 1,
			}); err != nil {
				t.Fatalf("record treatment: %v", err)
			}
			if err := svc.RecordObservation(source.ID, ObservationInput{
				PlateID: "plate-1", Counts: map[domain.ObservationClass]int64{domain.ClassGerminated: 30},
				Operator: "operator", LogicalTime: 10,
			}); err != nil {
				t.Fatalf("record observation: %v", err)
			}
			if _, err := svc.GenerateRetest(source.ID, RetestInput{
				Reason: "contamination",
				Members: []domain.RetestMember{{
					SeedLotID: "lot-1", SampleID: "sample-1", GroupID: "group-1", PlateIndex: 0,
				}},
			}); err != nil {
				t.Fatalf("generate retest: %v", err)
			}
			for _, reviewer := range []string{"reviewer-1", "reviewer-2"} {
				if err := svc.SubmitReview(source.ID, ReviewInput{
					ReviewerID: reviewer, Qualification: "qualified", Digest: "digest-" + reviewer,
				}); err != nil {
					t.Fatalf("submit review %s: %v", reviewer, err)
				}
			}
			if _, err := svc.DecideTerminal(source.ID, TerminalInput{Type: domain.TerminalRelease}); err != nil {
				t.Fatalf("decide terminal state: %v", err)
			}

			events, err := st.EventsSince(0)
			if err != nil {
				t.Fatalf("read persisted events: %v", err)
			}
			for i, event := range events {
				if event.Seq != int64(i+1) {
					t.Fatalf("event at index %d has sequence %d, want %d", i, event.Seq, i+1)
				}
			}
			if err := st.Close(); err != nil {
				t.Fatalf("close store: %v", err)
			}

			if tt.redirectLease {
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatalf("open database for corruption fixture: %v", err)
				}
				result, err := db.Exec(
					`UPDATE events SET trial_id = ? WHERE trial_id = ? AND type = 'lease.acquired'`,
					destination.ID, source.ID,
				)
				if err != nil {
					db.Close()
					t.Fatalf("redirect persisted event: %v", err)
				}
				changed, err := result.RowsAffected()
				if err != nil {
					db.Close()
					t.Fatalf("read redirected row count: %v", err)
				}
				if changed != 1 {
					db.Close()
					t.Fatalf("redirected rows = %d, want 1", changed)
				}
				if err := db.Close(); err != nil {
					t.Fatalf("close corruption fixture database: %v", err)
				}
			}

			reopened, err := store.Open(path)
			if err != nil {
				t.Fatalf("reopen store: %v", err)
			}
			defer reopened.Close()
			recovered := New(reopened, rules.NewStandardCatalog())
			err = recovered.Recover()
			if tt.wantRecoverErr {
				if err == nil {
					t.Fatal("recovery accepted an event redirected to another trial")
				}
				return
			}
			if err != nil {
				t.Fatalf("recover intact log: %v", err)
			}

			trial, err := recovered.GetTrial(source.ID)
			if err != nil {
				t.Fatalf("get recovered trial: %v", err)
			}
			if !trial.Locked || trial.CurrentGen != 2 || trial.LogicalClock != 10 || trial.Terminal != domain.TerminalStatusDecided {
				t.Fatalf("recovered trial state = %+v", trial)
			}
			lineageView, err := recovered.Lineage(source.ID)
			if err != nil || len(lineageView.Plates) != 1 || !lineageView.Conserved {
				t.Fatalf("recovered lineage = %+v, err = %v", lineageView, err)
			}
			if _, err := recovered.PlateMetrics(source.ID, "plate-1"); err != nil {
				t.Fatalf("recovered observation: %v", err)
			}
			if _, err := recovered.GetRetest(source.ID, 1, "contamination"); err != nil {
				t.Fatalf("recovered retest: %v", err)
			}
			credential, err := recovered.GetCredential(source.ID)
			if err != nil || credential.Type != domain.TerminalRelease || len(credential.Reviews) != 2 {
				t.Fatalf("recovered credential = %+v, err = %v", credential, err)
			}
			if err := recovered.RecordTreatment(source.ID, TreatmentInput{
				PlateID: "plate-1", Stage: domain.StageDormancyBreak, Operator: "operator", LogicalTime: 11,
			}); err != nil {
				t.Fatalf("recovered treatment order did not continue: %v", err)
			}
			if err := recovered.ReleaseLease(source.ID, "lease-1", "operator"); err != nil {
				t.Fatalf("recovered lease: %v", err)
			}
		})
	}
}
