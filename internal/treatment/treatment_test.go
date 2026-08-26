package treatment_test

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/treatment"
)

func TestStageSequenceContiguousPrefix(t *testing.T) {
	stages := []domain.Stage{
		domain.StageWarmup, domain.StageDormancyBreak, domain.StageSowing,
		domain.StageIncubation, domain.StageObservation, domain.StageClosed,
	}
	seq := treatment.NewStageSequence(stages)
	for _, want := range stages {
		if err := seq.Advance(want); err != nil {
			t.Fatalf("Advance(%q): %v", want, err)
		}
		if seq.Current() != want {
			t.Fatalf("current = %q, want %q", seq.Current(), want)
		}
	}
}

func TestStageSequenceSkipsSowing(t *testing.T) {
	seq := treatment.NewStageSequence([]domain.Stage{
		domain.StageWarmup, domain.StageDormancyBreak, domain.StageSowing, domain.StageIncubation,
	})
	if err := seq.Advance(domain.StageWarmup); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := seq.Advance(domain.StageDormancyBreak); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := seq.Advance(domain.StageIncubation); !domain.IsCode(err, domain.CodeStageGap) {
		t.Fatalf("got %v, want STAGE_GAP", err)
	}
	// sequence must be unchanged after the rejected advance
	if seq.Current() != domain.StageDormancyBreak {
		t.Fatalf("current = %q, want dormancy_break", seq.Current())
	}
}

func TestLeaseConflictAndExpiry(t *testing.T) {
	s := treatment.NewLeaseStore()
	_, err := s.Acquire(treatment.LeaseRequest{
		ID: "l1", Resource: "incubator-1", Kind: domain.ResourceIncubator,
		Holder: "operator-a", Purpose: "culture", Generation: 1, Now: 0, Duration: 100,
	})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// a competing holder at the same time must be rejected
	_, err = s.Acquire(treatment.LeaseRequest{
		ID: "l2", Resource: "incubator-1", Kind: domain.ResourceIncubator,
		Holder: "operator-b", Purpose: "culture", Generation: 1, Now: 10, Duration: 100,
	})
	if !domain.IsCode(err, domain.CodeLeaseConflict) {
		t.Fatalf("got %v, want LEASE_CONFLICT", err)
	}
	// after the logical clock passes the expiry, a new holder succeeds
	_, err = s.Acquire(treatment.LeaseRequest{
		ID: "l3", Resource: "incubator-1", Kind: domain.ResourceIncubator,
		Holder: "operator-b", Purpose: "culture", Generation: 1, Now: 150, Duration: 100,
	})
	if err != nil {
		t.Fatalf("re-acquire after expiry: %v", err)
	}
}

func TestLeaseRenewExpired(t *testing.T) {
	s := treatment.NewLeaseStore()
	l, _ := s.Acquire(treatment.LeaseRequest{
		ID: "l1", Resource: "incubator-1", Kind: domain.ResourceIncubator,
		Holder: "operator-a", Purpose: "culture", Generation: 1, Now: 0, Duration: 50,
	})
	if _, err := s.Renew(l.ID, "operator-a", 200, 50); !domain.IsCode(err, domain.CodeLeaseExpired) {
		t.Fatalf("got %v, want LEASE_EXPIRED", err)
	}
}
