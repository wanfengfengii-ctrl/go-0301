package service

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
)

func TestModel_InstrumentReceiptRetryOrdinal(t *testing.T) {
	failure := func(kind string, ordinal int) ReceiptInput {
		return ReceiptInput{
			CallID: "call-1", Summary: "scan-1", Generation: 1,
			RetryOrdinal: ordinal, FailureKind: kind,
		}
	}
	success := func(ordinal int) ReceiptInput {
		return ReceiptInput{
			CallID: "call-1", Summary: "scan-1", Generation: 1,
			RetryOrdinal: ordinal, Success: true, Payload: "image-ok",
		}
	}

	tests := []struct {
		name              string
		setup             []ReceiptInput
		receipt           ReceiptInput
		wantErr           domain.ErrorCode
		wantReceiptEvents int
		wantOrdinal       int
		wantStatus        domain.InstrumentStatus
		wantFailure       domain.ErrorCode
		followup          *ReceiptInput
		followupOrdinal   int
		followupStatus    domain.InstrumentStatus
	}{
		{name: "rejected advances", receipt: failure("rejected", 0), wantReceiptEvents: 1, wantOrdinal: 1, wantStatus: domain.InstrumentFailed, wantFailure: domain.CodeInstrumentRejected},
		{name: "disconnected advances", receipt: failure("disconnected", 0), wantReceiptEvents: 1, wantOrdinal: 1, wantStatus: domain.InstrumentFailed, wantFailure: domain.CodeInstrumentDisconnected},
		{name: "timeout advances", receipt: failure("timeout", 0), wantReceiptEvents: 1, wantOrdinal: 1, wantStatus: domain.InstrumentFailed, wantFailure: domain.CodeInstrumentTimeout},
		{name: "malformed advances", receipt: failure("malformed", 0), wantReceiptEvents: 1, wantOrdinal: 1, wantStatus: domain.InstrumentFailed, wantFailure: domain.CodeMalformedReceipt},
		{name: "duplicate timeout is absorbed", setup: []ReceiptInput{failure("timeout", 0)}, receipt: failure("timeout", 0), wantOrdinal: 1, wantStatus: domain.InstrumentFailed, wantFailure: domain.CodeInstrumentTimeout},
		{name: "older failure is absorbed", setup: []ReceiptInput{failure("rejected", 0), failure("disconnected", 1)}, receipt: failure("rejected", 0), wantOrdinal: 2, wantStatus: domain.InstrumentFailed, wantFailure: domain.CodeInstrumentDisconnected},
		{name: "future failure is rejected", receipt: failure("timeout", 1), wantErr: domain.CodeMalformedReceipt, followup: func() *ReceiptInput { in := failure("timeout", 0); return &in }(), followupOrdinal: 1, followupStatus: domain.InstrumentFailed},
		{name: "stale success is rejected", setup: []ReceiptInput{failure("timeout", 0)}, receipt: success(0), wantErr: domain.CodeMalformedReceipt, followup: func() *ReceiptInput { in := success(1); return &in }(), followupOrdinal: 1, followupStatus: domain.InstrumentSucceeded},
		{name: "current success is accepted", setup: []ReceiptInput{failure("timeout", 0)}, receipt: success(1), wantReceiptEvents: 1, wantOrdinal: 1, wantStatus: domain.InstrumentSucceeded, wantFailure: domain.CodeInstrumentTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t)
			trialID := createLockedTrial(t, svc, "Oryza "+tt.name)
			if _, err := svc.CreateInstrumentCall(trialID, InstrumentCallInput{
				ID: "call-1", Summary: "scan-1", Generation: 1,
			}); err != nil {
				t.Fatalf("create call: %v", err)
			}
			for i, in := range tt.setup {
				if _, err := svc.SubmitReceipt(trialID, in); err != nil {
					t.Fatalf("setup receipt %d: %v", i, err)
				}
			}

			before, err := svc.store.LastSeq()
			if err != nil {
				t.Fatalf("last sequence before receipt: %v", err)
			}
			got, err := svc.SubmitReceipt(trialID, tt.receipt)
			if tt.wantErr != "" {
				if !domain.IsCode(err, tt.wantErr) {
					t.Fatalf("receipt error = %v, want %s", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("submit receipt: %v", err)
			}

			events, err := svc.store.EventsSince(before)
			if err != nil {
				t.Fatalf("events after receipt: %v", err)
			}
			receiptEvents := 0
			for _, event := range events {
				if event.Type == evInstrumentReceipt {
					receiptEvents++
				}
			}
			if receiptEvents != tt.wantReceiptEvents {
				t.Fatalf("new instrument.receipt events = %d, want %d", receiptEvents, tt.wantReceiptEvents)
			}

			if tt.wantErr == "" {
				if got.RetryOrdinal != tt.wantOrdinal || got.Status != tt.wantStatus || got.Failure != tt.wantFailure {
					t.Fatalf("call = {ordinal:%d status:%q failure:%q}, want {ordinal:%d status:%q failure:%q}",
						got.RetryOrdinal, got.Status, got.Failure, tt.wantOrdinal, tt.wantStatus, tt.wantFailure)
				}
			}
			if tt.followup != nil {
				followed, err := svc.SubmitReceipt(trialID, *tt.followup)
				if err != nil {
					t.Fatalf("matching follow-up after rejected receipt: %v", err)
				}
				if followed.RetryOrdinal != tt.followupOrdinal || followed.Status != tt.followupStatus {
					t.Fatalf("follow-up call = {ordinal:%d status:%q}, want {ordinal:%d status:%q}",
						followed.RetryOrdinal, followed.Status, tt.followupOrdinal, tt.followupStatus)
				}
			}
		})
	}
}
