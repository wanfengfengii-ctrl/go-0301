package service

import (
	"sync"
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/lineage"
)

// TestConcurrentAllocationSingleWinner races two allocations of the same sample
// and asserts exactly one succeeds.
func TestConcurrentAllocationSingleWinner(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza race")

	in := AllocateInput{
		SampleID:   "sample-1",
		Allocation: lineage.Allocation{Source: 100, Culture: 60, Retain: 20, Measurement: 10, Quarantine: 5, Loss: 5},
		SeedLots:   []domain.SeedLot{{ID: "lot-1", ParentID: "c1", Species: "Oryza sativa"}},
		Samples:    []domain.SampleUnit{{ID: "sample-1", SeedLotID: "lot-1", Count: 100}},
		Groups:     []domain.CultureGroup{{ID: "group-1", SampleID: "sample-1", SeedLotID: "lot-1", Generation: 1, Count: 60}},
		Plates:     []domain.Plate{{ID: "plate-1", GroupID: "group-1", Position: 0, Generation: 1, Sown: 60}},
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			results[n] = svc.AllocateSamples(id, in)
		}(i)
	}
	close(start)
	wg.Wait()

	okCount := 0
	for _, err := range results {
		if err == nil {
			okCount++
		} else if !domain.IsCode(err, domain.CodeSampleAlreadyAllocated) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if okCount != 1 {
		t.Fatalf("exactly one allocation should succeed, got %d", okCount)
	}
}

// TestLeaseCompetitionAndExpiry races two holders for one incubator and then
// advances the logical clock past expiry so a new holder can acquire.
func TestLeaseCompetitionAndExpiry(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza lease")

	l1, err := svc.AcquireLease(id, LeaseAcquireInput{
		ID: "l1", Resource: "incubator-1", Kind: domain.ResourceIncubator,
		Holder: "op-a", Purpose: "culture", Generation: 1, Now: 0, Duration: 100,
	})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := svc.AcquireLease(id, LeaseAcquireInput{
		ID: "l2", Resource: "incubator-1", Kind: domain.ResourceIncubator,
		Holder: "op-b", Purpose: "culture", Generation: 1, Now: 10, Duration: 100,
	}); !domain.IsCode(err, domain.CodeLeaseConflict) {
		t.Fatalf("got %v, want LEASE_CONFLICT", err)
	}

	// after expiry the new holder succeeds.
	if _, err := svc.AcquireLease(id, LeaseAcquireInput{
		ID: "l3", Resource: "incubator-1", Kind: domain.ResourceIncubator,
		Holder: "op-b", Purpose: "culture", Generation: 1, Now: 150, Duration: 100,
	}); err != nil {
		t.Fatalf("re-acquire after expiry: %v", err)
	}
	_ = l1
}

// TestLeaseGenerationMismatch verifies a lease for a stale generation is
// rejected.
func TestLeaseGenerationMismatch(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza genmismatch")
	_, err := svc.AcquireLease(id, LeaseAcquireInput{
		ID: "l1", Resource: "incubator-1", Kind: domain.ResourceIncubator,
		Holder: "op-a", Purpose: "culture", Generation: 9, Now: 0, Duration: 100,
	})
	if !domain.IsCode(err, domain.CodeGenerationMismatch) {
		t.Fatalf("got %v, want GENERATION_MISMATCH", err)
	}
}
