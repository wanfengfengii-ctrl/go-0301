// Package httpapi is the HTTP and frontend layer. It exposes a stable JSON API
// over the application service, enforces idempotency and deterministic error
// codes, and serves the built frontend from an embedded filesystem. Handlers
// return canonically sorted responses so output is stable across restarts.
package httpapi

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/service"
)

//go:embed all:dist
var distFS embed.FS

// Server hosts the stable JSON API and the built frontend.
type Server struct {
	svc *service.Service
}

// NewServer returns a Server wired to the application service.
func NewServer(svc *service.Service) *Server {
	return &Server{svc: svc}
}

// Handler returns the HTTP handler for the service.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Trial lifecycle.
	mux.HandleFunc("POST /api/trials", s.handleCreateTrial)
	mux.HandleFunc("GET /api/trials/{id}", s.handleGetTrial)
	mux.HandleFunc("POST /api/trials/{id}/lock", s.handleLockTrial)

	// Allocation and lineage.
	mux.HandleFunc("POST /api/trials/{id}/samples/allocate", s.handleAllocate)
	mux.HandleFunc("GET /api/trials/{id}/lineage", s.handleLineage)

	// Leases.
	mux.HandleFunc("POST /api/leases/acquire", s.handleLeaseAcquire)
	mux.HandleFunc("POST /api/leases/renew", s.handleLeaseRenew)
	mux.HandleFunc("POST /api/leases/release", s.handleLeaseRelease)

	// Treatment and observation.
	mux.HandleFunc("POST /api/trials/{id}/plates/{plateId}/events", s.handleTreatment)
	mux.HandleFunc("POST /api/trials/{id}/observations", s.handleObservation)
	mux.HandleFunc("POST /api/trials/{id}/environment", s.handleEnvironment)

	// Instrument calls.
	mux.HandleFunc("POST /api/instrument-calls", s.handleInstrumentCall)
	mux.HandleFunc("POST /api/instrument-calls/{id}/receipts", s.handleReceipt)

	// Retest.
	mux.HandleFunc("POST /api/trials/{id}/retests", s.handleGenerateRetest)
	mux.HandleFunc("GET /api/trials/{id}/retests/{generation}", s.handleGetRetest)

	// Review and terminal.
	mux.HandleFunc("POST /api/trials/{id}/reviews", s.handleSubmitReview)
	mux.HandleFunc("POST /api/trials/{id}/terminal", s.handleDecideTerminal)
	mux.HandleFunc("GET /api/trials/{id}/credential", s.handleGetCredential)

	// Status.
	mux.HandleFunc("GET /api/status", s.handleStatus)

	// Frontend static files.
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	return mux
}

// Status is the live backend state surfaced to the frontend.
type Status struct {
	Service string                  `json:"service"`
	Trials  []domain.ViabilityTrial `json:"trials"`
	Now     int64                   `json:"now"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Status{
		Service: "seed-vault-viability-release",
		Trials:  s.svc.TrialSummaries(),
		Now:     time.Now().UnixMilli(),
	})
}

// writeJSON writes a JSON response body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorBody is the stable error envelope returned to clients.
type errorBody struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

// writeDomainError writes a domain error with the appropriate HTTP status.
func writeDomainError(w http.ResponseWriter, err error) {
	de, ok := err.(*domain.Error)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorBody{Code: "INTERNAL", Message: err.Error()})
		return
	}
	writeJSON(w, statusFor(de.Code), errorBody{
		Code:    string(de.Code),
		Message: de.Message,
		Details: de.Details,
	})
}

// statusFor maps a stable error code to an HTTP status.
func statusFor(c domain.ErrorCode) int {
	switch c {
	case domain.CodeLeaseConflict, domain.CodeLeaseExpired, domain.CodeStageGap,
		domain.CodeGenerationMismatch, domain.CodeTimeRegression,
		domain.CodeObservationRegression, domain.CodeTerminalAlreadyDecided,
		domain.CodeSampleAlreadyAllocated, domain.CodeIdempotencyConflict,
		domain.CodeInstrumentRejected, domain.CodeInstrumentDisconnected,
		domain.CodeInstrumentTimeout, domain.CodeMalformedReceipt:
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func writeError(w http.ResponseWriter, status int, code domain.ErrorCode, format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	writeJSON(w, status, errorBody{Code: string(code), Message: msg})
}
