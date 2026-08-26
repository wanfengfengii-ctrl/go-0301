// Package treatment implements the pretreatment and culture stage machine and
// the resource lease engine. Stages advance as a contiguous prefix of the
// locked scheme, and device evidence requires a non-expired lease whose
// resource, purpose, operator and generation all match.
package treatment

import (
	"strconv"

	"seed-vault-viability-release/internal/domain"
)

// StageSequence tracks the ordered applicable stages for a single plate and
// enforces that advances form a contiguous prefix of the canonical order.
type StageSequence struct {
	applicable []domain.Stage
	current    domain.Stage
	started    bool
}

// NewStageSequence builds a sequence from the given ordered stages. The caller
// supplies stages already in canonical order.
func NewStageSequence(stages []domain.Stage) *StageSequence {
	return &StageSequence{applicable: append([]domain.Stage(nil), stages...)}
}

// Applicable returns the ordered applicable stages.
func (s *StageSequence) Applicable() []domain.Stage {
	return append([]domain.Stage(nil), s.applicable...)
}

// Current returns the current stage; the zero stage is reported before the
// first advance.
func (s *StageSequence) Current() domain.Stage {
	if !s.started {
		return ""
	}
	return s.current
}

// Advance moves to the given stage only if it is the immediate next applicable
// stage. It returns STAGE_GAP otherwise, leaving the sequence unchanged.
func (s *StageSequence) Advance(next domain.Stage) error {
	if !s.started {
		if len(s.applicable) == 0 {
			return domain.New(domain.CodeStageGap, "plate has no applicable stages")
		}
		if next != s.applicable[0] {
			return domain.New(domain.CodeStageGap, "first stage must be %q, got %q", s.applicable[0], next)
		}
		s.current = next
		s.started = true
		return nil
	}
	idx := -1
	for i, st := range s.applicable {
		if st == s.current {
			idx = i
			break
		}
	}
	if idx == -1 || idx+1 >= len(s.applicable) {
		return domain.New(domain.CodeStageGap, "no stage after %q", s.current)
	}
	if next != s.applicable[idx+1] {
		return domain.New(domain.CodeStageGap, "expected %q after %q, got %q", s.applicable[idx+1], s.current, next)
	}
	s.current = next
	return nil
}

// LeaseRequest describes an acquisition request on the logical clock.
type LeaseRequest struct {
	ID         string
	Resource   string
	Kind       domain.ResourceKind
	Holder     string
	Purpose    string
	Generation domain.GenerationNumber
	Now        int64
	Duration   int64
}

// LeaseManager is the lease book for devices. Acquire, renew and release all
// operate on a logical clock; an expired lease must not protect evidence.
type LeaseManager interface {
	Acquire(LeaseRequest) (domain.ResourceLease, error)
	Renew(id, holder string, now, duration int64) (domain.ResourceLease, error)
	Release(id, holder string, now int64) error
}

// LeaseStore is the in-memory implementation of LeaseManager. It is guarded by
// a mutex so concurrent acquisitions of the same resource resolve to a single
// winner.
type LeaseStore struct {
	byID       map[string]domain.ResourceLease
	byResource map[string]string // resource -> lease id
}

// NewLeaseStore returns an empty lease store.
func NewLeaseStore() *LeaseStore {
	return &LeaseStore{
		byID:       make(map[string]domain.ResourceLease),
		byResource: make(map[string]string),
	}
}

// Acquire grants a lease if the resource is free or its current lease has
// expired at the request's logical time. A conflict reports the resource and
// the current deterministic expiry.
func (s *LeaseStore) Acquire(req LeaseRequest) (domain.ResourceLease, error) {
	if req.Duration < 0 {
		return domain.ResourceLease{}, domain.New(domain.CodeLeaseConflict, "duration must be non-negative")
	}
	if cur, ok := s.byResource[req.Resource]; ok {
		existing := s.byID[cur]
		if existing.ExpiresAt > req.Now {
			return domain.ResourceLease{}, domain.New(domain.CodeLeaseConflict,
				"resource %q held by %q until %d", req.Resource, existing.Holder, existing.ExpiresAt).
				WithDetails(req.Resource, strconv.FormatInt(existing.ExpiresAt, 10))
		}
		// existing lease expired: reclaim the resource
		delete(s.byID, cur)
		delete(s.byResource, req.Resource)
	}
	l := domain.ResourceLease{
		ID:         req.ID,
		Resource:   req.Resource,
		Kind:       req.Kind,
		Holder:     req.Holder,
		Purpose:    req.Purpose,
		Generation: req.Generation,
		ExpiresAt:  req.Now + req.Duration,
		Version:    1,
	}
	s.byID[l.ID] = l
	s.byResource[l.Resource] = l.ID
	return l, nil
}

// Renew extends a lease if the holder matches and it has not expired at now.
func (s *LeaseStore) Renew(id, holder string, now, duration int64) (domain.ResourceLease, error) {
	l, ok := s.byID[id]
	if !ok {
		return domain.ResourceLease{}, domain.New(domain.CodeLeaseExpired, "lease %q unknown", id)
	}
	if l.Holder != holder {
		return domain.ResourceLease{}, domain.New(domain.CodeLeaseConflict, "lease %q held by %q, not %q", id, l.Holder, holder)
	}
	if now > l.ExpiresAt {
		return domain.ResourceLease{}, domain.New(domain.CodeLeaseExpired, "lease %q expired at %d", id, l.ExpiresAt)
	}
	l.ExpiresAt = now + duration
	l.Version++
	s.byID[id] = l
	return l, nil
}

// Release frees a lease if the holder matches.
func (s *LeaseStore) Release(id, holder string, now int64) error {
	l, ok := s.byID[id]
	if !ok {
		return domain.New(domain.CodeLeaseExpired, "lease %q unknown", id)
	}
	if l.Holder != holder {
		return domain.New(domain.CodeLeaseConflict, "lease %q held by %q, not %q", id, l.Holder, holder)
	}
	delete(s.byID, id)
	delete(s.byResource, l.Resource)
	return nil
}
