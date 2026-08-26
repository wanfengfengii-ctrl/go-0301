package domain_test

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
)

func TestFixedPointExactAndRounding(t *testing.T) {
	cases := []struct {
		name     string
		num, den int64
		scale    int
		wantRaw  int64
	}{
		{"exact half", 1, 2, 1, 5},
		{"round half away from zero up", 1, 4, 1, 3},
		{"round half away from zero up hundredths", 2, 3, 2, 67},
		{"integer result", 6, 3, 2, 200},
		{"zero numerator", 0, 5, 2, 0},
		{"scale zero half away", 1, 2, 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := domain.FixedPoint(c.num, c.den, c.scale)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Raw != c.wantRaw {
				t.Fatalf("FixedPoint(%d,%d,%d) = %d, want %d", c.num, c.den, c.scale, got.Raw, c.wantRaw)
			}
		})
	}
}

func TestFixedPointErrors(t *testing.T) {
	cases := []struct {
		name     string
		num, den int64
		scale    int
		code     domain.ErrorCode
	}{
		{"negative numerator", -1, 2, 1, domain.CodeNegativeMeasure},
		{"negative denominator", 1, -2, 1, domain.CodeNegativeMeasure},
		{"divide by zero", 1, 0, 1, domain.CodeDivideByZero},
		{"invalid scale", 1, 2, -1, domain.CodeInvalidFixedPointScale},
		{"overflow", 1, 1, 20, domain.CodeArithmeticOverflow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := domain.FixedPoint(c.num, c.den, c.scale)
			if !domain.IsCode(err, c.code) {
				t.Fatalf("got %v, want code %s", err, c.code)
			}
		})
	}
}
