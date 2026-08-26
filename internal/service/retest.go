package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/observation"
)

// RetestInput is the command payload for generating a normalized retest set
// from an anomaly source.
type RetestInput struct {
	Reason  string
	Members []domain.RetestMember
}

// GenerateRetest creates the canonical, ordered retest set for an anomaly
// source and opens a new, isolated generation. The same anomaly source in the
// same generation yields exactly one retest set; members are sorted by seed
// lot, sample, group and plate so the result is stable across restarts.
func (s *Service) GenerateRetest(trialID string, in RetestInput) (domain.RetestSet, error) {
	if in.Reason == "" {
		return domain.RetestSet{}, domain.New(domain.CodeInvalidSampleCount, "retest reason is required")
	}
	members := append([]domain.RetestMember(nil), in.Members...)
	observation.SortRetestMembers(members)

	var out domain.RetestSet
	err := s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		if !t.Trial.Locked {
			return nil, domain.New(domain.CodeStageGap, "trial %q is not locked", trialID)
		}
		sourceGen := t.Trial.CurrentGen
		key := retestKey(sourceGen, in.Reason)
		if existing, ok := t.Retests[key]; ok {
			out = existing
			return nil, nil // unique per anomaly source + generation
		}
		newGen := sourceGen + 1
		rs := domain.RetestSet{
			ID:        retestID(trialID, sourceGen, in.Reason),
			TrialID:   trialID,
			SourceGen: sourceGen,
			TargetGen: newGen,
			Reason:    in.Reason,
			Members:   members,
			Digest:    retestDigest(trialID, sourceGen, in.Reason, members),
			Complete:  false,
		}
		out = rs
		return []event{{trialID: trialID, typ: evRetestCreated,
			payload: retestCreatedPayload{RetestSet: rs, NewGen: newGen, At: s.now()}}}, nil
	})
	return out, err
}

// GetRetest returns a retest set by its source generation and reason, or by its
// id if the reason is empty.
func (s *Service) GetRetest(trialID string, sourceGen domain.GenerationNumber, reason string) (domain.RetestSet, error) {
	t, unlock, ok := s.readTrial(trialID)
	if !ok {
		return domain.RetestSet{}, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
	}
	defer unlock()
	if reason == "" {
		// find by source generation (the unique set that opened it)
		for _, rs := range t.Retests {
			if rs.SourceGen == sourceGen {
				return rs, nil
			}
		}
		return domain.RetestSet{}, domain.New(domain.CodeInvalidSampleCount,
			"no retest set for generation %d", sourceGen)
	}
	rs, ok := t.Retests[retestKey(sourceGen, reason)]
	if !ok {
		return domain.RetestSet{}, domain.New(domain.CodeInvalidSampleCount,
			"no retest set for generation %d reason %q", sourceGen, reason)
	}
	return rs, nil
}

func retestKey(sourceGen domain.GenerationNumber, reason string) string {
	return fmt.Sprintf("%d:%s", sourceGen, reason)
}

func retestID(trialID string, sourceGen domain.GenerationNumber, reason string) string {
	return fmt.Sprintf("%s-retest-%d-%s", trialID, sourceGen, reason)
}

func retestDigest(trialID string, sourceGen domain.GenerationNumber, reason string, members []domain.RetestMember) string {
	b, _ := json.Marshal(struct {
		TrialID   string
		SourceGen domain.GenerationNumber
		Reason    string
		Members   []domain.RetestMember
	}{trialID, sourceGen, reason, members})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
