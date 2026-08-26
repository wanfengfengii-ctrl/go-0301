package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/httpapi"
	"seed-vault-viability-release/internal/lineage"
	"seed-vault-viability-release/internal/rules"
	"seed-vault-viability-release/internal/service"
	"seed-vault-viability-release/internal/store"
)

func TestModel_LineageConcurrentAllocation(t *testing.T) {
	tests := []struct {
		name                string
		preloaded           int
		concurrentWrites    int
		readers             int
		readsPerReader      int
		failNextPersistence bool
	}{
		{
			name:             "concurrent lineage reads observe complete sorted allocations",
			preloaded:        128,
			concurrentWrites: 64,
			readers:          12,
			readsPerReader:   50,
		},
		{
			name:                "failed event persistence does not update the projection",
			preloaded:           4,
			failNextPersistence: true,
		},
	}

	for caseIndex, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "events.db"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			svc := service.New(st, rules.NewStandardCatalog())
			if err := svc.Recover(); err != nil {
				t.Fatalf("recover: %v", err)
			}
			h := httpapi.NewServer(svc).Handler()

			do := func(method, path string, body []byte) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, bytes.NewReader(body))
				if body != nil {
					req.Header.Set("Content-Type", "application/json")
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				return rec
			}

			createBody, _ := json.Marshal(map[string]string{
				"species":         fmt.Sprintf("Oryza concurrent %d", caseIndex),
				"idempotency_key": fmt.Sprintf("concurrent-key-%d", caseIndex),
			})
			rec := do(http.MethodPost, "/api/trials", createBody)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
			}
			var created struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
				t.Fatalf("decode created trial: %v", err)
			}
			rec = do(http.MethodPost, "/api/trials/"+created.ID+"/lock", []byte(`{"version":"v1"}`))
			if rec.Code != http.StatusOK {
				t.Fatalf("lock status = %d body=%s", rec.Code, rec.Body.String())
			}
			lineagePath := "/api/trials/" + created.ID + "/lineage"
			allocatePath := "/api/trials/" + created.ID + "/samples/allocate"

			allocationBody := func(i int) []byte {
				suffix := fmt.Sprintf("%04d", i)
				body, err := json.Marshal(struct {
					SampleID   string                `json:"sample_id"`
					Allocation lineage.Allocation    `json:"allocation"`
					SeedLots   []domain.SeedLot      `json:"seed_lots"`
					Samples    []domain.SampleUnit   `json:"samples"`
					Groups     []domain.CultureGroup `json:"groups"`
					Plates     []domain.Plate        `json:"plates"`
				}{
					SampleID:   "sample-" + suffix,
					Allocation: lineage.Allocation{Source: 100, Culture: 60, Retain: 20, Measurement: 10, Quarantine: 5, Loss: 5},
					SeedLots:   []domain.SeedLot{{ID: "lot-" + suffix, ParentID: "collection-" + suffix, Species: "Oryza sativa", Location: "cold", Count: 100}},
					Samples:    []domain.SampleUnit{{ID: "sample-" + suffix, SeedLotID: "lot-" + suffix, Count: 100, Moisture: 8}},
					Groups:     []domain.CultureGroup{{ID: "group-" + suffix, TrialID: created.ID, SampleID: "sample-" + suffix, SeedLotID: "lot-" + suffix, Generation: 1, Count: 60}},
					Plates:     []domain.Plate{{ID: "plate-" + suffix, GroupID: "group-" + suffix, Position: i, Generation: 1, Sown: 60}},
				})
				if err != nil {
					t.Fatalf("marshal allocation %d: %v", i, err)
				}
				return body
			}

			for i := 0; i < tc.preloaded; i++ {
				rec := do(http.MethodPost, allocatePath, allocationBody(i))
				if rec.Code != http.StatusCreated {
					t.Fatalf("preload allocation %d status = %d body=%s", i, rec.Code, rec.Body.String())
				}
			}

			validateView := func(body []byte) error {
				var view service.LineageView
				if err := json.Unmarshal(body, &view); err != nil {
					return fmt.Errorf("decode lineage: %w", err)
				}
				if view.TrialID != created.ID || !view.Conserved {
					return fmt.Errorf("trial_id/conserved = %q/%v", view.TrialID, view.Conserved)
				}
				if !sort.SliceIsSorted(view.SeedLots, func(i, j int) bool { return view.SeedLots[i].ID < view.SeedLots[j].ID }) ||
					!sort.SliceIsSorted(view.Samples, func(i, j int) bool { return view.Samples[i].ID < view.Samples[j].ID }) ||
					!sort.SliceIsSorted(view.Groups, func(i, j int) bool { return view.Groups[i].ID < view.Groups[j].ID }) ||
					!sort.SliceIsSorted(view.Plates, func(i, j int) bool { return view.Plates[i].ID < view.Plates[j].ID }) ||
					!sort.SliceIsSorted(view.Allocations, func(i, j int) bool { return view.Allocations[i].SampleID < view.Allocations[j].SampleID }) ||
					!sort.SliceIsSorted(view.Edges, func(i, j int) bool {
						if view.Edges[i].ParentID != view.Edges[j].ParentID {
							return view.Edges[i].ParentID < view.Edges[j].ParentID
						}
						return view.Edges[i].ChildID < view.Edges[j].ChildID
					}) {
					return fmt.Errorf("lineage identities or edges are not canonically sorted")
				}
				n := len(view.Samples)
				if len(view.SeedLots) != n || len(view.Groups) != n || len(view.Plates) != n || len(view.Allocations) != n || len(view.Edges) != 4*n {
					return fmt.Errorf("partial lineage counts: lots=%d samples=%d groups=%d plates=%d allocations=%d edges=%d", len(view.SeedLots), n, len(view.Groups), len(view.Plates), len(view.Allocations), len(view.Edges))
				}
				for _, a := range view.Allocations {
					if !a.Conserved || a.Allocation.Total() != a.Allocation.Source {
						return fmt.Errorf("unconserved allocation for %q: %+v", a.SampleID, a.Allocation)
					}
				}
				return nil
			}

			if tc.failNextPersistence {
				if err := st.Close(); err != nil {
					t.Fatalf("close store: %v", err)
				}
				rec := do(http.MethodPost, allocatePath, allocationBody(tc.preloaded))
				if rec.Code != http.StatusInternalServerError {
					t.Fatalf("allocation after store close status = %d, want 500; body=%s", rec.Code, rec.Body.String())
				}
				rec = do(http.MethodGet, lineagePath, nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("lineage after failed persistence status = %d body=%s", rec.Code, rec.Body.String())
				}
				if err := validateView(rec.Body.Bytes()); err != nil {
					t.Fatal(err)
				}
				var view service.LineageView
				_ = json.Unmarshal(rec.Body.Bytes(), &view)
				if len(view.Allocations) != tc.preloaded {
					t.Fatalf("projection contains failed allocation: got %d allocations, want %d", len(view.Allocations), tc.preloaded)
				}
				return
			}

			start := make(chan struct{})
			errs := make(chan error, tc.concurrentWrites+tc.readers)
			var wg sync.WaitGroup
			for i := 0; i < tc.concurrentWrites; i++ {
				i := i
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					rec := do(http.MethodPost, allocatePath, allocationBody(tc.preloaded+i))
					if rec.Code != http.StatusCreated {
						errs <- fmt.Errorf("allocation %d status = %d body=%s", i, rec.Code, rec.Body.String())
					}
				}()
			}
			for i := 0; i < tc.readers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					for read := 0; read < tc.readsPerReader; read++ {
						rec := do(http.MethodGet, lineagePath, nil)
						if rec.Code != http.StatusOK {
							errs <- fmt.Errorf("lineage status = %d body=%s", rec.Code, rec.Body.String())
							return
						}
						if err := validateView(rec.Body.Bytes()); err != nil {
							errs <- err
							return
						}
					}
				}()
			}
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Error(err)
			}

			rec = do(http.MethodGet, lineagePath, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("final lineage status = %d body=%s", rec.Code, rec.Body.String())
			}
			if err := validateView(rec.Body.Bytes()); err != nil {
				t.Fatal(err)
			}
			var final service.LineageView
			if err := json.Unmarshal(rec.Body.Bytes(), &final); err != nil {
				t.Fatalf("decode final lineage: %v", err)
			}
			want := tc.preloaded + tc.concurrentWrites
			if len(final.Allocations) != want {
				t.Fatalf("final allocations = %d, want %d", len(final.Allocations), want)
			}
		})
	}
}
