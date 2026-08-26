// Package lineage implements batch lineage and grain aggregation: the
// append-only provenance graph over seed lots, sample units, culture groups
// and plates, together with integer grain-count conservation. Edges have a
// single parent and are never allowed to form a cycle.
package lineage

import (
	"seed-vault-viability-release/internal/domain"
)

// Allocation is the integer grain breakdown of a sample unit into its
// destinations. Every count is a non-negative integer and the destinations
// must sum exactly to the source count.
type Allocation struct {
	Source      int64 `json:"source"`      // seed grains in the source sample
	Culture     int64 `json:"culture"`     // assigned to culture groups
	Retain      int64 `json:"retain"`      // reserved for re-test
	Measurement int64 `json:"measurement"` // moisture determination
	Quarantine  int64 `json:"quarantine"`  // contamination isolation
	Loss        int64 `json:"loss"`        // justified loss
}

// Total returns the sum of all destinations.
func (a Allocation) Total() int64 {
	return a.Culture + a.Retain + a.Measurement + a.Quarantine + a.Loss
}

// ValidateAllocation enforces grain conservation: non-negative counts and
// exact equality between the source and the sum of destinations.
func ValidateAllocation(a Allocation) error {
	if a.Source < 0 || a.Culture < 0 || a.Retain < 0 || a.Measurement < 0 || a.Quarantine < 0 || a.Loss < 0 {
		return domain.New(domain.CodeInvalidSampleCount, "grain counts must be non-negative")
	}
	if a.Total() != a.Source {
		return domain.New(domain.CodeInvalidSampleCount,
			"allocation sums to %d but source is %d", a.Total(), a.Source)
	}
	return nil
}

// Graph is an append-only lineage graph. Each node may have at most one
// parent and the graph must remain acyclic.
type Graph struct {
	parents  map[string]string
	children map[string][]string
}

// NewGraph returns an empty lineage graph.
func NewGraph() *Graph {
	return &Graph{
		parents:  make(map[string]string),
		children: make(map[string][]string),
	}
}

// AddEdge appends a parent -> child edge. It rejects a child that already has
// a (different) parent with MULTIPLE_PARENT and rejects a would-be cycle with
// LINEAGE_CYCLE. A successful add leaves no partial state.
func (g *Graph) AddEdge(parent, child string) error {
	if parent == child {
		return domain.New(domain.CodeLineageCycle, "self-loop on %q", child)
	}
	if existing, ok := g.parents[child]; ok && existing != parent {
		return domain.New(domain.CodeMultipleParent,
			"node %q already has parent %q", child, existing)
	}
	// Detect a cycle: walking child -> ... must never reach parent.
	if g.reaches(child, parent) {
		return domain.New(domain.CodeLineageCycle, "edge %q -> %q would create a cycle", parent, child)
	}
	g.parents[child] = parent
	g.children[parent] = append(g.children[parent], child)
	return nil
}

// reaches reports whether there is a directed path from start to target.
func (g *Graph) reaches(start, target string) bool {
	if start == target {
		return true
	}
	seen := map[string]bool{}
	stack := []string{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n] {
			continue
		}
		seen[n] = true
		for _, c := range g.children[n] {
			if c == target {
				return true
			}
			stack = append(stack, c)
		}
	}
	return false
}

// Parents returns a copy of the parent map.
func (g *Graph) Parents() map[string]string {
	out := make(map[string]string, len(g.parents))
	for k, v := range g.parents {
		out[k] = v
	}
	return out
}

// NodeCount returns the number of distinct nodes recorded in the graph.
func (g *Graph) NodeCount() int {
	nodes := make(map[string]bool)
	for k, v := range g.parents {
		nodes[k] = true
		nodes[v] = true
	}
	for _, cs := range g.children {
		for _, c := range cs {
			nodes[c] = true
		}
	}
	return len(nodes)
}
