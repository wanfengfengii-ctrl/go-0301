package service

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/observation"
)

// TestTreatmentStageGap verifies a skipped stage returns STAGE_GAP and leaves
// the plate's stage unchanged.
func TestTreatmentStageGap(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza stagegap")
	allocateFixture(t, svc, id)

	if err := svc.RecordTreatment(id, TreatmentInput{
		PlateID: "plate-1", Stage: domain.StageWarmup, Operator: "op", LogicalTime: 1,
	}); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	if err := svc.RecordTreatment(id, TreatmentInput{
		PlateID: "plate-1", Stage: domain.StageDormancyBreak, Operator: "op", LogicalTime: 2,
	}); err != nil {
		t.Fatalf("dormancy: %v", err)
	}
	// skipping sowing directly to incubation is a gap.
	if err := svc.RecordTreatment(id, TreatmentInput{
		PlateID: "plate-1", Stage: domain.StageIncubation, Operator: "op", LogicalTime: 3,
	}); !domain.IsCode(err, domain.CodeStageGap) {
		t.Fatalf("got %v, want STAGE_GAP", err)
	}
}

// TestTreatmentRequiresLease verifies device stages require a matching lease and
// a time regression is rejected.
func TestTreatmentRequiresLease(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza leaseproof")
	allocateFixture(t, svc, id)

	if err := svc.RecordTreatment(id, TreatmentInput{
		PlateID: "plate-1", Stage: domain.StageWarmup, Operator: "op", LogicalTime: 10,
	}); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	if err := svc.RecordTreatment(id, TreatmentInput{
		PlateID: "plate-1", Stage: domain.StageDormancyBreak, Operator: "op", LogicalTime: 11,
	}); err != nil {
		t.Fatalf("dormancy: %v", err)
	}
	// sowing is a device stage: a missing lease is rejected.
	if err := svc.RecordTreatment(id, TreatmentInput{
		PlateID: "plate-1", Stage: domain.StageSowing, Operator: "op", LeaseID: "missing", LogicalTime: 12,
	}); !domain.IsCode(err, domain.CodeLeaseExpired) {
		t.Fatalf("got %v, want LEASE_EXPIRED", err)
	}
	// time regression.
	if err := svc.RecordTreatment(id, TreatmentInput{
		PlateID: "plate-1", Stage: domain.StageSowing, Operator: "op", LeaseID: "missing", LogicalTime: 5,
	}); !domain.IsCode(err, domain.CodeTimeRegression) {
		t.Fatalf("got %v, want TIME_REGRESSION", err)
	}
}

// TestObservationRegressionAndMetrics verifies observation invariants and the
// fixed-point viability metric boundaries.
func TestObservationRegressionAndMetrics(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza obs")
	allocateFixture(t, svc, id)

	first := map[domain.ObservationClass]int64{domain.ClassGerminated: 30, domain.ClassHard: 10}
	if err := svc.RecordObservation(id, ObservationInput{
		PlateID: "plate-1", Counts: first, Operator: "op", LogicalTime: 100,
	}); err != nil {
		t.Fatalf("first observation: %v", err)
	}
	// regression below a previous count.
	if err := svc.RecordObservation(id, ObservationInput{
		PlateID: "plate-1", Counts: map[domain.ObservationClass]int64{domain.ClassGerminated: 10},
		Operator: "op", LogicalTime: 200,
	}); !domain.IsCode(err, domain.CodeObservationRegression) {
		t.Fatalf("got %v, want OBSERVATION_REGRESSION", err)
	}
	// total exceeding sown (60) is rejected.
	if err := svc.RecordObservation(id, ObservationInput{
		PlateID: "plate-1", Counts: map[domain.ObservationClass]int64{domain.ClassGerminated: 70},
		Operator: "op", LogicalTime: 300,
	}); !domain.IsCode(err, domain.CodeObservationRegression) {
		t.Fatalf("got %v, want OBSERVATION_REGRESSION for over-sown", err)
	}

	// metrics rounding: 30/60 = 50.00%.
	m, err := svc.PlateMetrics(id, "plate-1")
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if m.GerminationRate.Raw != 5000 {
		t.Fatalf("germination rate = %d, want 5000", m.GerminationRate.Raw)
	}
	if m.VigorIndex.Raw != 7500 { // 30/(30+10) = 75.00%
		t.Fatalf("vigor index = %d, want 7500", m.VigorIndex.Raw)
	}
}

// TestMetricsDivideByZero verifies a plate with no viable seeds yields zero
// vigor rather than a division error.
func TestMetricsDivideByZero(t *testing.T) {
	m, err := observation.ComputeMetrics(map[domain.ObservationClass]int64{
		domain.ClassDecayed: 5,
	}, 5, 2)
	if err != nil {
		t.Fatalf("compute metrics: %v", err)
	}
	if m.VigorIndex.Raw != 0 {
		t.Fatalf("vigor = %d, want 0", m.VigorIndex.Raw)
	}
	if m.ContaminationRate.Raw != 100 {
		t.Fatalf("contamination = %d, want 100", m.ContaminationRate.Raw)
	}
}

// TestRecordEnvironment verifies environment readings require a declared
// dimension and a monotonic logical time.
func TestRecordEnvironment(t *testing.T) {
	svc := newTestService(t)
	id := createLockedTrial(t, svc, "Oryza env")

	if err := svc.RecordEnvironment(id, EnvironmentInput{
		Dimension: "temperature", Value: 25, LogicalTime: 5,
	}); err != nil {
		t.Fatalf("record environment: %v", err)
	}
	// an undeclared dimension is rejected.
	if err := svc.RecordEnvironment(id, EnvironmentInput{
		Dimension: "co2", Value: 1, LogicalTime: 6,
	}); !domain.IsCode(err, domain.CodeInvalidSchedule) {
		t.Fatalf("got %v, want INVALID_SCHEDULE", err)
	}
	// a time regression is rejected.
	if err := svc.RecordEnvironment(id, EnvironmentInput{
		Dimension: "humidity", Value: 50, LogicalTime: 3,
	}); !domain.IsCode(err, domain.CodeTimeRegression) {
		t.Fatalf("got %v, want TIME_REGRESSION", err)
	}
}
