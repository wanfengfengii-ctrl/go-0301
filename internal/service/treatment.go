package service

import (
	"seed-vault-viability-release/internal/domain"
)

// deviceStages are the treatment stages that write device-backed evidence and
// therefore require a valid, matching, unexpired lease.
var deviceStages = map[domain.Stage]bool{
	domain.StageSowing:      true,
	domain.StageIncubation:  true,
	domain.StageObservation: true,
}

// TreatmentInput is the command payload for recording one treatment stage.
type TreatmentInput struct {
	PlateID     string
	Stage       domain.Stage
	Operator    string
	Evidence    string
	LeaseID     string
	LogicalTime int64
}

// RecordTreatment records a stage transition for a plate, enforcing the
// contiguous stage prefix, generation match, monotonic logical time and, for
// device-backed stages, a valid matching lease. A gap, expired lease, time
// regression or generation mismatch produces no evidence.
func (s *Service) RecordTreatment(trialID string, in TreatmentInput) error {
	return s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		if !t.Trial.Locked {
			return nil, domain.New(domain.CodeStageGap, "trial %q is not locked", trialID)
		}
		if _, ok := t.Plates[in.PlateID]; !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "plate %q not found", in.PlateID)
		}
		if err := t.advanceClock(in.LogicalTime); err != nil {
			return nil, err
		}

		// Stage-gap check comes before the lease check so a skipped stage is
		// reported as STAGE_GAP regardless of device availability.
		if expected, ok := t.nextStage(in.PlateID); !ok || expected != in.Stage {
			return nil, domain.New(domain.CodeStageGap,
				"expected stage %q after %q, got %q", expected, t.plateStage(in.PlateID), in.Stage)
		}

		if deviceStages[in.Stage] {
			l, ok := t.Leases[in.LeaseID]
			if !ok {
				return nil, domain.New(domain.CodeLeaseExpired, "lease %q required for stage %q", in.LeaseID, in.Stage)
			}
			if l.Holder != in.Operator {
				return nil, domain.New(domain.CodeLeaseConflict,
					"lease %q held by %q, operator is %q", in.LeaseID, l.Holder, in.Operator)
			}
			if l.Generation != t.Trial.CurrentGen {
				return nil, domain.New(domain.CodeGenerationMismatch,
					"lease generation %d != trial generation %d", l.Generation, t.Trial.CurrentGen)
			}
			if in.LogicalTime > l.ExpiresAt {
				return nil, domain.New(domain.CodeLeaseExpired,
					"lease %q expired at %d, logical time %d", in.LeaseID, l.ExpiresAt, in.LogicalTime)
			}
		}

		ev := domain.TreatmentEvent{
			ID:          treatmentEventID(trialID, in.PlateID, in.Stage),
			TrialID:     trialID,
			PlateID:     in.PlateID,
			Stage:       in.Stage,
			Operator:    in.Operator,
			LogicalTime: in.LogicalTime,
			Evidence:    in.Evidence,
			Generation:  t.Trial.CurrentGen,
		}
		return []event{{trialID: trialID, typ: evTreatmentRecorded,
			payload: treatmentRecordedPayload{Event: ev}}}, nil
	})
}

func treatmentEventID(trialID, plateID string, stage domain.Stage) string {
	return trialID + "-" + plateID + "-" + string(stage)
}
