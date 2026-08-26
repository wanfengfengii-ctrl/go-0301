package service

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
)

func TestModel_RejectedObservationDoesNotAdvancePersistedState(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza rejected observation clock")
	allocateFixture(t, svc, id)

	baseSeq, err := svc.store.LastSeq()
	if err != nil {
		t.Fatalf("read initial event sequence: %v", err)
	}

	tests := []struct {
		name        string
		logicalTime int64
		counts      map[domain.ObservationClass]int64
		wantCode    domain.ErrorCode
		wantClock   int64
		wantHistory int
		wantEvents  int64
	}{
		{
			name:        "over-sown observation at a future time is rejected without state",
			logicalTime: 300,
			counts:      map[domain.ObservationClass]int64{domain.ClassGerminated: 61},
			wantCode:    domain.CodeObservationRegression,
			wantClock:   0,
			wantHistory: 0,
			wantEvents:  0,
		},
		{
			name:        "valid observation before rejected time succeeds",
			logicalTime: 200,
			counts:      map[domain.ObservationClass]int64{domain.ClassGerminated: 30, domain.ClassHard: 10},
			wantClock:   200,
			wantHistory: 1,
			wantEvents:  1,
		},
		{
			name:        "committed observation advances the clock",
			logicalTime: 250,
			counts:      map[domain.ObservationClass]int64{domain.ClassGerminated: 35, domain.ClassHard: 10},
			wantClock:   250,
			wantHistory: 2,
			wantEvents:  2,
		},
		{
			name:        "time before latest committed evidence still regresses",
			logicalTime: 225,
			counts:      map[domain.ObservationClass]int64{domain.ClassGerminated: 40, domain.ClassHard: 10},
			wantCode:    domain.CodeTimeRegression,
			wantClock:   250,
			wantHistory: 2,
			wantEvents:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.RecordObservation(id, ObservationInput{
				PlateID: "plate-1", Counts: tt.counts, Operator: "operator-1", LogicalTime: tt.logicalTime,
			})
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("RecordObservation() error = %v, want nil", err)
				}
			} else if !domain.IsCode(err, tt.wantCode) {
				t.Fatalf("RecordObservation() error = %v, want %s", err, tt.wantCode)
			}

			trial, err := svc.GetTrial(id)
			if err != nil {
				t.Fatalf("GetTrial() error: %v", err)
			}
			if trial.LogicalClock != tt.wantClock {
				t.Errorf("LogicalClock = %d, want %d", trial.LogicalClock, tt.wantClock)
			}

			state, ok := svc.readTrial(id)
			if !ok {
				t.Fatal("trial projection disappeared")
			}
			if got := len(state.Observations["plate-1"]); got != tt.wantHistory {
				t.Errorf("observation history length = %d, want %d", got, tt.wantHistory)
			}
			if got := len(state.Treatments); got != 0 {
				t.Errorf("treatment stage sequence length = %d, want 0", got)
			}

			seq, err := svc.store.LastSeq()
			if err != nil {
				t.Fatalf("LastSeq() error: %v", err)
			}
			if got := seq - baseSeq; got != tt.wantEvents {
				t.Errorf("persisted event count = %d, want %d", got, tt.wantEvents)
			}
		})
	}
}
