package service

import (
	"strconv"

	"seed-vault-viability-release/internal/domain"
)

// LeaseAcquireInput is the command payload for acquiring a device lease.
type LeaseAcquireInput struct {
	ID         string
	Resource   string
	Kind       domain.ResourceKind
	Holder     string
	Purpose    string
	Generation domain.GenerationNumber
	Now        int64
	Duration   int64
}

// AcquireLease grants a lease on a device if the resource is free or its
// current lease has expired at the given logical time. It enforces generation
// match and rejects a conflict with the resource id and deterministic expiry.
func (s *Service) AcquireLease(trialID string, in LeaseAcquireInput) (domain.ResourceLease, error) {
	if in.Duration < 0 {
		return domain.ResourceLease{}, domain.New(domain.CodeLeaseConflict, "duration must be non-negative")
	}
	var out domain.ResourceLease
	err := s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		if in.Generation != t.Trial.CurrentGen {
			return nil, domain.New(domain.CodeGenerationMismatch,
				"lease generation %d does not match trial generation %d", in.Generation, t.Trial.CurrentGen)
		}
		if cur, held := t.LeaseRes[in.Resource]; held {
			existing := t.Leases[cur]
			if existing.ExpiresAt > in.Now {
				return nil, domain.New(domain.CodeLeaseConflict,
					"resource %q held by %q until %d", in.Resource, existing.Holder, existing.ExpiresAt).
					WithDetails(in.Resource, strconv.FormatInt(existing.ExpiresAt, 10))
			}
			delete(t.Leases, cur)
			delete(t.LeaseRes, in.Resource)
		}
		l := domain.ResourceLease{
			ID:         in.ID,
			Resource:   in.Resource,
			Kind:       in.Kind,
			Holder:     in.Holder,
			Purpose:    in.Purpose,
			Generation: in.Generation,
			ExpiresAt:  in.Now + in.Duration,
			Version:    1,
		}
		out = l
		return []event{{trialID: trialID, typ: evLeaseAcquired, payload: leaseAcquiredPayload{Lease: l}}}, nil
	})
	return out, err
}

// RenewLease extends an existing lease if the holder matches and it has not
// expired at now.
func (s *Service) RenewLease(trialID, id, holder string, now, duration int64) (domain.ResourceLease, error) {
	var out domain.ResourceLease
	err := s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		l, ok := t.Leases[id]
		if !ok {
			return nil, domain.New(domain.CodeLeaseExpired, "lease %q unknown", id)
		}
		if l.Holder != holder {
			return nil, domain.New(domain.CodeLeaseConflict, "lease %q held by %q, not %q", id, l.Holder, holder)
		}
		if now > l.ExpiresAt {
			return nil, domain.New(domain.CodeLeaseExpired, "lease %q expired at %d", id, l.ExpiresAt)
		}
		l.ExpiresAt = now + duration
		l.Version++
		out = l
		return []event{{trialID: trialID, typ: evLeaseRenewed,
			payload: leaseRenewedPayload{ID: id, ExpiresAt: l.ExpiresAt, Version: l.Version}}}, nil
	})
	return out, err
}

// ReleaseLease frees a lease if the holder matches. It refuses to release a
// lease that has been superseded on its resource by a newer acquisition, so a
// stale holder cannot drop the current holder's index (for example after a
// restart replays an acquisition that left the old lease orphaned).
func (s *Service) ReleaseLease(trialID, id, holder string) error {
	return s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		l, ok := t.Leases[id]
		if !ok {
			return nil, domain.New(domain.CodeLeaseExpired, "lease %q unknown", id)
		}
		if l.Holder != holder {
			return nil, domain.New(domain.CodeLeaseConflict, "lease %q held by %q, not %q", id, l.Holder, holder)
		}
		// The resource index is authoritative: if a later acquisition has
		// rebound this resource to a different lease, this id is a stale,
		// superseded entry that must not be released.
		if cur, held := t.LeaseRes[l.Resource]; held && cur != id {
			return nil, domain.New(domain.CodeLeaseConflict,
				"lease %q superseded on resource %q by %q", id, l.Resource, cur).
				WithDetails(l.Resource, strconv.FormatInt(l.ExpiresAt, 10))
		}
		return []event{{trialID: trialID, typ: evLeaseReleased, payload: leaseReleasedPayload{ID: id}}}, nil
	})
}
