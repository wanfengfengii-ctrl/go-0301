package service

import (
	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/observation"
)

// InstrumentCallInput is the command payload for creating an instrument call.
type InstrumentCallInput struct {
	ID         string
	Summary    string
	Generation domain.GenerationNumber
}

// CreateInstrumentCall registers a pending instrument call with a zero retry
// ordinal. It is the durable record that a device request is outstanding.
func (s *Service) CreateInstrumentCall(trialID string, in InstrumentCallInput) (domain.InstrumentCall, error) {
	var out domain.InstrumentCall
	err := s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		if in.Generation != t.Trial.CurrentGen {
			return nil, domain.New(domain.CodeGenerationMismatch,
				"call generation %d != trial generation %d", in.Generation, t.Trial.CurrentGen)
		}
		call := domain.InstrumentCall{
			ID:           in.ID,
			TrialID:      trialID,
			Generation:   in.Generation,
			Summary:      in.Summary,
			RetryOrdinal: 0,
			Status:       domain.InstrumentPending,
		}
		out = call
		return []event{{trialID: trialID, typ: evInstrumentCall,
			payload: instrumentCallPayload{Call: call}}}, nil
	})
	return out, err
}

// ReceiptInput is the command payload for a device receipt (success or a
// classified failure).
type ReceiptInput struct {
	CallID       string
	Summary      string
	Generation   domain.GenerationNumber
	RetryOrdinal int
	Success      bool
	FailureKind  string
	Payload      string
}

// SubmitReceipt applies a device receipt. A failure only increments the
// deterministic retry ordinal (no evidence is formed); a success must match the
// request summary, the current generation and the retry ordinal before it
// advances coverage. A stale or malformed receipt is rejected or kept as
// history.
func (s *Service) SubmitReceipt(trialID string, in ReceiptInput) (domain.InstrumentCall, error) {
	var out domain.InstrumentCall
	err := s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		call, ok := t.Calls[in.CallID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "instrument call %q not found", in.CallID)
		}
		if call.Status == domain.InstrumentSucceeded {
			out = call
			return nil, nil // already succeeded: idempotent
		}
		if call.Generation != t.Trial.CurrentGen {
			// A receipt for an old generation is kept as history and must not
			// affect the current generation's conclusion.
			return nil, domain.New(domain.CodeGenerationMismatch,
				"call %q belongs to generation %d, trial is at %d",
				in.CallID, call.Generation, t.Trial.CurrentGen)
		}
		if in.Generation != call.Generation {
			return nil, domain.New(domain.CodeGenerationMismatch,
				"receipt generation %d != call generation %d", in.Generation, call.Generation)
		}
		if in.Summary != call.Summary {
			return nil, domain.New(domain.CodeMalformedReceipt,
				"receipt summary %q != call summary %q", in.Summary, call.Summary)
		}
		if in.Success {
			if in.RetryOrdinal != call.RetryOrdinal {
				return nil, domain.New(domain.CodeMalformedReceipt,
					"receipt retry ordinal %d != call ordinal %d", in.RetryOrdinal, call.RetryOrdinal)
			}
			call.Status = domain.InstrumentSucceeded
			call.Payload = in.Payload
			out = call
			return []event{{trialID: trialID, typ: evInstrumentReceipt,
				payload: instrumentReceiptPayload{
					CallID: call.ID, Status: call.Status, Payload: in.Payload,
					RetryOrdinal: call.RetryOrdinal,
				}}}, nil
		}
		// Failure: only a deterministic retry ordinal, no evidence. A replay of
		// an already-recorded failure (its ordinal is below the current one,
		// e.g. a gateway re-delivering the timeout receipt) must not advance the
		// ordinal again or it would desync from the real retry, which carries
		// the next ordinal and would no longer match.
		if in.RetryOrdinal < call.RetryOrdinal {
			out = call
			return nil, nil // stale failure receipt: already absorbed as history
		}
		if in.RetryOrdinal != call.RetryOrdinal {
			return nil, domain.New(domain.CodeMalformedReceipt,
				"receipt retry ordinal %d != call ordinal %d", in.RetryOrdinal, call.RetryOrdinal)
		}
		call.Failure = observation.RecordFailure(in.FailureKind)
		call.RetryOrdinal = observation.NextRetryOrdinal(call)
		call.Status = domain.InstrumentFailed
		out = call
		return []event{{trialID: trialID, typ: evInstrumentReceipt,
			payload: instrumentReceiptPayload{
				CallID: call.ID, Status: call.Status, Failure: call.Failure,
				RetryOrdinal: call.RetryOrdinal,
			}}}, nil
	})
	return out, err
}
