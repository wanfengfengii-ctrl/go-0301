package service

import (
	"seed-vault-viability-release/internal/domain"
)

// EnvironmentInput is the command payload for recording one environment
// reading during a trial.
type EnvironmentInput struct {
	Dimension   string
	Value       int64
	LogicalTime int64
}

// RecordEnvironment records an environment reading against the locked snapshot.
// The dimension must be declared in the snapshot's environment ranges and the
// logical time must not regress. Readings form part of the observation
// evidence and are replayed on recovery.
func (s *Service) RecordEnvironment(trialID string, in EnvironmentInput) error {
	return s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		if t.Snapshot == nil {
			return nil, domain.New(domain.CodeStageGap, "trial %q is not locked", trialID)
		}
		if !t.hasDimension(in.Dimension) {
			return nil, domain.New(domain.CodeInvalidSchedule,
				"dimension %q is not declared in the rule snapshot", in.Dimension)
		}
		if err := t.advanceClock(in.LogicalTime); err != nil {
			return nil, err
		}
		ev := domain.EnvironmentEvidence{
			ID:          environmentID(trialID, in.Dimension, len(t.Environments)),
			TrialID:     trialID,
			Dimension:   in.Dimension,
			Value:       in.Value,
			LogicalTime: in.LogicalTime,
		}
		return []event{{trialID: trialID, typ: evEnvironmentRecorded,
			payload: environmentRecordedPayload{Evidence: ev}}}, nil
	})
}

// hasDimension reports whether a dimension is declared in the snapshot.
func (t *trialState) hasDimension(d string) bool {
	for _, r := range t.Snapshot.EnvironmentRanges {
		if r.Dimension == d {
			return true
		}
	}
	return false
}

func environmentID(trialID, dimension string, idx int) string {
	return trialID + "-env-" + dimension + "-" + itoa(idx)
}
