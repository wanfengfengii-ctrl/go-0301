// Package domain defines the stable domain types, error codes and pure
// arithmetic shared by every business component of the viability-release
// service. Types in this package are the single source of truth for the
// on-disk data model and for cross-package contracts.
package domain

import (
	"fmt"
	"strings"
)

// ErrorCode is a stable, machine-readable failure code. Codes are ordered
// by the failure boundary they belong to so that HTTP responses and retry
// decisions never depend on error text.
type ErrorCode string

// Input errors (failure boundary 1).
const (
	CodeStaleRuleDigest    ErrorCode = "STALE_RULE_DIGEST"
	CodeDuplicateSampleID  ErrorCode = "DUPLICATE_SAMPLE_ID"
	CodeLineageCycle       ErrorCode = "LINEAGE_CYCLE"
	CodeMultipleParent     ErrorCode = "MULTIPLE_PARENT"
	CodeInvalidSampleCount ErrorCode = "INVALID_SAMPLE_COUNT"
	CodeMissingControl     ErrorCode = "MISSING_CONTROL"
	CodeInvalidSchedule    ErrorCode = "INVALID_SCHEDULE"
)

// Flow and concurrency errors (failure boundary 2).
const (
	CodeSampleAlreadyAllocated ErrorCode = "SAMPLE_ALREADY_ALLOCATED"
	CodeLeaseConflict          ErrorCode = "LEASE_CONFLICT"
	CodeLeaseExpired           ErrorCode = "LEASE_EXPIRED"
	CodeStageGap               ErrorCode = "STAGE_GAP"
	CodeGenerationMismatch     ErrorCode = "GENERATION_MISMATCH"
	CodeTimeRegression         ErrorCode = "TIME_REGRESSION"
	CodeObservationRegression  ErrorCode = "OBSERVATION_REGRESSION"
	CodeTerminalAlreadyDecided ErrorCode = "TERMINAL_ALREADY_DECIDED"
)

// Arithmetic errors (failure boundary 3).
const (
	CodeNegativeMeasure        ErrorCode = "NEGATIVE_MEASURE"
	CodeDivideByZero           ErrorCode = "DIVIDE_BY_ZERO"
	CodeArithmeticOverflow     ErrorCode = "ARITHMETIC_OVERFLOW"
	CodeInvalidFixedPointScale ErrorCode = "INVALID_FIXED_POINT_SCALE"
)

// Instrument failures (failure boundary 4).
const (
	CodeInstrumentRejected     ErrorCode = "INSTRUMENT_REJECTED"
	CodeInstrumentDisconnected ErrorCode = "INSTRUMENT_DISCONNECTED"
	CodeInstrumentTimeout      ErrorCode = "INSTRUMENT_TIMEOUT"
	CodeMalformedReceipt       ErrorCode = "MALFORMED_RECEIPT"
)

// Idempotency conflict (failure boundary 10).
const (
	CodeIdempotencyConflict ErrorCode = "IDEMPOTENCY_CONFLICT"
)

// Error is a domain failure carrying a stable code, a human message and an
// ordered list of detail records (for example the canonical sort order of a
// rejected batch).
type Error struct {
	Code    ErrorCode
	Message string
	Details []string
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if len(e.Details) == 0 {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s [%s]", e.Code, e.Message, strings.Join(e.Details, ", "))
}

// New returns a domain error with the given code and formatted message.
func New(code ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WithDetails attaches ordered detail records to an existing domain error.
func (e *Error) WithDetails(details ...string) *Error {
	if e == nil {
		return &Error{Details: append([]string(nil), details...)}
	}
	out := *e
	out.Details = append(append([]string(nil), e.Details...), details...)
	return &out
}

// IsCode reports whether err carries the given stable code.
func IsCode(err error, code ErrorCode) bool {
	de, ok := err.(*Error)
	return ok && de.Code == code
}
