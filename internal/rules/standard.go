package rules

import (
	"time"

	"seed-vault-viability-release/internal/domain"
)

// StandardCatalog is the concrete rules catalogue used by the service. It holds
// a set of immutable rule snapshots keyed by version, and validates any
// candidate snapshot against the structural invariants before a trial locks.
type StandardCatalog struct {
	versions map[string]domain.RuleSnapshot
}

// NewStandardCatalog returns a catalogue seeded with a single default version
// ("v1") describing a representative seed-viability rule set.
func NewStandardCatalog() *StandardCatalog {
	c := &StandardCatalog{versions: make(map[string]domain.RuleSnapshot)}
	c.Register("v1", domain.RuleSnapshot{
		Version:         "v1",
		Species:         "Oryza sativa",
		FixedPointScale: 4,
		Stages: []domain.Stage{
			domain.StageWarmup,
			domain.StageDormancyBreak,
			domain.StageSowing,
			domain.StageIncubation,
			domain.StageObservation,
			domain.StageClosed,
		},
		EnvironmentRanges: []domain.EnvironmentRange{
			{Dimension: "temperature", Min: 20, Max: 30},
			{Dimension: "humidity", Min: 40, Max: 80},
		},
		Schedule: domain.ObservationSchedule{
			Intervals: []time.Duration{24 * time.Hour, 48 * time.Hour, 72 * time.Hour},
			Window:    time.Hour,
		},
		Thresholds: domain.Thresholds{
			MinGermination:   8000, // 80.00%
			MaxContamination: 1000, // 10.00%
			MinVigor:         6000, // 60.00%
		},
		Qualification: domain.QualificationRules{
			MinDistinctReviewers: 2,
			RequiredRoles:        []string{"qualified"},
		},
	})
	return c
}

// Register stores (or replaces) a rule snapshot under a version. The slice
// fields are deep-copied so the stored snapshot is isolated from any later
// mutation of the caller's slices; otherwise a config builder that reuses the
// stage and environment slices across versions would pollute earlier
// registrations and invalidate the digest computed at registration time.
func (c *StandardCatalog) Register(version string, snap domain.RuleSnapshot) {
	snap.Version = version
	snap.Stages = append([]domain.Stage(nil), snap.Stages...)
	snap.EnvironmentRanges = append([]domain.EnvironmentRange(nil), snap.EnvironmentRanges...)
	snap.Schedule.Intervals = append([]time.Duration(nil), snap.Schedule.Intervals...)
	snap.Qualification.RequiredRoles = append([]string(nil), snap.Qualification.RequiredRoles...)
	c.versions[version] = snap
}

// Snapshot returns the rule snapshot for the given version.
func (c *StandardCatalog) Snapshot(version string) (domain.RuleSnapshot, error) {
	snap, ok := c.versions[version]
	if !ok {
		return domain.RuleSnapshot{}, domain.New(domain.CodeStaleRuleDigest,
			"unknown rule version %q", version)
	}
	return snap, nil
}

// Validate checks a candidate snapshot for structural validity.
func (c *StandardCatalog) Validate(snap domain.RuleSnapshot) error {
	return Validate(snap)
}
