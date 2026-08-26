package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"seed-vault-viability-release/internal/httpapi"
	"seed-vault-viability-release/internal/rules"
	"seed-vault-viability-release/internal/service"
	"seed-vault-viability-release/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st, rules.NewStandardCatalog())
	if err := svc.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	return httpapi.NewServer(svc).Handler()
}

func postJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func getJSON(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestHTTPEndToEnd drives the full locked loop through HTTP: create, lock,
// allocate, treat, observe, double review, terminal decision and credential.
func TestHTTPEndToEnd(t *testing.T) {
	h := newTestServer(t)

	rec := postJSON(t, h, "/api/trials", `{"species":"Oryza sativa","idempotency_key":"k-e2e"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var trial struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &trial); err != nil {
		t.Fatalf("decode trial: %v", err)
	}
	id := trial.ID

	rec = postJSON(t, h, "/api/trials/"+id+"/lock", `{"version":"v1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("lock status = %d body=%s", rec.Code, rec.Body.String())
	}

	alloc := `{
		"sample_id":"sample-1",
		"allocation":{"source":100,"culture":60,"retain":20,"measurement":10,"quarantine":5,"loss":5},
		"seed_lots":[{"id":"lot-1","parent_id":"collection-1","species":"Oryza sativa","location":"cold-1","count":500}],
		"samples":[{"id":"sample-1","seed_lot_id":"lot-1","count":100,"moisture":8}],
		"groups":[{"id":"group-1","sample_id":"sample-1","seed_lot_id":"lot-1","generation":1,"count":60}],
		"plates":[{"id":"plate-1","group_id":"group-1","position":0,"generation":1,"sown":60}]
	}`
	rec = postJSON(t, h, "/api/trials/"+id+"/samples/allocate", alloc)
	if rec.Code != http.StatusCreated {
		t.Fatalf("allocate status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = postJSON(t, h, "/api/trials/"+id+"/plates/plate-1/events",
		`{"stage":"warmup","operator":"op","logical_time":1}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("treatment status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = postJSON(t, h, "/api/trials/"+id+"/observations",
		`{"plate_id":"plate-1","counts":{"germinated":30,"hard":10},"operator":"op","logical_time":100}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("observation status = %d body=%s", rec.Code, rec.Body.String())
	}

	for _, r := range []string{"reviewer-1", "reviewer-2"} {
		rec = postJSON(t, h, "/api/trials/"+id+"/reviews",
			`{"reviewer_id":"`+r+`","qualification":"qualified","digest":"d-`+r+`"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("review status = %d body=%s", rec.Code, rec.Body.String())
		}
	}

	rec = postJSON(t, h, "/api/trials/"+id+"/terminal", `{"type":"release"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("terminal status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = getJSON(t, h, "/api/trials/"+id+"/credential")
	if rec.Code != http.StatusOK {
		t.Fatalf("credential status = %d", rec.Code)
	}
	var cred struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cred); err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	if cred.Type != "release" {
		t.Fatalf("credential type = %q, want release", cred.Type)
	}
}

// TestHTTPFrontendServed asserts the built frontend is served at the root.
func TestHTTPFrontendServed(t *testing.T) {
	h := newTestServer(t)
	rec := getJSON(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("frontend status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "种质库") {
		t.Fatalf("frontend index.html not served")
	}
}

// TestHTTPStableErrorCodes asserts a stale rule digest surfaces the stable code.
func TestHTTPStableErrorCodes(t *testing.T) {
	h := newTestServer(t)
	rec := postJSON(t, h, "/api/trials", `{"species":"Oryza err","idempotency_key":"k-err"}`)
	var trial struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &trial)

	rec = postJSON(t, h, "/api/trials/"+trial.ID+"/lock", `{"version":"v1","expected_digest":"stale"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Code != "STALE_RULE_DIGEST" {
		t.Fatalf("code = %q, want STALE_RULE_DIGEST", body.Code)
	}
}
