package service

import (
	"encoding/json"
	"fmt"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/lineage"
	"seed-vault-viability-release/internal/treatment"
)

// Event type names. These are stable strings persisted in the event log; they
// must never change once written, or replay would fail.
const (
	evTrialCreated        = "trial.created"
	evTrialLocked         = "trial.locked"
	evSampleAllocated     = "sample.allocated"
	evLeaseAcquired       = "lease.acquired"
	evLeaseRenewed        = "lease.renewed"
	evLeaseReleased       = "lease.released"
	evTreatmentRecorded   = "treatment.recorded"
	evEnvironmentRecorded = "environment.recorded"
	evObservationRecorded = "observation.recorded"
	evInstrumentCall      = "instrument.call"
	evInstrumentReceipt   = "instrument.receipt"
	evRetestCreated       = "retest.created"
	evReviewSubmitted     = "review.submitted"
	evTerminalDecided     = "terminal.decided"
)

// event is a pending, not-yet-persisted mutation.
type event struct {
	trialID string
	typ     string
	payload any
}

// --- event payloads (also used verbatim for replay) ---

type trialCreatedPayload struct {
	ID      string `json:"id"`
	Species string `json:"species"`
	At      int64  `json:"at"`
}

type trialLockedPayload struct {
	Version  string              `json:"version"`
	Digest   string              `json:"digest"`
	At       int64               `json:"at"`
	Snapshot domain.RuleSnapshot `json:"snapshot"`
}

type allocatedPayload struct {
	SampleID   string                  `json:"sample_id"`
	Generation domain.GenerationNumber `json:"generation"`
	SeedLots   []domain.SeedLot        `json:"seed_lots"`
	Samples    []domain.SampleUnit     `json:"samples"`
	Groups     []domain.CultureGroup   `json:"groups"`
	Plates     []domain.Plate          `json:"plates"`
	Allocation lineage.Allocation      `json:"allocation"`
	Leases     []domain.ResourceLease  `json:"leases"`
	At         int64                   `json:"at"`
}

type leaseAcquiredPayload struct {
	Lease domain.ResourceLease `json:"lease"`
}

type leaseRenewedPayload struct {
	ID        string `json:"id"`
	ExpiresAt int64  `json:"expires_at"`
	Version   int64  `json:"version"`
}

type leaseReleasedPayload struct {
	ID string `json:"id"`
}

type treatmentRecordedPayload struct {
	Event domain.TreatmentEvent `json:"event"`
}

type observationRecordedPayload struct {
	Observation domain.Observation `json:"observation"`
}

type environmentRecordedPayload struct {
	Evidence domain.EnvironmentEvidence `json:"evidence"`
}

type instrumentCallPayload struct {
	Call domain.InstrumentCall `json:"call"`
}

type instrumentReceiptPayload struct {
	CallID       string                  `json:"call_id"`
	Status       domain.InstrumentStatus `json:"status"`
	Failure      domain.ErrorCode        `json:"failure,omitempty"`
	Payload      string                  `json:"payload,omitempty"`
	RetryOrdinal int                     `json:"retry_ordinal"`
}

type retestCreatedPayload struct {
	RetestSet domain.RetestSet        `json:"retest_set"`
	NewGen    domain.GenerationNumber `json:"new_generation"`
	At        int64                   `json:"at"`
}

type reviewSubmittedPayload struct {
	Review domain.Review `json:"review"`
}

type terminalDecidedPayload struct {
	Credential domain.TerminalCredential `json:"credential"`
}

// trialState is the in-memory projection of a single viability trial. It is
// rebuilt by replaying the persisted event log and is the only place where
// live state lives between writes.
type trialState struct {
	Trial        domain.ViabilityTrial
	Snapshot     *domain.RuleSnapshot
	SeedLots     map[string]domain.SeedLot
	Samples      map[string]domain.SampleUnit
	Groups       map[string]domain.CultureGroup
	Plates       map[string]domain.Plate
	Graph        *lineage.Graph
	Allocations  map[string]lineage.Allocation
	StageSeq     map[string]*treatment.StageSequence
	Treatments   []domain.TreatmentEvent
	Leases       map[string]domain.ResourceLease
	LeaseRes     map[string]string
	Observations map[string][]domain.Observation
	Environments []domain.EnvironmentEvidence
	Calls        map[string]domain.InstrumentCall
	Retests      map[string]domain.RetestSet
	Reviews      map[string]domain.Review
	Credential   *domain.TerminalCredential
	Faulted      bool
	lastSeq      int64
}

