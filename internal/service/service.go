// Package service is the application orchestration layer. It binds the rules
// catalogue, the lineage graph, the treatment and lease engine, the observation
// and retest engine, and the review and terminal arbitration into one
// transactionally persisted, restartable service. Every mutation is applied to
// an in-memory projection and appended to the durable event log in the same
// atomic step, so a crash never leaves a partial write and a restart replays
// the log to recover the exact state.
package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/rules"
	"seed-vault-viability-release/internal/store"
)

// Service coordinates all business flows for the viability-release closed loop.
type Service struct {
	store   *store.Store
	catalog rules.Catalog

	mu sync.Mutex
	// now is the logical clock; injectable for deterministic tests.
	now func() int64

	trials map[string]*trialState
}

// New returns a Service backed by st and resolving rules from catalog.
func New(st *store.Store, catalog rules.Catalog) *Service {
	return &Service{
		store:   st,
		catalog: catalog,
		now:     func() int64 { return time.Now().UnixMilli() },
		trials:  make(map[string]*trialState),
	}
}

// Recover rebuilds the in-memory projections by verifying the event hash chain
// and replaying every persisted event from genesis. A broken chain faults the
// affected trial read-only instead of guessing state.
func (s *Service) Recover() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if broken, err := s.store.VerifyChain(); err != nil {
		return fmt.Errorf("verify chain: %w", err)
	} else if broken != 0 {
		return fmt.Errorf("event chain broken at seq %d", broken)
	}

	events, err := s.store.EventsSince(0)
	if err != nil {
		return fmt.Errorf("load events: %w", err)
	}
	s.trials = make(map[string]*trialState)
	for _, e := range events {
		if err := s.applyEvent(e); err != nil {
			return fmt.Errorf("replay event %d (%s): %w", e.Seq, e.Type, err)
		}
	}
	return nil
}

// applyEvent replays a single persisted event into the projection.
func (s *Service) applyEvent(e store.Event) error {
	t, ok := s.trials[e.TrialID]
	if !ok {
		t = newTrialState(e.TrialID)
		s.trials[e.TrialID] = t
	}
	return t.apply(e.Type, json.RawMessage(e.Payload))
}

// mutate runs fn while holding the exclusive lock, then appends the produced
// events to the store inside one transaction and applies them to the
// in-memory projection. The projection is updated only after the events
// durably commit, so a projection change is never exposed without its event.
// On append failure the projection is left untouched.
func (s *Service) mutate(fn func() ([]event, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	evts, err := fn()
	if err != nil {
		return err
	}
	if len(evts) == 0 {
		return nil
	}
	drafts := make([]store.EventDraft, len(evts))
	for i, e := range evts {
		drafts[i] = store.EventDraft{TrialID: e.trialID, Type: e.typ, Payload: e.payload}
	}
	if err := s.store.AppendMany(drafts); err != nil {
		return err
	}
	for _, e := range evts {
		raw, err := json.Marshal(e.payload)
		if err != nil {
			return err
		}
		t, ok := s.trials[e.trialID]
		if !ok {
			t = newTrialState(e.trialID)
			s.trials[e.trialID] = t
		}
		if err := t.apply(e.typ, raw); err != nil {
			return err
		}
	}
	return nil
}

// readTrial returns a projection snapshot under the lock, without permitting
// concurrent mutation.
func (s *Service) readTrial(id string) (*trialState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.trials[id]
	return t, ok
}

// TrialSummaries returns a stable, id-ordered summary of every known trial.
func (s *Service) TrialSummaries() []domain.ViabilityTrial {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.ViabilityTrial, 0, len(s.trials))
	for _, t := range s.trials {
		out = append(out, t.Trial)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
