package service

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/lineage"
)

// TestLockFreezesSnapshot verifies that locking a trial fixes the rule snapshot
// and rejects a stale expected digest with STALE_RULE_DIGEST.
func TestLockFreezesSnapshot(t *testing.T) {
	svc := newTestService(t)
	trial, err := svc.CreateTrial(CreateTrialInput{Species: "Oryza sativa", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if trial.Locked {
		t.Fatalf("trial should start unlocked")
	}

	if _, err := svc.LockTrial(trial.ID, LockTrialInput{Version: "v1", ExpectedDigest: "stale"}); !domain.IsCode(err, domain.CodeStaleRuleDigest) {
		t.Fatalf("got %v, want STALE_RULE_DIGEST", err)
	}
	if _, err := svc.LockTrial(trial.ID, LockTrialInput{Version: "nope"}); !domain.IsCode(err, domain.CodeStaleRuleDigest) {
		t.Fatalf("got %v, want STALE_RULE_DIGEST for unknown version", err)
	}

	locked, err := svc.LockTrial(trial.ID, LockTrialInput{Version: "v1"})
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if !locked.Locked || locked.CurrentGen != 1 {
		t.Fatalf("locked trial = %+v", locked)
	}
}

// TestLineageCycleAndMultipleParent verifies that a cycle or a second parent is
// rejected during allocation with no residual state.
func TestLineageCycleAndMultipleParent(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza cycle")

	// cycle: lot -> sample -> lot
	err := svc.AllocateSamples(id, AllocateInput{
		SampleID:   "s1",
		Allocation: lineage.Allocation{Source: 10, Culture: 10},
		SeedLots:   []domain.SeedLot{{ID: "lot-a", ParentID: "s1"}},
		Samples:    []domain.SampleUnit{{ID: "s1", SeedLotID: "lot-a"}},
	})
	if !domain.IsCode(err, domain.CodeLineageCycle) {
		t.Fatalf("got %v, want LINEAGE_CYCLE", err)
	}

	// Establish a seed lot with one parent, then a second allocation tries to
	// give the same lot a different parent: MULTIPLE_PARENT.
	err = svc.AllocateSamples(id, AllocateInput{
		SampleID:   "s2",
		Allocation: lineage.Allocation{Source: 10, Culture: 10},
		SeedLots:   []domain.SeedLot{{ID: "lot-b", ParentID: "c1"}},
		Samples:    []domain.SampleUnit{{ID: "s2", SeedLotID: "lot-b"}},
	})
	if err != nil {
		t.Fatalf("setup allocation: %v", err)
	}
	err = svc.AllocateSamples(id, AllocateInput{
		SampleID:   "s3",
		Allocation: lineage.Allocation{Source: 10, Culture: 10},
		SeedLots:   []domain.SeedLot{{ID: "lot-b", ParentID: "c2"}},
		Samples:    []domain.SampleUnit{{ID: "s3", SeedLotID: "lot-b"}},
	})
	if !domain.IsCode(err, domain.CodeMultipleParent) {
		t.Fatalf("got %v, want MULTIPLE_PARENT", err)
	}

	view, err := svc.Lineage(id)
	if err != nil {
		t.Fatalf("lineage: %v", err)
	}
	// only the successful setup allocation should remain, not the rejected one.
	if len(view.SeedLots) != 1 || len(view.Samples) != 1 {
		t.Fatalf("residual lineage state after rejected allocations: %+v", view)
	}
}

// TestDuplicateSampleAndConservation verifies duplicate sample identity and
// broken grain conservation are rejected with stable codes.
func TestDuplicateSampleAndConservation(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza dup")
	allocateFixture(t, svc, id)

	// re-allocating the same sample is SAMPLE_ALREADY_ALLOCATED.
	err := svc.AllocateSamples(id, AllocateInput{
		SampleID: "sample-1", Allocation: lineage.Allocation{Source: 10, Culture: 10},
	})
	if !domain.IsCode(err, domain.CodeSampleAlreadyAllocated) {
		t.Fatalf("got %v, want SAMPLE_ALREADY_ALLOCATED", err)
	}

	// broken conservation.
	err = svc.AllocateSamples(id, AllocateInput{
		SampleID: "sample-2", Allocation: lineage.Allocation{Source: 10, Culture: 5},
	})
	if !domain.IsCode(err, domain.CodeInvalidSampleCount) {
		t.Fatalf("got %v, want INVALID_SAMPLE_COUNT", err)
	}
}