func newTrialState(id string) *trialState {
	return &trialState{
		Trial:        domain.ViabilityTrial{ID: id, Terminal: domain.TerminalStatusOpen},
		SeedLots:     make(map[string]domain.SeedLot),
		Samples:      make(map[string]domain.SampleUnit),
		Groups:       make(map[string]domain.CultureGroup),
		Plates:       make(map[string]domain.Plate),
		Graph:        lineage.NewGraph(),
		Allocations:  make(map[string]lineage.Allocation),
		StageSeq:     make(map[string]*treatment.StageSequence),
		Leases:       make(map[string]domain.ResourceLease),
		LeaseRes:     make(map[string]string),
		Observations: make(map[string][]domain.Observation),
		Calls:        make(map[string]domain.InstrumentCall),
		Retests:      make(map[string]domain.RetestSet),
		Reviews:      make(map[string]domain.Review),
	}
}

// advanceClock enforces the monotonic logical clock per trial.
func (t *trialState) advanceClock(at int64) error {
	if at < t.Trial.LogicalClock {
		return domain.New(domain.CodeTimeRegression,
			"logical time %d is earlier than current clock %d", at, t.Trial.LogicalClock)
	}
	if at > t.Trial.LogicalClock {
		t.Trial.LogicalClock = at
	}
	return nil
}

// apply replays one event payload onto the projection.
func (t *trialState) apply(typ string, raw json.RawMessage) error {
	if t.Faulted {
		return nil // a faulted trial stops accepting replayed mutations
	}
	switch typ {
	case evTrialCreated:
		var p trialCreatedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		t.Trial.Species = p.Species
	case evTrialLocked:
		var p trialLockedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		snap := p.Snapshot
		t.Snapshot = &snap
		t.Trial.Locked = true
		t.Trial.CurrentGen = 1
		t.Trial.InputDigest = p.Digest
	case evSampleAllocated:
		var p allocatedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return t.applyAllocation(p)
	case evLeaseAcquired:
		var p leaseAcquiredPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		t.Leases[p.Lease.ID] = p.Lease
		t.LeaseRes[p.Lease.Resource] = p.Lease.ID
	case evLeaseRenewed:
		var p leaseRenewedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if l, ok := t.Leases[p.ID]; ok {
			l.ExpiresAt = p.ExpiresAt
			l.Version = p.Version
			t.Leases[p.ID] = l
		}
	case evLeaseReleased:
		var p leaseReleasedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if l, ok := t.Leases[p.ID]; ok {
			delete(t.Leases, p.ID)
			delete(t.LeaseRes, l.Resource)
		}
	case evTreatmentRecorded:
		var p treatmentRecordedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return t.applyTreatment(p.Event)
	case evObservationRecorded:
		var p observationRecordedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return t.applyObservation(p.Observation)
	case evEnvironmentRecorded:
		var p environmentRecordedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		t.Environments = append(t.Environments, p.Evidence)
		if p.Evidence.LogicalTime > t.Trial.LogicalClock {
			t.Trial.LogicalClock = p.Evidence.LogicalTime
		}
	case evInstrumentCall:
		var p instrumentCallPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		t.Calls[p.Call.ID] = p.Call
	case evInstrumentReceipt:
		var p instrumentReceiptPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if c, ok := t.Calls[p.CallID]; ok {
			c.Status = p.Status
			c.Failure = p.Failure
			c.Payload = p.Payload
			c.RetryOrdinal = p.RetryOrdinal
			t.Calls[p.CallID] = c
		}
	case evRetestCreated:
		var p retestCreatedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		t.Retests[retestKey(p.RetestSet.SourceGen, p.RetestSet.Reason)] = p.RetestSet
		t.Trial.CurrentGen = p.NewGen
	case evReviewSubmitted:
		var p reviewSubmittedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		t.Reviews[p.Review.ReviewerID] = p.Review
	case evTerminalDecided:
		var p terminalDecidedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		cred := p.Credential
		t.Credential = &cred
		t.Trial.Terminal = domain.TerminalStatusDecided
	default:
		return fmt.Errorf("unknown event type %q", typ)
	}
	return nil
}

