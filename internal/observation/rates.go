package observation

import (
	"seed-vault-viability-release/internal/domain"
)

// Metrics is the fixed-point viability metric set computed from a plate's
// classification counts at a given explicit scale. Every value uses
// round-half-away-from-zero integer arithmetic and checks for negative
// measures, division by zero and overflow before any transaction writes.
type Metrics struct {
	GerminationRate   domain.Fixed
	ContaminationRate domain.Fixed
	AbnormalRate      domain.Fixed
	VigorIndex        domain.Fixed
}

// ComputeMetrics derives the four viability metrics from a complete set of
// classification counts and the sown grain count. Vigor is the fraction of
// viable (non-decayed, non-abnormal) seeds that have germinated, so it is
// distinct from the raw germination rate (germinated / sown).
func ComputeMetrics(counts map[domain.ObservationClass]int64, sown int64, scale int) (Metrics, error) {
	c := ClassCounts(counts)
	germ := c[domain.ClassGerminated]
	hard := c[domain.ClassHard]
	decayed := c[domain.ClassDecayed]
	abnormal := c[domain.ClassAbnormal]

	germRate, err := domain.FixedPoint(germ, sown, scale)
	if err != nil {
		return Metrics{}, err
	}
	contRate, err := domain.FixedPoint(decayed, sown, scale)
	if err != nil {
		return Metrics{}, err
	}
	abnRate, err := domain.FixedPoint(abnormal, sown, scale)
	if err != nil {
		return Metrics{}, err
	}

	// Vigor: germinated / (germinated + hard). A plate with no viable seeds
	// has zero vigor rather than a division-by-zero error.
	denom := germ + hard
	var vigor domain.Fixed
	if denom == 0 {
		vigor = domain.Fixed{Raw: 0, Scale: scale}
	} else {
		vigor, err = domain.FixedPoint(germ, denom, scale)
		if err != nil {
			return Metrics{}, err
		}
	}

	return Metrics{
		GerminationRate:   germRate,
		ContaminationRate: contRate,
		AbnormalRate:      abnRate,
		VigorIndex:        vigor,
	}, nil
}
