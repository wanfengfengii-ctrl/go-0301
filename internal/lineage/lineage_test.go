package lineage_test

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/lineage"
)

func TestValidateAllocationConservation(t *testing.T) {
	a := lineage.Allocation{Source: 100, Culture: 60, Retain: 20, Measurement: 10, Quarantine: 5, Loss: 5}
	if err := lineage.ValidateAllocation(a); err != nil {
		t.Fatalf("valid allocation rejected: %v", err)
	}
}

func TestValidateAllocationMismatch(t *testing.T) {
	a := lineage.Allocation{Source: 100, Culture: 60, Retain: 20, Measurement: 10, Quarantine: 5, Loss: 4}
	if err := lineage.ValidateAllocation(a); !domain.IsCode(err, domain.CodeInvalidSampleCount) {
		t.Fatalf("got %v, want INVALID_SAMPLE_COUNT", err)
	}
}

func TestValidateAllocationNegative(t *testing.T) {
	a := lineage.Allocation{Source: 100, Culture: 110, Retain: -10}
	if err := lineage.ValidateAllocation(a); !domain.IsCode(err, domain.CodeInvalidSampleCount) {
		t.Fatalf("got %v, want INVALID_SAMPLE_COUNT", err)
	}
}

func TestLineageCycleRejected(t *testing.T) {
	g := lineage.NewGraph()
	if err := g.AddEdge("lot-a", "sample-1"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := g.AddEdge("sample-1", "plate-1"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := g.AddEdge("plate-1", "lot-a"); !domain.IsCode(err, domain.CodeLineageCycle) {
		t.Fatalf("got %v, want LINEAGE_CYCLE", err)
	}
	// no residual edge should remain after the failed add
	if got := g.NodeCount(); got != 3 {
		t.Fatalf("node count = %d, want 3", got)
	}
}

func TestLineageMultipleParentRejected(t *testing.T) {
	g := lineage.NewGraph()
	if err := g.AddEdge("lot-a", "sample-1"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := g.AddEdge("lot-b", "sample-1"); !domain.IsCode(err, domain.CodeMultipleParent) {
		t.Fatalf("got %v, want MULTIPLE_PARENT", err)
	}
	if g.Parents()["sample-1"] != "lot-a" {
		t.Fatalf("parent overwritten to %q", g.Parents()["sample-1"])
	}
}