// applyAllocation replays a sample-allocation event: seed lots, samples,
// groups, plates, lineage edges, the grain allocation and any lease
// acquisitions are all restored atomically.
func (t *trialState) applyAllocation(p allocatedPayload) error {
	for _, sl := range p.SeedLots {
		t.SeedLots[sl.ID] = sl
		if sl.ParentID != "" {
			if err := t.Graph.AddEdge(sl.ParentID, sl.ID); err != nil {
				return err
			}
		}
	}
	for _, su := range p.Samples {
		t.Samples[su.ID] = su
		if su.SeedLotID != "" {
			if err := t.Graph.AddEdge(su.SeedLotID, su.ID); err != nil {
				return err
			}
		}
	}
	for _, g := range p.Groups {
		t.Groups[g.ID] = g
		if g.SampleID != "" {
			if err := t.Graph.AddEdge(g.SampleID, g.ID); err != nil {
				return err
			}
		}
	}
	for _, pl := range p.Plates {
		t.Plates[pl.ID] = pl
		if pl.GroupID != "" {
			if err := t.Graph.AddEdge(pl.GroupID, pl.ID); err != nil {
				return err
			}
		}
	}
	t.Allocations[p.SampleID] = p.Allocation
	for _, l := range p.Leases {
		t.Leases[l.ID] = l
		t.LeaseRes[l.Resource] = l.ID
	}
	return nil
}

// applyTreatment appends a treatment event and advances the plate's stage
// sequence, lazily initialising it from the locked snapshot's applicable
// stages.
func (t *trialState) applyTreatment(ev domain.TreatmentEvent) error {
	seq, ok := t.StageSeq[ev.PlateID]
	if !ok {
		stages := []domain.Stage{}
		if t.Snapshot != nil {
			stages = append(stages, t.Snapshot.Stages...)
		}
		seq = treatment.NewStageSequence(stages)
		t.StageSeq[ev.PlateID] = seq
	}
	if err := seq.Advance(ev.Stage); err != nil {
		return err
	}
	t.Treatments = append(t.Treatments, ev)
	if ev.LogicalTime > t.Trial.LogicalClock {
		t.Trial.LogicalClock = ev.LogicalTime
	}
	return nil
}

// applyObservation appends an observation to its plate's history.
func (t *trialState) applyObservation(o domain.Observation) error {
	t.Observations[o.PlateID] = append(t.Observations[o.PlateID], o)
	if o.LogicalTime > t.Trial.LogicalClock {
		t.Trial.LogicalClock = o.LogicalTime
	}
	return nil
}

// plateStage returns the most recent stage recorded for a plate.
func (t *trialState) plateStage(plateID string) domain.Stage {
	for i := len(t.Treatments) - 1; i >= 0; i-- {
		if t.Treatments[i].PlateID == plateID {
			return t.Treatments[i].Stage
		}
	}
	return ""
}

// nextStage returns the stage that must follow the plate's current stage in
// the locked snapshot's ordered applicable stages.
func (t *trialState) nextStage(plateID string) (domain.Stage, bool) {
	stages := []domain.Stage{}
	if t.Snapshot != nil {
		stages = t.Snapshot.Stages
	}
	cur := t.plateStage(plateID)
	if cur == "" {
		if len(stages) == 0 {
			return "", false
		}
		return stages[0], true
	}
	for i, st := range stages {
		if st == cur {
			if i+1 < len(stages) {
				return stages[i+1], true
			}
			return "", false
		}
	}
	return "", false
}
