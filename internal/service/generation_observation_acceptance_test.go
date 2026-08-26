package service

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/lineage"
)

func TestModel_observationRequiresCurrentGeneration(t *testing.T) {
	tests := []struct {
		name              string
		plateID           string
		primeCounts       map[domain.ObservationClass]int64
		counts            map[domain.ObservationClass]int64
		wantCode          domain.ErrorCode
		wantEventDelta    int64
		wantClock         int64
		wantGermination   int64
		wantContamination int64
	}{
		{
			name:           "old generation plate is rejected without side effects",
			plateID:        "plate-1",
			counts:         map[domain.ObservationClass]int64{domain.ClassGerminated: 30},
			wantCode:       domain.CodeGenerationMismatch,
			wantEventDelta: 0,
			wantClock:      0,
		},
		{
			name:              "current generation plate retains metrics behavior",
			plateID:           "plate-2",
			counts:            map[domain.ObservationClass]int64{domain.ClassGerminated: 30, domain.ClassDecayed: 6},
			wantEventDelta:    1,
			wantClock:         100,
			wantGermination:   5000,
			wantContamination: 1000,
		},
		{
			name:            "current generation counts remain monotonic",
			plateID:         "plate-2",
			primeCounts:     map[domain.ObservationClass]int64{domain.ClassGerminated: 30},
			counts:          map[domain.ObservationClass]int64{domain.ClassGerminated: 29},
			wantCode:        domain.CodeObservationRegression,
			wantEventDelta:  0,
			wantClock:       100,
			wantGermination: 5000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t)
			id := createLockedTrial(t, svc, "Oryza generation "+tt.name)
			allocateFixture(t, svc, id)

			if _, err := svc.GenerateRetest(id, RetestInput{
				Reason: "contamination",
				Members: []domain.RetestMember{{
					SeedLotID: "lot-1", SampleID: "sample-1", GroupID: "group-1", PlateIndex: 0,
				}},
			}); err != nil {
				t.Fatalf("generate retest: %v", err)
			}
			if err := svc.AllocateSamples(id, AllocateInput{
				SampleID:   "sample-2",
				Allocation: lineage.Allocation{Source: 100, Culture: 60, Retain: 20, Measurement: 10, Quarantine: 5, Loss: 5},
				SeedLots:   []domain.SeedLot{{ID: "lot-2", ParentID: "collection-2", Species: "Oryza sativa", Location: "cold-1", Count: 500}},
				Samples:    []domain.SampleUnit{{ID: "sample-2", SeedLotID: "lot-2", Count: 100, Moisture: 8}},
				Groups:     []domain.CultureGroup{{ID: "group-2", SampleID: "sample-2", SeedLotID: "lot-2", Generation: 2, Count: 60}},
				Plates:     []domain.Plate{{ID: "plate-2", GroupID: "group-2", Position: 0, Generation: 2, Sown: 60}},
			}); err != nil {
				t.Fatalf("allocate current generation: %v", err)
			}

			if tt.primeCounts != nil {
				if err := svc.RecordObservation(id, ObservationInput{
					PlateID: tt.plateID, Counts: tt.primeCounts, Operator: "op", LogicalTime: 100,
				}); err != nil {
					t.Fatalf("prime observation: %v", err)
				}
			}
			before, err := svc.store.LastSeq()
			if err != nil {
				t.Fatalf("last sequence before observation: %v", err)
			}

			err = svc.RecordObservation(id, ObservationInput{
				PlateID: tt.plateID, Counts: tt.counts, Operator: "op", LogicalTime: 100,
			})
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("record observation: %v", err)
				}
			} else if !domain.IsCode(err, tt.wantCode) {
				t.Fatalf("record observation error = %v, want %s", err, tt.wantCode)
			}

			after, err := svc.store.LastSeq()
			if err != nil {
				t.Fatalf("last sequence after observation: %v", err)
			}
			if got := after - before; got != tt.wantEventDelta {
				t.Fatalf("persisted event delta = %d, want %d", got, tt.wantEventDelta)
			}
			events, err := svc.store.EventsSince(before)
			if err != nil {
				t.Fatalf("events after observation: %v", err)
			}
			for _, event := range events {
				if tt.wantCode != "" && event.Type == evObservationRecorded {
					t.Fatalf("rejected observation appended %q event", event.Type)
				}
			}

			trial, err := svc.GetTrial(id)
			if err != nil {
				t.Fatalf("get trial: %v", err)
			}
			if trial.CurrentGen != 2 {
				t.Fatalf("current generation = %d, want 2", trial.CurrentGen)
			}
			if trial.LogicalClock != tt.wantClock {
				t.Fatalf("logical clock = %d, want %d", trial.LogicalClock, tt.wantClock)
			}

			if tt.wantGermination != 0 || tt.wantContamination != 0 {
				metrics, err := svc.PlateMetrics(id, tt.plateID)
				if err != nil {
					t.Fatalf("plate metrics: %v", err)
				}
				if metrics.GerminationRate.Raw != tt.wantGermination {
					t.Fatalf("germination rate = %d, want %d", metrics.GerminationRate.Raw, tt.wantGermination)
				}
				if metrics.ContaminationRate.Raw != tt.wantContamination {
					t.Fatalf("contamination rate = %d, want %d", metrics.ContaminationRate.Raw, tt.wantContamination)
				}
			}
		})
	}
}
