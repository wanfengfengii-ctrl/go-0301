// Package review implements the review and terminal arbitration component: it
// verifies reviewer independence and qualification snapshots, then competes a
// single terminal credential through a single-writer barrier.
package review

import (
	"sort"
	"strconv"

	"seed-vault-viability-release/internal/domain"
)

// ValidateReviews checks that the given reviews satisfy the qualification
// rules: at least the required number of distinct, qualified reviewers.
// It returns a *domain.Error on the first violation.
func ValidateReviews(reviews []domain.Review, rules domain.QualificationRules) error {
	if len(reviews) < rules.MinDistinctReviewers {
		return domain.New(domain.CodeMissingControl,
			"need %d reviewers, got %d", rules.MinDistinctReviewers, len(reviews))
	}
	seen := make(map[string]bool)
	for _, r := range reviews {
		if r.ReviewerID == "" {
			return domain.New(domain.CodeMissingControl, "reviewer id is empty")
		}
		if seen[r.ReviewerID] {
			return domain.New(domain.CodeMissingControl, "reviewer %q signed more than once", r.ReviewerID)
		}
		if !qualified(r.Qualification, rules.RequiredRoles) {
			return domain.New(domain.CodeMissingControl,
				"reviewer %q lacks required qualification %q", r.ReviewerID, r.Qualification)
		}
		seen[r.ReviewerID] = true
	}
	return nil
}

func qualified(q string, roles []string) bool {
	for _, r := range roles {
		if q == r {
			return true
		}
	}
	return false
}

// SortedReviewerIDs returns the reviewer ids in canonical order for stable
// credential digests.
func SortedReviewerIDs(reviews []domain.Review) []string {
	ids := make([]string, 0, len(reviews))
	for _, r := range reviews {
		ids = append(ids, r.ReviewerID)
	}
	sort.Strings(ids)
	return ids
}

// TerminalArbiter is the single-writer barrier that produces exactly one
// terminal credential per trial.
type TerminalArbiter interface {
	// Decide validates the reviews and competes for the terminal slot. It
	// returns the winning credential or TERMINAL_ALREADY_DECIDED.
	Decide(trialID string, t domain.TerminalType, reviews []domain.Review, at int64) (domain.TerminalCredential, error)
}

// Slot is the in-memory single-writer implementation of TerminalArbiter. The
// first successful Decide binds the slot; every later Decide returns
// TERMINAL_ALREADY_DECIDED with the existing credential.
type Slot struct {
	decided map[string]domain.TerminalCredential
}

// NewSlot returns an empty terminal slot store.
func NewSlot() *Slot {
	return &Slot{decided: make(map[string]domain.TerminalCredential)}
}

// Decide validates and then competes for the terminal slot.
func (s *Slot) Decide(trialID string, t domain.TerminalType, reviews []domain.Review, at int64) (domain.TerminalCredential, error) {
	if existing, ok := s.decided[trialID]; ok {
		return domain.TerminalCredential{}, domain.New(domain.CodeTerminalAlreadyDecided,
			"trial %q already decided as %q", trialID, existing.Type).
			WithDetails(string(existing.Type))
	}
	if err := ValidateReviews(reviews, domain.QualificationRules{
		MinDistinctReviewers: 2,
		RequiredRoles:        []string{"qualified"},
	}); err != nil {
		return domain.TerminalCredential{}, err
	}
	cred := domain.TerminalCredential{
		ID:          terminalCredentialID(trialID, t),
		TrialID:     trialID,
		Type:        t,
		Number:      terminalNumber(trialID, t, at),
		SubmittedAt: at,
		Reviews:     SortedReviewerIDs(reviews),
	}
	s.decided[trialID] = cred
	return cred, nil
}

// Credential returns the decided credential for a trial, if any.
func (s *Slot) Credential(trialID string) (domain.TerminalCredential, bool) {
	c, ok := s.decided[trialID]
	return c, ok
}

func terminalCredentialID(trialID string, t domain.TerminalType) string {
	return trialID + "-" + string(t)
}

func terminalNumber(trialID string, t domain.TerminalType, at int64) string {
	return trialID + "/" + string(t) + "/" + strconv.FormatInt(at, 10)
}
