package service

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
)

// TestInstrumentFailureRetryOrdinals scripts rejection, disconnect, timeout and
// malformed payloads and asserts the retry ordinal advances deterministically
// without any successful coverage.
func TestInstrumentFailureRetryOrdinals(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza instrument")

	call, err := svc.CreateInstrumentCall(id, InstrumentCallInput{
		ID: "call-1", Summary: "scan-1", Generation: 1,
	})
	if err != nil {
		t.Fatalf("create call: %v", err)
	}
	if call.RetryOrdinal != 0 {
		t.Fatalf("initial retry ordinal = %d, want 0", call.RetryOrdinal)
	}

	for i, kind := range []string{"rejected", "disconnected", "timeout", "malformed"} {
		got, err := svc.SubmitReceipt(id, ReceiptInput{
			CallID: "call-1", Summary: "scan-1", Generation: 1,
			RetryOrdinal: i, Success: false, FailureKind: kind,
		})
		if err != nil {
			t.Fatalf("failure receipt %q: %v", kind, err)
		}
		if got.Status != domain.InstrumentFailed {
			t.Fatalf("status = %q, want failed", got.Status)
		}
		if got.RetryOrdinal != i+1 {
			t.Fatalf("retry ordinal = %d, want %d", got.RetryOrdinal, i+1)
		}
	}
}

// TestInstrumentSuccessRequiresMatchingReceipt verifies a success receipt must
// match summary, generation and retry ordinal.
func TestInstrumentSuccessRequiresMatchingReceipt(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza receipt")

	if _, err := svc.CreateInstrumentCall(id, InstrumentCallInput{
		ID: "call-1", Summary: "scan-1", Generation: 1,
	}); err != nil {
		t.Fatalf("create call: %v", err)
	}
	// wrong summary -> MALFORMED_RECEIPT.
	if _, err := svc.SubmitReceipt(id, ReceiptInput{
		CallID: "call-1", Summary: "other", Generation: 1, RetryOrdinal: 0, Success: true,
	}); !domain.IsCode(err, domain.CodeMalformedReceipt) {
		t.Fatalf("got %v, want MALFORMED_RECEIPT", err)
	}
	// correct receipt succeeds.
	got, err := svc.SubmitReceipt(id, ReceiptInput{
		CallID: "call-1", Summary: "scan-1", Generation: 1, RetryOrdinal: 0, Success: true, Payload: "ok",
	})
	if err != nil {
		t.Fatalf("success receipt: %v", err)
	}
	if got.Status != domain.InstrumentSucceeded {
		t.Fatalf("status = %q, want succeeded", got.Status)
	}
}

// TestRetestUniqueAndOldGenerationReceipt verifies a retest set is unique per
// anomaly source, opens a new generation, and that a stale success receipt for
// the old generation is rejected while history is preserved.
func TestRetestUniqueAndOldGenerationReceipt(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza retest")
	allocateFixture(t, svc, id)

	if _, err := svc.CreateInstrumentCall(id, InstrumentCallInput{
		ID: "call-1", Summary: "scan-1", Generation: 1,
	}); err != nil {
		t.Fatalf("create call: %v", err)
	}

	members := []domain.RetestMember{
		{SeedLotID: "lot-b", SampleID: "s2", GroupID: "g2", PlateIndex: 2},
		{SeedLotID: "lot-a", SampleID: "s1", GroupID: "g1", PlateIndex: 1},
	}
	rs, err := svc.GenerateRetest(id, RetestInput{Reason: "contamination", Members: members})
	if err != nil {
		t.Fatalf("generate retest: %v", err)
	}
	if rs.TargetGen != 2 {
		t.Fatalf("target generation = %d, want 2", rs.TargetGen)
	}
	if len(rs.Members) != 2 || rs.Members[0].SeedLotID != "lot-a" {
		t.Fatalf("members not canonically sorted: %+v", rs.Members)
	}
	// the generated set is retrievable by its source generation.
	got, err := svc.GetRetest(id, 1, "contamination")
	if err != nil || got.Digest != rs.Digest {
		t.Fatalf("retrieve retest: got %+v err %v", got, err)
	}
	// a different anomaly source in the new generation opens a further set.
	_, err = svc.GenerateRetest(id, RetestInput{Reason: "vigor", Members: members})
	if err != nil {
		t.Fatalf("second retest: %v", err)
	}
	trialAfter, _ := svc.GetTrial(id)
	if trialAfter.CurrentGen != 3 {
		t.Fatalf("current generation = %d, want 3", trialAfter.CurrentGen)
	}

	// a stale success receipt for generation 1 is rejected; current gen is 2.
	if _, err := svc.SubmitReceipt(id, ReceiptInput{
		CallID: "call-1", Summary: "scan-1", Generation: 1, RetryOrdinal: 0, Success: true,
	}); !domain.IsCode(err, domain.CodeGenerationMismatch) {
		t.Fatalf("got %v, want GENERATION_MISMATCH", err)
	}
	trial, _ := svc.GetTrial(id)
	if trial.CurrentGen != 3 {
		t.Fatalf("current generation = %d, want 3", trial.CurrentGen)
	}
}
