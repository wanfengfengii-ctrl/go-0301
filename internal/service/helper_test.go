package service

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/lineage"
	"seed-vault-viability-release/internal/rules"
	"seed-vault-viability-release/internal/store"
)

// newTestService returns a service backed by a throwaway in-memory store.
func newTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	svc := New(st, rules.NewStandardCatalog())
	if err := svc.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	return svc
}

// createLockedTrial creates and locks a trial with a unique species and a fixed
// snapshot, returning its id.
func createLockedTrial(t *testing.T, svc *Service, species string) string {
	t.Helper()
	trial, err := svc.CreateTrial(CreateTrialInput{Species: species, IdempotencyKey: "key-" + species})
	if err != nil {
		t.Fatalf("create trial: %v", err)
	}
	if _, err := svc.LockTrial(trial.ID, LockTrialInput{Version: "v1"}); err != nil {
		t.Fatalf("lock trial: %v", err)
	}
	return trial.ID
}

// allocateFixture allocates a single sample of 100 grains into a full universe.
func allocateFixture(t *testing.T, svc *Service, trialID string) {
	t.Helper()
	err := svc.AllocateSamples(trialID, AllocateInput{
		SampleID:   "sample-1",
		Allocation: lineage.Allocation{Source: 100, Culture: 60, Retain: 20, Measurement: 10, Quarantine: 5, Loss: 5},
		SeedLots:   []domain.SeedLot{{ID: "lot-1", ParentID: "collection-1", Species: "Oryza sativa", Location: "cold-1", Count: 500}},
		Samples:    []domain.SampleUnit{{ID: "sample-1", SeedLotID: "lot-1", Count: 100, Moisture: 8}},
		Groups:     []domain.CultureGroup{{ID: "group-1", SampleID: "sample-1", SeedLotID: "lot-1", Generation: 1, Count: 60}},
		Plates:     []domain.Plate{{ID: "plate-1", GroupID: "group-1", Position: 0, Generation: 1, Sown: 60}},
	})
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
}
