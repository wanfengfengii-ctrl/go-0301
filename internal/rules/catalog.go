// Package rules implements the trial rule catalogue: the species rules,
// pretreatment schemes, environment ranges, observation classifications,
// sampling ratios, viability thresholds and reviewer qualifications together
// with their version digest. Locking a trial freezes the catalogue into an
// immutable RuleSnapshot.
package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"seed-vault-viability-release/internal/domain"
)

// Catalog is the read-side view of the rules catalogue. It resolves a version
// into a stable, ordered snapshot and computes the digest used to detect
// stale submissions.
type Catalog interface {
	// Snapshot returns the rule snapshot for the given version.
	Snapshot(version string) (domain.RuleSnapshot, error)
	// Validate checks a candidate snapshot for structural validity before
	// locking. It returns a *domain.Error on the first violation.
	Validate(snap domain.RuleSnapshot) error
}

// Digest computes the deterministic digest of a rule snapshot. The snapshot's
// stage list is sorted into canonical order and the whole structure is JSON
// marshalled so the digest is stable across map iteration order, architecture
// and restart.
func Digest(snap domain.RuleSnapshot) (string, error) {
	cp := snap
	cp.Stages = append([]domain.Stage(nil), snap.Stages...)
	sort.SliceStable(cp.Stages, func(i, j int) bool {
		ii, _ := domain.StageIndex(cp.Stages[i])
		jj, _ := domain.StageIndex(cp.Stages[j])
		return ii < jj
	})
	cp.Digest = ""
	b, err := json.Marshal(cp)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Validate enforces the structural invariants of a rule snapshot: at least one
// stage, a non-negative fixed-point scale and a non-negative observation
// window.
func Validate(snap domain.RuleSnapshot) error {
	if len(snap.Stages) == 0 {
		return domain.New(domain.CodeInvalidSchedule, "rule snapshot has no stages")
	}
	if snap.FixedPointScale < 0 {
		return domain.New(domain.CodeInvalidFixedPointScale, "fixed-point scale must be non-negative")
	}
	if snap.Schedule.Window < 0 {
		return domain.New(domain.CodeInvalidSchedule, "observation window must be non-negative")
	}
	for _, r := range snap.EnvironmentRanges {
		if r.Min > r.Max {
			return domain.New(domain.CodeInvalidSchedule, "environment range %q has min > max", r.Dimension)
		}
	}
	return nil
}
