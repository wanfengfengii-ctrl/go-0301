package service_test

import (
	"path/filepath"
	"testing"
	"time"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/rules"
	"seed-vault-viability-release/internal/service"
	"seed-vault-viability-release/internal/store"
)

func TestModel_RuleCatalogSliceOwnershipKeepsLockDigestStable(t *testing.T) {
	tests := []struct {
		name           string
		mutateSnapshot bool
		mutate         func(*domain.RuleSnapshot)
	}{
		{
			name: "stages",
			mutate: func(s *domain.RuleSnapshot) {
				s.Stages[0] = domain.StageClosed
			},
		},
		{
			name: "environment ranges",
			mutate: func(s *domain.RuleSnapshot) {
				s.EnvironmentRanges[0].Max = 99
			},
		},
		{
			name: "schedule intervals",
			mutate: func(s *domain.RuleSnapshot) {
				s.Schedule.Intervals[0] = 99 * time.Hour
			},
		},
		{
			name: "qualification roles",
			mutate: func(s *domain.RuleSnapshot) {
				s.Qualification.RequiredRoles[0] = "polluted"
			},
		},
		{
			name:           "snapshot stages",
			mutateSnapshot: true,
			mutate: func(s *domain.RuleSnapshot) {
				s.Stages = append(s.Stages, domain.StageClosed)
			},
		},
		{
			name:           "snapshot environment ranges",
			mutateSnapshot: true,
			mutate: func(s *domain.RuleSnapshot) {
				s.EnvironmentRanges = append(s.EnvironmentRanges, domain.EnvironmentRange{Dimension: "humidity", Min: 30, Max: 60})
			},
		},
		{
			name:           "snapshot schedule intervals",
			mutateSnapshot: true,
			mutate: func(s *domain.RuleSnapshot) {
				s.Schedule.Intervals = append(s.Schedule.Intervals, 48*time.Hour)
			},
		},
		{
			name:           "snapshot qualification roles",
			mutateSnapshot: true,
			mutate: func(s *domain.RuleSnapshot) {
				s.Qualification.RequiredRoles = append(s.Qualification.RequiredRoles, "auditor")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			catalog := rules.NewStandardCatalog()
			configured := domain.RuleSnapshot{
				Species: "Triticum aestivum",
				Stages: []domain.Stage{
					domain.StageWarmup,
					domain.StageSowing,
					domain.StageObservation,
				},
				EnvironmentRanges: []domain.EnvironmentRange{
					{Dimension: "temperature", Min: 12, Max: 24},
				},
				Schedule: domain.ObservationSchedule{
					Intervals: []time.Duration{12 * time.Hour, 24 * time.Hour},
					Window:    time.Hour,
				},
				FixedPointScale: 4,
				Qualification: domain.QualificationRules{
					MinDistinctReviewers: 1,
					RequiredRoles:        []string{"qualified"},
				},
			}
			catalog.Register("v2", configured)

			registered, err := catalog.Snapshot("v2")
			if err != nil {
				t.Fatalf("snapshot registered rule: %v", err)
			}
			registeredDigest, err := rules.Digest(registered)
			if err != nil {
				t.Fatalf("digest registered rule: %v", err)
			}

			if tc.mutateSnapshot {
				tc.mutate(&registered)
			} else {
				tc.mutate(&configured)
			}

			afterMutation, err := catalog.Snapshot("v2")
			if err != nil {
				t.Fatalf("snapshot after caller mutation: %v", err)
			}
			afterDigest, err := rules.Digest(afterMutation)
			if err != nil {
				t.Fatalf("digest after caller mutation: %v", err)
			}
			if afterDigest != registeredDigest {
				t.Fatalf("catalog digest changed after mutating caller-owned %s: got %s, want %s", tc.name, afterDigest, registeredDigest)
			}

			st, err := store.Open(filepath.Join(t.TempDir(), "events.db"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			svc := service.New(st, catalog)
			if err := svc.Recover(); err != nil {
				t.Fatalf("recover service: %v", err)
			}
			trial, err := svc.CreateTrial(service.CreateTrialInput{
				Species:        "Triticum aestivum " + tc.name,
				IdempotencyKey: "ownership-" + tc.name,
			})
			if err != nil {
				t.Fatalf("create trial: %v", err)
			}
			locked, err := svc.LockTrial(trial.ID, service.LockTrialInput{
				Version:        "v2",
				ExpectedDigest: registeredDigest,
			})
			if err != nil {
				t.Fatalf("lock with registration-time digest: %v", err)
			}
			if locked.InputDigest != registeredDigest {
				t.Fatalf("locked digest = %s, want %s", locked.InputDigest, registeredDigest)
			}
		})
	}
}
