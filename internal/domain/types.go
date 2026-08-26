package domain

import "time"

// Stage is a strictly ordered treatment phase. The canonical sequence is
// warmup -> dormancy_break -> sowing -> incubation -> observation -> closed.
// A locked scheme may declare stages not applicable, but the sequence formed
// by the applied stages must remain a contiguous prefix of the order above.
type Stage string

const (
	StageWarmup        Stage = "warmup"
	StageDormancyBreak Stage = "dormancy_break"
	StageSowing        Stage = "sowing"
	StageIncubation    Stage = "incubation"
	StageObservation   Stage = "observation"
	StageClosed        Stage = "closed"
)

// StageOrder is the canonical treatment stage order.
var StageOrder = []Stage{
	StageWarmup,
	StageDormancyBreak,
	StageSowing,
	StageIncubation,
	StageObservation,
	StageClosed,
}

// StageIndex returns the position of s in the canonical order.
func StageIndex(s Stage) (int, bool) {
	for i, st := range StageOrder {
		if st == s {
			return i, true
		}
	}
	return -1, false
}

// ObservationClass is a mutually exclusive and collectively exhaustive
// classification for a plate at a given observation point.
type ObservationClass string

const (
	ClassGerminated   ObservationClass = "germinated"
	ClassHard         ObservationClass = "hard"
	ClassDecayed      ObservationClass = "decayed"
	ClassAbnormal     ObservationClass = "abnormal"
	ClassUngerminated ObservationClass = "ungerminated"
)

// TerminalType is the single-writer final outcome of a viability trial.
type TerminalType string

const (
	TerminalRelease    TerminalType = "release"
	TerminalQuarantine TerminalType = "quarantine"
	TerminalVoid       TerminalType = "void"
)

// ResourceKind enumerates the device classes that may be leased.
type ResourceKind string

const (
	ResourceIncubator ResourceKind = "incubator"
	ResourceWaterBath ResourceKind = "water_bath"
	ResourceImager    ResourceKind = "imager"
)

// RuleSnapshot is an immutable copy of the rules catalogue in force at lock
// time: species rules, scheme stages, environment ranges, observation
// schedule, fixed-point scale, thresholds and reviewer qualifications.
type RuleSnapshot struct {
	Version           string              `json:"version"`
	Digest            string              `json:"digest,omitempty"`
	Species           string              `json:"species"`
	Stages            []Stage             `json:"stages"`
	EnvironmentRanges []EnvironmentRange  `json:"environment_ranges"`
	Schedule          ObservationSchedule `json:"schedule"`
	FixedPointScale   int                 `json:"fixed_point_scale"`
	Thresholds        Thresholds          `json:"thresholds"`
	Qualification     QualificationRules  `json:"qualification"`
}

// EnvironmentRange constrains a single environment dimension.
type EnvironmentRange struct {
	Dimension string `json:"dimension"`
	Min       int64  `json:"min"`
	Max       int64  `json:"max"`
}

// ObservationSchedule is the locked observation cadence.
type ObservationSchedule struct {
	Intervals []time.Duration `json:"intervals"`
	Window    time.Duration   `json:"window"`
}

// Thresholds holds the viability decision thresholds in fixed-point form.
type Thresholds struct {
	MinGermination   int64 `json:"min_germination"`
	MaxContamination int64 `json:"max_contamination"`
	MinVigor         int64 `json:"min_vigor"`
}

// QualificationRules describes who may review and sign a trial.
type QualificationRules struct {
	MinDistinctReviewers int      `json:"min_distinct_reviewers"`
	RequiredRoles        []string `json:"required_roles"`
}

// ViabilityTrial is the root aggregate for one seed-viability trial.
type ViabilityTrial struct {
	ID           string           `json:"id"`
	Species      string           `json:"species"`
	Locked       bool             `json:"locked"`
	CurrentGen   GenerationNumber `json:"current_gen"`
	InputDigest  string           `json:"input_digest"`
	LogicalClock int64            `json:"logical_clock"`
	RecoveryVer  int64            `json:"recovery_ver"`
	Terminal     TerminalStatus   `json:"terminal"`
}

// GenerationNumber identifies a generation within a trial.
type GenerationNumber int

// TerminalStatus is the final-state flag of a trial.
type TerminalStatus string

const (
	TerminalStatusOpen    TerminalStatus = "open"
	TerminalStatusDecided TerminalStatus = "decided"
	TerminalStatusFaulted TerminalStatus = "faulted"
)

// SeedLot is a seed storage batch. Samples are split from it.
type SeedLot struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	Species  string `json:"species"`
	Location string `json:"location"`
	Count    int64  `json:"count"`
}

// SampleUnit is a divided portion of a seed lot with an integer grain count.
type SampleUnit struct {
	ID        string `json:"id"`
	SeedLotID string `json:"seed_lot_id"`
	Count     int64  `json:"count"`
	Moisture  int64  `json:"moisture"`
}

