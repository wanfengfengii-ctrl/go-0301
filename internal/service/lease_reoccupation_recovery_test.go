package service

import (
	"path/filepath"
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/rules"
	"seed-vault-viability-release/internal/store"
)

func TestModel_LeaseReoccupationRecovery(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, svc *Service, st *store.Store, trialID string)
	}{
		{
			name: "superseded lease cannot release the recovered current lease",
			run: func(t *testing.T, svc *Service, st *store.Store, trialID string) {
				before, err := st.LastSeq()
				if err != nil {
					t.Fatalf("last sequence before stale release: %v", err)
				}
				err = svc.ReleaseLease(trialID, "l1", "holder-1")
				if !domain.IsCode(err, domain.CodeLeaseExpired) && !domain.IsCode(err, domain.CodeLeaseConflict) {
					t.Fatalf("stale release error = %v, want LEASE_EXPIRED or LEASE_CONFLICT", err)
				}
				afterRelease, err := st.LastSeq()
				if err != nil {
					t.Fatalf("last sequence after stale release: %v", err)
				}
				if afterRelease != before {
					t.Fatalf("stale release appended an event: last sequence = %d, want %d", afterRelease, before)
				}

				if _, err := svc.AcquireLease(trialID, LeaseAcquireInput{
					ID: "l3", Resource: "incubator-1", Kind: domain.ResourceIncubator,
					Holder: "holder-3", Purpose: "culture", Generation: 1, Now: 12, Duration: 100,
				}); !domain.IsCode(err, domain.CodeLeaseConflict) {
					t.Fatalf("third acquire error = %v, want LEASE_CONFLICT", err)
				}
				afterAcquire, err := st.LastSeq()
				if err != nil {
					t.Fatalf("last sequence after conflicting acquire: %v", err)
				}
				if afterAcquire != before {
					t.Fatalf("conflicting acquire appended an event: last sequence = %d, want %d", afterAcquire, before)
				}
			},
		},
		{
			name: "unreplaced recovered lease still renews and releases",
			run: func(t *testing.T, svc *Service, _ *store.Store, trialID string) {
				renewed, err := svc.RenewLease(trialID, "l1", "holder-1", 50, 100)
				if err != nil {
					t.Fatalf("renew recovered lease: %v", err)
				}
				if renewed.ExpiresAt != 150 || renewed.Version != 2 {
					t.Fatalf("renewed lease = %+v, want expiry 150 and version 2", renewed)
				}
				if err := svc.ReleaseLease(trialID, "l1", "holder-1"); err != nil {
					t.Fatalf("release recovered lease: %v", err)
				}
				if _, err := svc.AcquireLease(trialID, LeaseAcquireInput{
					ID: "l2", Resource: "incubator-1", Kind: domain.ResourceIncubator,
					Holder: "holder-2", Purpose: "culture", Generation: 1, Now: 51, Duration: 100,
				}); err != nil {
					t.Fatalf("acquire released resource: %v", err)
				}
			},
		},
		{
			name: "unreplaced expired recovered lease keeps existing behavior",
			run: func(t *testing.T, svc *Service, _ *store.Store, trialID string) {
				if _, err := svc.RenewLease(trialID, "l1", "holder-1", 101, 100); !domain.IsCode(err, domain.CodeLeaseExpired) {
					t.Fatalf("renew expired recovered lease error = %v, want LEASE_EXPIRED", err)
				}
				if err := svc.ReleaseLease(trialID, "l1", "holder-1"); err != nil {
					t.Fatalf("release unreplaced expired lease: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "leases.db")
			st, err := store.Open(path)
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			svc := New(st, rules.NewStandardCatalog())
			if err := svc.Recover(); err != nil {
				t.Fatalf("initial recover: %v", err)
			}
			trialID := createLockedTrial(t, svc, "Oryza "+tc.name)

			if tc.name == "superseded lease cannot release the recovered current lease" {
				if _, err := svc.AcquireLease(trialID, LeaseAcquireInput{
					ID: "l1", Resource: "incubator-1", Kind: domain.ResourceIncubator,
					Holder: "holder-1", Purpose: "culture", Generation: 1, Now: 0, Duration: 10,
				}); err != nil {
					t.Fatalf("acquire l1: %v", err)
				}
				if _, err := svc.AcquireLease(trialID, LeaseAcquireInput{
					ID: "l2", Resource: "incubator-1", Kind: domain.ResourceIncubator,
					Holder: "holder-2", Purpose: "culture", Generation: 1, Now: 11, Duration: 100,
				}); err != nil {
					t.Fatalf("acquire l2: %v", err)
				}
			} else {
				if _, err := svc.AcquireLease(trialID, LeaseAcquireInput{
					ID: "l1", Resource: "incubator-1", Kind: domain.ResourceIncubator,
					Holder: "holder-1", Purpose: "culture", Generation: 1, Now: 0, Duration: 100,
				}); err != nil {
					t.Fatalf("acquire l1: %v", err)
				}
			}
			if err := st.Close(); err != nil {
				t.Fatalf("close before restart: %v", err)
			}

			reopened, err := store.Open(path)
			if err != nil {
				t.Fatalf("reopen store: %v", err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			recovered := New(reopened, rules.NewStandardCatalog())
			if err := recovered.Recover(); err != nil {
				t.Fatalf("recover after restart: %v", err)
			}
			tc.run(t, recovered, reopened, trialID)
		})
	}
}
