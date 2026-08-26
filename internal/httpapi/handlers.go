package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/lineage"
	"seed-vault-viability-release/internal/service"
)

// createTrialRequest is the trial-creation command payload.
type createTrialRequest struct {
	Species        string `json:"species"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) handleCreateTrial(w http.ResponseWriter, r *http.Request) {
	var req createTrialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	trial, err := s.svc.CreateTrial(service.CreateTrialInput{
		Species: req.Species, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, trial)
}

func (s *Server) handleGetTrial(w http.ResponseWriter, r *http.Request) {
	trial, err := s.svc.GetTrial(r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, trial)
}

type lockTrialRequest struct {
	Version        string `json:"version"`
	ExpectedDigest string `json:"expected_digest"`
}

func (s *Server) handleLockTrial(w http.ResponseWriter, r *http.Request) {
	var req lockTrialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	trial, err := s.svc.LockTrial(r.PathValue("id"), service.LockTrialInput{
		Version: req.Version, ExpectedDigest: req.ExpectedDigest,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, trial)
}

type allocateRequest struct {
	SampleID   string                `json:"sample_id"`
	Allocation lineage.Allocation    `json:"allocation"`
	SeedLots   []domain.SeedLot      `json:"seed_lots"`
	Samples    []domain.SampleUnit   `json:"samples"`
	Groups     []domain.CultureGroup `json:"groups"`
	Plates     []domain.Plate        `json:"plates"`
}

func (s *Server) handleAllocate(w http.ResponseWriter, r *http.Request) {
	var req allocateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	if err := s.svc.AllocateSamples(r.PathValue("id"), service.AllocateInput{
		SampleID: req.SampleID, Allocation: req.Allocation,
		SeedLots: req.SeedLots, Samples: req.Samples, Groups: req.Groups, Plates: req.Plates,
	}); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "allocated"})
}

func (s *Server) handleLineage(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.Lineage(r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type leaseAcquireRequest struct {
	TrialID    string                  `json:"trial_id"`
	ID         string                  `json:"id"`
	Resource   string                  `json:"resource"`
	Kind       domain.ResourceKind     `json:"kind"`
	Holder     string                  `json:"holder"`
	Purpose    string                  `json:"purpose"`
	Generation domain.GenerationNumber `json:"generation"`
	Now        int64                   `json:"now"`
	Duration   int64                   `json:"duration"`
}

func (s *Server) handleLeaseAcquire(w http.ResponseWriter, r *http.Request) {
	var req leaseAcquireRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	lease, err := s.svc.AcquireLease(req.TrialID, service.LeaseAcquireInput{
		ID: req.ID, Resource: req.Resource, Kind: req.Kind, Holder: req.Holder,
		Purpose: req.Purpose, Generation: req.Generation, Now: req.Now, Duration: req.Duration,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, lease)
}

type leaseRenewRequest struct {
	TrialID  string `json:"trial_id"`
	ID       string `json:"id"`
	Holder   string `json:"holder"`
	Now      int64  `json:"now"`
	Duration int64  `json:"duration"`
}

func (s *Server) handleLeaseRenew(w http.ResponseWriter, r *http.Request) {
	var req leaseRenewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	lease, err := s.svc.RenewLease(req.TrialID, req.ID, req.Holder, req.Now, req.Duration)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lease)
}

type leaseReleaseRequest struct {
	TrialID string `json:"trial_id"`
	ID      string `json:"id"`
	Holder  string `json:"holder"`
}

func (s *Server) handleLeaseRelease(w http.ResponseWriter, r *http.Request) {
	var req leaseReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	if err := s.svc.ReleaseLease(req.TrialID, req.ID, req.Holder); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

type treatmentRequest struct {
	Stage       domain.Stage `json:"stage"`
	Operator    string       `json:"operator"`
	Evidence    string       `json:"evidence"`
	LeaseID     string       `json:"lease_id"`
	LogicalTime int64        `json:"logical_time"`
}

func (s *Server) handleTreatment(w http.ResponseWriter, r *http.Request) {
	var req treatmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	err := s.svc.RecordTreatment(r.PathValue("id"), service.TreatmentInput{
		PlateID: r.PathValue("plateId"), Stage: req.Stage, Operator: req.Operator,
		Evidence: req.Evidence, LeaseID: req.LeaseID, LogicalTime: req.LogicalTime,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
}

type observationRequest struct {
	PlateID     string                            `json:"plate_id"`
	Counts      map[domain.ObservationClass]int64 `json:"counts"`
	Operator    string                            `json:"operator"`
	LogicalTime int64                             `json:"logical_time"`
}

func (s *Server) handleObservation(w http.ResponseWriter, r *http.Request) {
	var req observationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	err := s.svc.RecordObservation(r.PathValue("id"), service.ObservationInput{
		PlateID: req.PlateID, Counts: req.Counts, Operator: req.Operator, LogicalTime: req.LogicalTime,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
}

type instrumentCallRequest struct {
	TrialID    string                  `json:"trial_id"`
	ID         string                  `json:"id"`
	Summary    string                  `json:"summary"`
	Generation domain.GenerationNumber `json:"generation"`
}

func (s *Server) handleInstrumentCall(w http.ResponseWriter, r *http.Request) {
	var req instrumentCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	call, err := s.svc.CreateInstrumentCall(req.TrialID, service.InstrumentCallInput{
		ID: req.ID, Summary: req.Summary, Generation: req.Generation,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, call)
}

type receiptRequest struct {
	TrialID      string                  `json:"trial_id"`
	Summary      string                  `json:"summary"`
	Generation   domain.GenerationNumber `json:"generation"`
	RetryOrdinal int                     `json:"retry_ordinal"`
	Success      bool                    `json:"success"`
	FailureKind  string                  `json:"failure_kind"`
	Payload      string                  `json:"payload"`
}

func (s *Server) handleReceipt(w http.ResponseWriter, r *http.Request) {
	var req receiptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	call, err := s.svc.SubmitReceipt(req.TrialID, service.ReceiptInput{
		CallID: r.PathValue("id"), Summary: req.Summary, Generation: req.Generation,
		RetryOrdinal: req.RetryOrdinal, Success: req.Success,
		FailureKind: req.FailureKind, Payload: req.Payload,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, call)
}

type retestRequest struct {
	Reason  string                `json:"reason"`
	Members []domain.RetestMember `json:"members"`
}

func (s *Server) handleGenerateRetest(w http.ResponseWriter, r *http.Request) {
	var req retestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	rs, err := s.svc.GenerateRetest(r.PathValue("id"), service.RetestInput{
		Reason: req.Reason, Members: req.Members,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rs)
}

func (s *Server) handleGetRetest(w http.ResponseWriter, r *http.Request) {
	gen, err := strconv.Atoi(r.PathValue("generation"))
	if err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid generation")
		return
	}
	rs, err := s.svc.GetRetest(r.PathValue("id"), domain.GenerationNumber(gen), "")
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

type reviewRequest struct {
	ReviewerID    string `json:"reviewer_id"`
	Qualification string `json:"qualification"`
	Digest        string `json:"digest"`
}

func (s *Server) handleSubmitReview(w http.ResponseWriter, r *http.Request) {
	var req reviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	if err := s.svc.SubmitReview(r.PathValue("id"), service.ReviewInput{
		ReviewerID: req.ReviewerID, Qualification: req.Qualification, Digest: req.Digest,
	}); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "reviewed"})
}

type terminalRequest struct {
	Type domain.TerminalType `json:"type"`
}

func (s *Server) handleDecideTerminal(w http.ResponseWriter, r *http.Request) {
	var req terminalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	cred, err := s.svc.DecideTerminal(r.PathValue("id"), service.TerminalInput{Type: req.Type})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, cred)
}

func (s *Server) handleGetCredential(w http.ResponseWriter, r *http.Request) {
	cred, err := s.svc.GetCredential(r.PathValue("id"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cred)
}

type environmentRequest struct {
	Dimension   string `json:"dimension"`
	Value       int64  `json:"value"`
	LogicalTime int64  `json:"logical_time"`
}

func (s *Server) handleEnvironment(w http.ResponseWriter, r *http.Request) {
	var req environmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, domain.CodeInvalidSampleCount, "invalid request body")
		return
	}
	if err := s.svc.RecordEnvironment(r.PathValue("id"), service.EnvironmentInput{
		Dimension: req.Dimension, Value: req.Value, LogicalTime: req.LogicalTime,
	}); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
}
