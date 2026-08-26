package service

import (
	"reflect"
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/lineage"
	"seed-vault-viability-release/internal/rules"
	"seed-vault-viability-release/internal/store"
)

func TestModel_CrossAllocationDuplicateIdentityDoesNotOverwriteLineage(t *testing.T) {
	tests := []struct {
		name      string
		duplicate func(*AllocateInput)
	}{
		{
			name: "seed lot",
			duplicate: func(in *AllocateInput) {
				in.SeedLots = []domain.SeedLot{{
					ID: "lot-1", ParentID: "collection-1", Species: "Triticum aestivum", Location: "warm-9", Count: 999,
				}}
			},
		},
		{
			name: "sample unit",
			duplicate: func(in *AllocateInput) {
				in.Samples = append(in.Samples, domain.SampleUnit{
					ID: "sample-1", SeedLotID: "lot-1", Count: 999, Moisture: 99,
				})
			},
		},
		{
			name: "culture group",
			duplicate: func(in *AllocateInput) {
				in.Groups = append(in.Groups, domain.CultureGroup{
					ID: "group-1", SampleID: "sample-1", SeedLotID: "lot-1", Generation: 1, Count: 999,
				})
			},
		},
		{
			name: "plate",
			duplicate: func(in *AllocateInput) {
				in.Plates = append(in.Plates, domain.Plate{
					ID: "plate-1", GroupID: "group-1", Position: 99, Generation: 1, Sown: 999,
				})
			},
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			svc := New(st, rules.NewStandardCatalog())
			if err := svc.Recover(); err != nil {
				t.Fatalf("recover service: %v", err)
			}
			trial, err := svc.CreateTrial(CreateTrialInput{
				Species: "Oryza persistence " + tc.name, IdempotencyKey: "identity-persistence-" + tc.name,
			})
			if err != nil {
				t.Fatalf("create trial: %v", err)
			}
			if _, err := svc.LockTrial(trial.ID, LockTrialInput{Version: "v1"}); err != nil {
				t.Fatalf("lock trial: %v", err)
			}
			if err := svc.AllocateSamples(trial.ID, AllocateInput{
				SampleID:   "sample-1",
				Allocation: lineage.Allocation{Source: 100, Culture: 60, Retain: 20, Measurement: 10, Quarantine: 5, Loss: 5},
				SeedLots:   []domain.SeedLot{{ID: "lot-1", ParentID: "collection-1", Species: "Oryza sativa", Location: "cold-1", Count: 500}},
				Samples:    []domain.SampleUnit{{ID: "sample-1", SeedLotID: "lot-1", Count: 100, Moisture: 8}},
				Groups:     []domain.CultureGroup{{ID: "group-1", SampleID: "sample-1", SeedLotID: "lot-1", Generation: 1, Count: 60}},
				Plates:     []domain.Plate{{ID: "plate-1", GroupID: "group-1", Position: 0, Generation: 1, Sown: 60}},
			}); err != nil {
				t.Fatalf("initial allocation: %v", err)
			}

			before, err := svc.Lineage(trial.ID)
			if err != nil {
				t.Fatalf("lineage before duplicate allocation: %v", err)
			}
			eventsBefore, err := st.EventsSince(0)
			if err != nil {
				t.Fatalf("events before duplicate allocation: %v", err)
			}

			in := AllocateInput{
				SampleID:   "sample-2",
				Allocation: lineage.Allocation{Source: 40, Culture: 20, Retain: 10, Measurement: 5, Quarantine: 3, Loss: 2},
				SeedLots:   []domain.SeedLot{{ID: "lot-2", ParentID: "collection-2", Species: "Oryza sativa", Location: "cold-2", Count: 400}},
				Samples:    []domain.SampleUnit{{ID: "sample-2", SeedLotID: "lot-2", Count: 40, Moisture: 7}},
				Groups:     []domain.CultureGroup{{ID: "group-2", SampleID: "sample-2", SeedLotID: "lot-2", Generation: 1, Count: 20}},
				Plates:     []domain.Plate{{ID: "plate-2", GroupID: "group-2", Position: i + 1, Generation: 1, Sown: 20}},
			}
			tc.duplicate(&in)

			if err := svc.AllocateSamples(trial.ID, in); !domain.IsCode(err, domain.CodeDuplicateSampleID) {
				t.Fatalf("duplicate allocation error = %v, want DUPLICATE_SAMPLE_ID", err)
			}

			after, err := svc.Lineage(trial.ID)
			if err != nil {
				t.Fatalf("lineage after duplicate allocation: %v", err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("rejected allocation changed lineage\nbefore: %+v\nafter:  %+v", before, after)
			}
			eventsAfter, err := st.EventsSince(0)
			if err != nil {
				t.Fatalf("events after duplicate allocation: %v", err)
			}
			if len(eventsAfter) != len(eventsBefore) {
				t.Fatalf("rejected allocation appended events: before=%d after=%d", len(eventsBefore), len(eventsAfter))
			}
		})
	}
}
