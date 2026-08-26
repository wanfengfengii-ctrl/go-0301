package observation_test

import (
	"strconv"
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/observation"
)

func TestValidateCountsMonotonicAndBounded(t *testing.T) {
	prev := map[domain.ObservationClass]int64{domain.ClassGerminated: 5}
	next := map[domain.ObservationClass]int64{domain.ClassGerminated: 8, domain.ClassHard: 1}
	if err := observation.ValidateCounts(prev, next, 100); err != nil {
		t.Fatalf("valid counts rejected: %v", err)
	}
}

func TestValidateCountsRegression(t *testing.T) {
	prev := map[domain.ObservationClass]int64{domain.ClassGerminated: 8}
	next := map[domain.ObservationClass]int64{domain.ClassGerminated: 3}
	if err := observation.ValidateCounts(prev, next, 100); !domain.IsCode(err, domain.CodeObservationRegression) {
		t.Fatalf("got %v, want OBSERVATION_REGRESSION", err)
	}
}

func TestValidateCountsExceedsSown(t *testing.T) {
	next := map[domain.ObservationClass]int64{domain.ClassGerminated: 6}
	if err := observation.ValidateCounts(nil, next, 5); !domain.IsCode(err, domain.CodeObservationRegression) {
		t.Fatalf("got %v, want OBSERVATION_REGRESSION", err)
	}
}

func TestSortRetestMembersCanonical(t *testing.T) {
	m := []domain.RetestMember{
		{SeedLotID: "lot-b", SampleID: "s1", GroupID: "g1", PlateIndex: 2},
		{SeedLotID: "lot-a", SampleID: "s2", GroupID: "g1", PlateIndex: 1},
		{SeedLotID: "lot-a", SampleID: "s1", GroupID: "g1", PlateIndex: 1},
	}
	observation.SortRetestMembers(m)
	want := []string{"lot-a/s1/g1/1", "lot-a/s2/g1/1", "lot-b/s1/g1/2"}
	for i, w := range want {
		got := m[i].SeedLotID + "/" + m[i].SampleID + "/" + m[i].GroupID + "/" + itoa(m[i].PlateIndex)
		if got != w {
			t.Fatalf("member[%d] = %s, want %s", i, got, w)
		}
	}
}

func TestRecordFailureMapping(t *testing.T) {
	cases := map[string]domain.ErrorCode{
		"rejected":     domain.CodeInstrumentRejected,
		"disconnected": domain.CodeInstrumentDisconnected,
		"timeout":      domain.CodeInstrumentTimeout,
		"malformed":    domain.CodeMalformedReceipt,
	}
	for in, want := range cases {
		if got := observation.RecordFailure(in); got != want {
			t.Fatalf("RecordFailure(%q) = %s, want %s", in, got, want)
		}
	}
}

func itoa(v int) string { return strconv.Itoa(v) }
