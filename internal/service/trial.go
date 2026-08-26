package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/rules"
)

// CreateTrialInput is the command payload for creating a viability trial.
type CreateTrialInput struct {
	Species        string
	IdempotencyKey string
}

// CreateTrial creates a new, unlocked trial. The trial id is deterministic for
// a given species, and an idempotency key makes a repeated identical request
// return the already-created trial. A different command under the same key is
// a stable IDEMPOTENCY_CONFLICT.
func (s *Service) CreateTrial(in CreateTrialInput) (domain.ViabilityTrial, error) {
	if in.Species == "" {
		return domain.ViabilityTrial{}, domain.New(domain.CodeInvalidSampleCount, "species is required")
	}
	if in.IdempotencyKey == "" {
		return domain.ViabilityTrial{}, domain.New(domain.CodeInvalidSampleCount, "idempotency_key is required")
	}
	id := trialID(in.Species)
	at := s.now()
	digest := createDigest(in.Species)

	// The trial id is deterministic for a species, so it is bound as the
	// idempotency result in the same step that claims the key.
	rec, created, err := s.store.PutIdempotency(in.IdempotencyKey, digest, id, at)
	if err != nil {
		return domain.ViabilityTrial{}, err
	}
	if !created {
		if t, unlock, ok := s.readTrial(rec.Result); ok {
			defer unlock()
			return t.Trial, nil
		}
		return domain.ViabilityTrial{}, domain.New(domain.CodeIdempotencyConflict, "idempotency key already bound")
	}

	if err := s.mutate(func() ([]event, error) {
		return []event{{
			trialID: id,
			typ:     evTrialCreated,
			payload: trialCreatedPayload{ID: id, Species: in.Species, At: at},
		}}, nil
	}); err != nil {
		return domain.ViabilityTrial{}, err
	}
	t, unlock, _ := s.readTrial(id)
	defer unlock()
	return t.Trial, nil
}

// LockTrialInput is the command payload for freezing a trial into generation 1.
type LockTrialInput struct {
	Version        string
	ExpectedDigest string
}

// LockTrial freezes the rule snapshot into the trial and opens generation 1.
// It rejects an unknown version or a stale expected digest with a stable code,
// and never modifies an already-locked trial in place.
func (s *Service) LockTrial(trialID string, in LockTrialInput) (domain.ViabilityTrial, error) {
	snap, err := s.catalog.Snapshot(in.Version)
	if err != nil {
		return domain.ViabilityTrial{}, err
	}
	digest, err := rules.Digest(snap)
	if err != nil {
		return domain.ViabilityTrial{}, err
	}
	if in.ExpectedDigest != "" && in.ExpectedDigest != digest {
		return domain.ViabilityTrial{}, domain.New(domain.CodeStaleRuleDigest,
			"expected rule digest %q does not match %q", in.ExpectedDigest, digest)
	}
	if err := s.catalog.Validate(snap); err != nil {
		return domain.ViabilityTrial{}, err
	}

	var already domain.ViabilityTrial
	err = s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		if t.Trial.Locked {
			already = t.Trial
			return nil, nil // already locked: idempotent
		}
		at := s.now()
		return []event{{
			trialID: trialID,
			typ:     evTrialLocked,
			payload: trialLockedPayload{Version: in.Version, Digest: digest, At: at, Snapshot: snap},
		}}, nil
	})
	if err != nil {
		return domain.ViabilityTrial{}, err
	}
	if already.ID != "" {
		return already, nil
	}
	t, unlock, _ := s.readTrial(trialID)
	defer unlock()
	return t.Trial, nil
}

// GetTrial returns the current trial view, or a not-found error.
func (s *Service) GetTrial(trialID string) (domain.ViabilityTrial, error) {
	t, unlock, ok := s.readTrial(trialID)
	if !ok {
		return domain.ViabilityTrial{}, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
	}
	defer unlock()
	return t.Trial, nil
}

func trialID(species string) string {
	sum := sha256.Sum256([]byte(species))
	return hex.EncodeToString(sum[:])[:12]
}

func createDigest(species string) string {
	b, _ := json.Marshal(struct {
		Species string `json:"species"`
	}{species})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
