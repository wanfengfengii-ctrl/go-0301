package store_test

import (
	"testing"

	"seed-vault-viability-release/internal/rules"
	"seed-vault-viability-release/internal/service"
	"seed-vault-viability-release/internal/store"
)

func TestModel_MemoryOpenIsolation(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{name: "memory sentinel", path: ":memory:"},
		{name: "empty path", path: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first, err := store.Open(tc.path)
			if err != nil {
				t.Fatalf("open first store: %v", err)
			}
			t.Cleanup(func() { _ = first.Close() })

			second, err := store.Open(tc.path)
			if err != nil {
				t.Fatalf("open second store: %v", err)
			}
			t.Cleanup(func() { _ = second.Close() })

			firstService := service.New(first, rules.NewStandardCatalog())
			secondService := service.New(second, rules.NewStandardCatalog())
			if err := firstService.Recover(); err != nil {
				t.Fatalf("recover first service: %v", err)
			}
			if err := secondService.Recover(); err != nil {
				t.Fatalf("recover second service: %v", err)
			}

			firstTrial, err := firstService.CreateTrial(service.CreateTrialInput{
				Species:        "first-service-species",
				IdempotencyKey: "shared-idempotency-key",
			})
			if err != nil {
				t.Fatalf("create trial in first service: %v", err)
			}

			if seq, err := second.LastSeq(); err != nil || seq != 0 {
				t.Fatalf("second store chain head after first write = %d, err = %v; want 0, nil", seq, err)
			}
			if _, found, err := second.GetIdempotency("shared-idempotency-key"); err != nil || found {
				t.Fatalf("second store idempotency leaked from first: found=%v err=%v", found, err)
			}

			secondTrial, err := secondService.CreateTrial(service.CreateTrialInput{
				Species:        "second-service-species",
				IdempotencyKey: "shared-idempotency-key",
			})
			if err != nil {
				t.Fatalf("create independent trial in second service: %v", err)
			}

			restartedSecond := service.New(second, rules.NewStandardCatalog())
			if err := restartedSecond.Recover(); err != nil {
				t.Fatalf("recover restarted second service: %v", err)
			}
			summaries := restartedSecond.TrialSummaries()
			if len(summaries) != 1 || summaries[0].ID != secondTrial.ID {
				t.Fatalf("restarted second service trials = %+v; want only %q (and not %q)", summaries, secondTrial.ID, firstTrial.ID)
			}

			third, err := store.Open(tc.path)
			if err != nil {
				t.Fatalf("open third store: %v", err)
			}
			t.Cleanup(func() { _ = third.Close() })
			if seq, err := third.LastSeq(); err != nil || seq != 0 {
				t.Fatalf("new memory store chain head = %d, err = %v; want 0, nil", seq, err)
			}
			if events, err := third.EventsSince(0); err != nil || len(events) != 0 {
				t.Fatalf("new memory store events = %+v, err = %v; want none", events, err)
			}
		})
	}
}
