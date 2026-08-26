// Package observation implements the observation evidence and retest engine:
// plate classification counts, environment evidence, instrument calls with
// deterministic retry ordinals, and the normalized, ordered retest sets that
// follow from an anomaly.
package observation

import (
	"sort"

	"seed-vault-viability-release/internal/domain"
)

// ValidateCounts enforces the observation invariants for a plate: every
// classification count is non-negative, cumulative counts never decrease
// relative to the previous observation, and the total never exceeds the sown
// count. At close the caller additionally asserts total == sown.
func ValidateCounts(prev, next map[domain.ObservationClass]int64, sown int64) error {
	var total int64
	for cls, n := range next {
		if n < 0 {
			return domain.New(domain.CodeNegativeMeasure, "classification %q is negative", cls)
		}
		if p, ok := prev[cls]; ok && n < p {
			return domain.New(domain.CodeObservationRegression,
				"classification %q regressed from %d to %d", cls, p, n)
		}
		total += n
	}
	if total > sown {
		return domain.New(domain.CodeObservationRegression,
			"classification total %d exceeds sown count %d", total, sown)
	}
	return nil
}

// ClassCounts returns the complete classification map with all classes present
// (zero-filled), so encoding is deterministic regardless of insertion order.
func ClassCounts(m map[domain.ObservationClass]int64) map[domain.ObservationClass]int64 {
	out := map[domain.ObservationClass]int64{
		domain.ClassGerminated:   0,
		domain.ClassHard:         0,
		domain.ClassDecayed:      0,
		domain.ClassAbnormal:     0,
		domain.ClassUngerminated: 0,
	}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// SortRetestMembers orders retest members canonically by seed lot, sample,
// group and plate index. The order is independent of map iteration, goroutine
// completion, architecture and restart.
func SortRetestMembers(m []domain.RetestMember) {
	sort.SliceStable(m, func(i, j int) bool {
		a, b := m[i], m[j]
		if a.SeedLotID != b.SeedLotID {
			return a.SeedLotID < b.SeedLotID
		}
		if a.SampleID != b.SampleID {
			return a.SampleID < b.SampleID
		}
		if a.GroupID != b.GroupID {
			return a.GroupID < b.GroupID
		}
		return a.PlateIndex < b.PlateIndex
	})
}

// NextRetryOrdinal returns the next deterministic retry ordinal for an
// instrument call: the first failure is 1, and every subsequent failure
// increments. Successful receipts must match the current ordinal.
func NextRetryOrdinal(call domain.InstrumentCall) int {
	return call.RetryOrdinal + 1
}

// RecordFailure classifies an instrument failure into a stable error code
// without inventing a value. Rejection, disconnect, timeout and malformed
// payload map to distinct persistent codes.
func RecordFailure(kind string) domain.ErrorCode {
	switch kind {
	case "rejected":
		return domain.CodeInstrumentRejected
	case "disconnected":
		return domain.CodeInstrumentDisconnected
	case "timeout":
		return domain.CodeInstrumentTimeout
	default:
		return domain.CodeMalformedReceipt
	}
}
