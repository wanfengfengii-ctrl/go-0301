package service

import (
	"sort"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/lineage"
)

// AllocateInput is the command payload for atomically allocating one sample
// unit's grains across culture, retain, measurement, quarantine and loss, while
// recording the surrounding seed lots, samples, groups and plates.
type AllocateInput struct {
	SampleID   string
	Allocation lineage.Allocation
	SeedLots   []domain.SeedLot
	Samples    []domain.SampleUnit
	Groups     []domain.CultureGroup
	Plates     []domain.Plate
}

// AllocateSamples validates and then atomically records the sample universe and
// grain allocation for a locked trial. It rejects duplicate sample identities,
// broken conservation, lineage cycles and multiple parents, and re-allocation
// of an already-allocated sample. Any validation failure leaves no partial
// state behind (the single sample.allocated event is all-or-nothing).
func (s *Service) AllocateSamples(trialID string, in AllocateInput) error {
	if err := lineage.ValidateAllocation(in.Allocation); err != nil {
		return err
	}

	return s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		if !t.Trial.Locked {
			return nil, domain.New(domain.CodeStageGap, "trial %q is not locked", trialID)
		}
		if _, exists := t.Allocations[in.SampleID]; exists {
			return nil, domain.New(domain.CodeSampleAlreadyAllocated,
				"sample %q already allocated", in.SampleID)
		}

		// Stage the new nodes and edges in a scratch graph so that any cycle or
		// multiple-parent violation aborts before a single byte is written.
		scratch := lineage.NewGraph()
		for child, parent := range t.Graph.Parents() {
			if err := scratch.AddEdge(parent, child); err != nil {
				return nil, err
			}
		}
		// seen tracks identities within this request only, so a repeated ID in
		// a single payload is rejected as DUPLICATE_SAMPLE_ID.
		seen := map[string]bool{}
		// Cross-request duplication is checked after the structural edge test:
		// resending an existing node with a different parent must still surface
		// as MULTIPLE_PARENT / LINEAGE_CYCLE, while resending it with the same
		// (or absent) parent but changed data — e.g. lot-1 with a new count or
		// species — is rejected as DUPLICATE_SAMPLE_ID rather than silently
		// overwriting the stored node when the event replays.
		for _, sl := range in.SeedLots {
			if seen[sl.ID] {
				return nil, duplicateSampleErr(sl.ID)
			}
			seen[sl.ID] = true
			if sl.ParentID != "" {
				if err := scratch.AddEdge(sl.ParentID, sl.ID); err != nil {
					return nil, err
				}
			}
			if _, exists := t.SeedLots[sl.ID]; exists {
				return nil, duplicateSampleErr(sl.ID)
			}
		}
		for _, su := range in.Samples {
			if seen[su.ID] {
				return nil, duplicateSampleErr(su.ID)
			}
			seen[su.ID] = true
			if su.SeedLotID != "" {
				if err := scratch.AddEdge(su.SeedLotID, su.ID); err != nil {
					return nil, err
				}
			}
			if _, exists := t.Samples[su.ID]; exists {
				return nil, duplicateSampleErr(su.ID)
			}
		}
		for _, g := range in.Groups {
			if seen[g.ID] {
				return nil, duplicateSampleErr(g.ID)
			}
			seen[g.ID] = true
			if g.SampleID != "" {
				if err := scratch.AddEdge(g.SampleID, g.ID); err != nil {
					return nil, err
				}
			}
			if _, exists := t.Groups[g.ID]; exists {
				return nil, duplicateSampleErr(g.ID)
			}
		}
		for _, pl := range in.Plates {
			if seen[pl.ID] {
				return nil, duplicateSampleErr(pl.ID)
			}
			seen[pl.ID] = true
			if pl.GroupID != "" {
				if err := scratch.AddEdge(pl.GroupID, pl.ID); err != nil {
					return nil, err
				}
			}
			if _, exists := t.Plates[pl.ID]; exists {
				return nil, duplicateSampleErr(pl.ID)
			}
		}

		payload := allocatedPayload{
			SampleID:   in.SampleID,
			Generation: t.Trial.CurrentGen,
			SeedLots:   in.SeedLots,
			Samples:    in.Samples,
			Groups:     in.Groups,
			Plates:     in.Plates,
			Allocation: in.Allocation,
			At:         s.now(),
		}
		return []event{{trialID: trialID, typ: evSampleAllocated, payload: payload}}, nil
	})
}

func duplicateSampleErr(id string) error {
	return domain.New(domain.CodeDuplicateSampleID, "duplicate sample identity %q", id)
}

// LineageView is the canonical, sorted lineage and conservation report for a
// trial, returned by the lineage endpoint.
type LineageView struct {
	TrialID     string                `json:"trial_id"`
	SeedLots    []domain.SeedLot      `json:"seed_lots"`
	Samples     []domain.SampleUnit   `json:"samples"`
	Groups      []domain.CultureGroup `json:"groups"`
	Plates      []domain.Plate        `json:"plates"`
	Edges       []domain.LineageEvent `json:"edges"`
	Allocations []AllocationView      `json:"allocations"`
	Conserved   bool                  `json:"conserved"`
}

// AllocationView is one grain allocation with its conservation flag.
type AllocationView struct {
	SampleID   string             `json:"sample_id"`
	Allocation lineage.Allocation `json:"allocation"`
	Conserved  bool               `json:"conserved"`
}

// Lineage returns the canonical lineage and grain-conservation report for a
// trial, ordered by identity so the response is stable across restarts.
func (s *Service) Lineage(trialID string) (LineageView, error) {
	t, ok := s.readTrial(trialID)
	if !ok {
		return LineageView{}, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
	}

	view := LineageView{TrialID: trialID, Conserved: true}
	for _, sl := range t.SeedLots {
		view.SeedLots = append(view.SeedLots, sl)
	}
	for _, su := range t.Samples {
		view.Samples = append(view.Samples, su)
	}
	for _, g := range t.Groups {
		view.Groups = append(view.Groups, g)
	}
	for _, pl := range t.Plates {
		view.Plates = append(view.Plates, pl)
	}
	sort.Slice(view.SeedLots, func(i, j int) bool { return view.SeedLots[i].ID < view.SeedLots[j].ID })
	sort.Slice(view.Samples, func(i, j int) bool { return view.Samples[i].ID < view.Samples[j].ID })
	sort.Slice(view.Groups, func(i, j int) bool { return view.Groups[i].ID < view.Groups[j].ID })
	sort.Slice(view.Plates, func(i, j int) bool { return view.Plates[i].ID < view.Plates[j].ID })

	for child, parent := range t.Graph.Parents() {
		view.Edges = append(view.Edges, domain.LineageEvent{
			ParentID: parent, ChildID: child,
			Generation: t.Trial.CurrentGen,
		})
	}
	sort.Slice(view.Edges, func(i, j int) bool {
		if view.Edges[i].ParentID != view.Edges[j].ParentID {
			return view.Edges[i].ParentID < view.Edges[j].ParentID
		}
		return view.Edges[i].ChildID < view.Edges[j].ChildID
	})

	for id, a := range t.Allocations {
		view.Allocations = append(view.Allocations, AllocationView{
			SampleID: id, Allocation: a, Conserved: a.Total() == a.Source,
		})
		if a.Total() != a.Source {
			view.Conserved = false
		}
	}
	sort.Slice(view.Allocations, func(i, j int) bool {
		return view.Allocations[i].SampleID < view.Allocations[j].SampleID
	})
	return view, nil
}
