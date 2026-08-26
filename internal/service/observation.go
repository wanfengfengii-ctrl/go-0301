package service

import (
	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/observation"
)

// ObservationInput is the command payload for recording one plate observation.
type ObservationInput struct {
	PlateID     string
	Counts      map[domain.ObservationClass]int64
	Operator    string
	LogicalTime int64
}

// RecordObservation records a plate's classification counts, enforcing the
// mutually-exclusive, monotonic and bounded classification invariants against
// the previous observation, plus generation match and monotonic logical time.
// Invalid counts or a time regression leave the valid observation history
// unchanged.
func (s *Service) RecordObservation(trialID string, in ObservationInput) error {
	return s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		pl, ok := t.Plates[in.PlateID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "plate %q not found", in.PlateID)
		}
		if err := t.advanceClock(in.LogicalTime); err != nil {
			return nil, err
		}
		history := t.Observations[in.PlateID]
		var prev map[domain.ObservationClass]int64
		if len(history) > 0 {
			prev = history[len(history)-1].Counts
		}
		if err := observation.ValidateCounts(prev, in.Counts, pl.Sown); err != nil {
			return nil, err
		}
		o := domain.Observation{
			ID:          observationID(trialID, in.PlateID, len(history)),
			PlateID:     in.PlateID,
			Generation:  t.Trial.CurrentGen,
			LogicalTime: in.LogicalTime,
			Counts:      observation.ClassCounts(in.Counts),
			Operator:    in.Operator,
		}
		return []event{{trialID: trialID, typ: evObservationRecorded,
			payload: observationRecordedPayload{Observation: o}}}, nil
	})
}

// PlateMetrics returns the viability metrics for a plate's latest observation,
// or an error if the plate has no observations yet.
func (s *Service) PlateMetrics(trialID, plateID string) (observation.Metrics, error) {
	t, unlock, ok := s.readTrial(trialID)
	if !ok {
		return observation.Metrics{}, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
	}
	defer unlock()
	pl, ok := t.Plates[plateID]
	if !ok {
		return observation.Metrics{}, domain.New(domain.CodeInvalidSampleCount, "plate %q not found", plateID)
	}
	history := t.Observations[plateID]
	if len(history) == 0 {
		return observation.Metrics{}, domain.New(domain.CodeInvalidSampleCount, "plate %q has no observations", plateID)
	}
	scale := 2
	if t.Snapshot != nil {
		scale = t.Snapshot.FixedPointScale
	}
	return observation.ComputeMetrics(history[len(history)-1].Counts, pl.Sown, scale)
}

func observationID(trialID, plateID string, idx int) string {
	return trialID + "-" + plateID + "-obs-" + itoa(idx)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
