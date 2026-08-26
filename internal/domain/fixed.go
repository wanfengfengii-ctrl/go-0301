package domain

// Fixed represents a fixed-point quantity with an explicit scale. Rates such
// as germination rate, contamination rate, abnormal-seedling rate and vigor
// index are computed with integer fixed-point arithmetic using round-half-
// away-from-zero, and every operation checks for negative measures, division
// by zero and overflow before any transaction write.
type Fixed struct {
	Raw   int64
	Scale int
}

// FixedPoint computes numerator/denominator at the given scale using integer
// arithmetic and round-half-away-from-zero. It returns a *Error on negative
// measure, division by zero, an invalid scale, or overflow.
func FixedPoint(numerator, denominator int64, scale int) (Fixed, error) {
	if scale < 0 {
		return Fixed{}, New(CodeInvalidFixedPointScale, "scale must be non-negative")
	}
	if numerator < 0 || denominator < 0 {
		return Fixed{}, New(CodeNegativeMeasure, "measures must be non-negative")
	}
	if denominator == 0 {
		return Fixed{}, New(CodeDivideByZero, "denominator is zero")
	}

	base := pow10(scale)
	if base < 0 {
		return Fixed{}, New(CodeArithmeticOverflow, "fixed-point computation overflowed")
	}
	if numerator > maxInt64/base {
		return Fixed{}, New(CodeArithmeticOverflow, "fixed-point computation overflowed")
	}

	scaled := numerator * base
	q := scaled / denominator
	r := scaled % denominator

	// Round half away from zero: for non-negative operands this is round-half-up.
	if 2*r >= denominator {
		q++
	}

	return Fixed{Raw: q, Scale: scale}, nil
}

// pow10 returns 10^n for n >= 0 without allocating.
func pow10(n int) int64 {
	v := int64(1)
	for i := 0; i < n; i++ {
		if v > maxInt64/10 {
			return -1 // overflow sentinel
		}
		v *= 10
	}
	return v
}

// maxInt64 is the largest representable int64.
const maxInt64 = int64(^uint64(0) >> 1)
