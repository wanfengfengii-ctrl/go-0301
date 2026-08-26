package rules_test

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/rules"
)

func TestDigestDeterministicAcrossStageOrder(t *testing.T) {
	base := domain.RuleSnapshot{
		Version:         "v1",
		Species:         "Oryza sativa",
		FixedPointScale: 2,
		Stages: []domain.Stage{
			domain.StageWarmup, domain.StageSowing, domain.StageIncubation,
		},
	}
	reordered := base
	reordered.Stages = []domain.Stage{
		domain.StageIncubation, domain.StageWarmup, domain.StageSowing,
	}
	d1, err := rules.Digest(base)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	d2, err := rules.Digest(reordered)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("digest differs for stage ordering: %s vs %s", d1, d2)
	}
}

func TestValidateEmptyStages(t *testing.T) {
	snap := domain.RuleSnapshot{Version: "v1", FixedPointScale: 2}
	if err := rules.Validate(snap); !domain.IsCode(err, domain.CodeInvalidSchedule) {
		t.Fatalf("got %v, want INVALID_SCHEDULE", err)
	}
}

func TestValidateEnvironmentRangeInverted(t *testing.T) {
	snap := domain.RuleSnapshot{
		Version: "v1",
		Stages:  []domain.Stage{domain.StageWarmup},
		EnvironmentRanges: []domain.EnvironmentRange{
			{Dimension: "temperature", Min: 30, Max: 20},
		},
	}
	if err := rules.Validate(snap); !domain.IsCode(err, domain.CodeInvalidSchedule) {
		t.Fatalf("got %v, want INVALID_SCHEDULE", err)
	}
}