// CultureGroup is a set of plates allocated from one sample unit.
type CultureGroup struct {
	ID         string           `json:"id"`
	TrialID    string           `json:"trial_id"`
	SampleID   string           `json:"sample_id"`
	SeedLotID  string           `json:"seed_lot_id"`
	Generation GenerationNumber `json:"generation"`
	Count      int64            `json:"count"`
}

// Plate is a single culture plate within a group, positioned in the incubator.
type Plate struct {
	ID         string           `json:"id"`
	GroupID    string           `json:"group_id"`
	Position   int              `json:"position"`
	Generation GenerationNumber `json:"generation"`
	Sown       int64            `json:"sown"`
}

// LineageEvent is an append-only lineage edge with a single parent.
type LineageEvent struct {
	ID         string           `json:"id"`
	ParentID   string           `json:"parent_id"`
	ChildID    string           `json:"child_id"`
	ChildKind  string           `json:"child_kind"`
	Generation GenerationNumber `json:"generation"`
	At         int64            `json:"at"`
}

// TreatmentEvent records one stage transition with evidence digest.
type TreatmentEvent struct {
	ID          string           `json:"id"`
	TrialID     string           `json:"trial_id"`
	PlateID     string           `json:"plate_id"`
	Stage       Stage            `json:"stage"`
	Operator    string           `json:"operator"`
	LogicalTime int64            `json:"logical_time"`
	Evidence    string           `json:"evidence"`
	Generation  GenerationNumber `json:"generation"`
}

// ResourceLease is a mutually exclusive, expiring lease on a device.
type ResourceLease struct {
	ID         string           `json:"id"`
	Resource   string           `json:"resource"`
	Kind       ResourceKind     `json:"kind"`
	Holder     string           `json:"holder"`
	Purpose    string           `json:"purpose"`
	Generation GenerationNumber `json:"generation"`
	ExpiresAt  int64            `json:"expires_at"`
	Version    int64            `json:"version"`
}

// Observation is a plate classification count at a point in time.
type Observation struct {
	ID          string                     `json:"id"`
	PlateID     string                     `json:"plate_id"`
	Generation  GenerationNumber           `json:"generation"`
	LogicalTime int64                      `json:"logical_time"`
	Counts      map[ObservationClass]int64 `json:"counts"`
	Operator    string                     `json:"operator"`
}

// EnvironmentEvidence is an environment reading captured during a trial.
type EnvironmentEvidence struct {
	ID          string `json:"id"`
	TrialID     string `json:"trial_id"`
	Dimension   string `json:"dimension"`
	Value       int64  `json:"value"`
	LogicalTime int64  `json:"logical_time"`
}

// InstrumentCall is a request to an external device with a deterministic
// retry ordinal; a matching successful receipt is required to form evidence.
type InstrumentCall struct {
	ID           string           `json:"id"`
	TrialID      string           `json:"trial_id"`
	Generation   GenerationNumber `json:"generation"`
	Summary      string           `json:"summary"`
	RetryOrdinal int              `json:"retry_ordinal"`
	Status       InstrumentStatus `json:"status"`
	Failure      ErrorCode        `json:"failure,omitempty"`
	Payload      string           `json:"payload,omitempty"`
}

// InstrumentStatus is the lifecycle state of an instrument call.
type InstrumentStatus string

const (
	InstrumentPending   InstrumentStatus = "pending"
	InstrumentRetrying  InstrumentStatus = "retrying"
	InstrumentSucceeded InstrumentStatus = "succeeded"
	InstrumentFailed    InstrumentStatus = "failed"
)

// RetestSet is a normalized, ordered retest collection for one anomaly source.
type RetestSet struct {
	ID        string           `json:"id"`
	TrialID   string           `json:"trial_id"`
	SourceGen GenerationNumber `json:"source_gen"`
	TargetGen GenerationNumber `json:"target_gen"`
	Reason    string           `json:"reason"`
	Members   []RetestMember   `json:"members"`
	Digest    string           `json:"digest"`
	Complete  bool             `json:"complete"`
}

// RetestMember is one canonical member of a retest set.
type RetestMember struct {
	SeedLotID  string `json:"seed_lot_id"`
	SampleID   string `json:"sample_id"`
	GroupID    string `json:"group_id"`
	PlateIndex int    `json:"plate_index"`
}

// Review is an independent reviewer signature.
type Review struct {
	ID            string `json:"id"`
	TrialID       string `json:"trial_id"`
	ReviewerID    string `json:"reviewer_id"`
	Qualification string `json:"qualification"`
	Digest        string `json:"digest"`
	SubmittedAt   int64  `json:"submitted_at"`
}

// TerminalCredential is the permanent single-writer final outcome.
type TerminalCredential struct {
	ID          string       `json:"id"`
	TrialID     string       `json:"trial_id"`
	Type        TerminalType `json:"type"`
	Number      string       `json:"number"`
	SubmittedAt int64        `json:"submitted_at"`
	Reviews     []string     `json:"reviews"`
}

// IdempotencyRecord binds an idempotency key to a command digest and result.
type IdempotencyRecord struct {
	Key       string `json:"key"`
	Digest    string `json:"digest"`
	Result    string `json:"result"`
	CreatedAt int64  `json:"created_at"`
}
