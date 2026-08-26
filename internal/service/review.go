package service

import (
	"strconv"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/review"
)

// ReviewInput is the command payload for one reviewer's independent signature.
type ReviewInput struct {
	ReviewerID    string
	Qualification string
	Digest        string
}

// SubmitReview records one independent reviewer signature after checking the
// reviewer holds a role required by the locked snapshot.
func (s *Service) SubmitReview(trialID string, in ReviewInput) error {
	return s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		if t.Snapshot == nil {
			return nil, domain.New(domain.CodeStageGap, "trial %q is not locked", trialID)
		}
		qualified := false
		for _, role := range t.Snapshot.Qualification.RequiredRoles {
			if in.Qualification == role {
				qualified = true
				break
			}
		}
		if !qualified {
			return nil, domain.New(domain.CodeMissingControl,
				"reviewer %q lacks required qualification %q", in.ReviewerID, in.Qualification)
		}
		r := domain.Review{
			ID:            reviewID(trialID, in.ReviewerID),
			TrialID:       trialID,
			ReviewerID:    in.ReviewerID,
			Qualification: in.Qualification,
			Digest:        in.Digest,
			SubmittedAt:   s.now(),
		}
		return []event{{trialID: trialID, typ: evReviewSubmitted,
			payload: reviewSubmittedPayload{Review: r}}}, nil
	})
}

// TerminalInput is the command payload for the single-writer terminal decision.
type TerminalInput struct {
	Type domain.TerminalType
}

// DecideTerminal validates the readiness preconditions (grain conservation and
// two distinct qualified reviewers) and then competes for the single terminal
// slot. The first valid decision wins; every later request returns
// TERMINAL_ALREADY_DECIDED with the existing outcome. The three terminal types
// share the same slot.
func (s *Service) DecideTerminal(trialID string, in TerminalInput) (domain.TerminalCredential, error) {
	var out domain.TerminalCredential
	err := s.mutate(func() ([]event, error) {
		t, ok := s.trials[trialID]
		if !ok {
			return nil, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
		}
		if t.Credential != nil {
			return nil, domain.New(domain.CodeTerminalAlreadyDecided,
				"trial %q already decided as %q", trialID, t.Credential.Type).
				WithDetails(string(t.Credential.Type))
		}
		if t.Snapshot == nil {
			return nil, domain.New(domain.CodeStageGap, "trial %q is not locked", trialID)
		}

		// Grain conservation must hold before any final decision.
		for id, a := range t.Allocations {
			if a.Total() != a.Source {
				return nil, domain.New(domain.CodeInvalidSampleCount,
					"sample %q allocation not conserved (%d != %d)", id, a.Total(), a.Source)
			}
		}

		reviews := make([]domain.Review, 0, len(t.Reviews))
		for _, r := range t.Reviews {
			reviews = append(reviews, r)
		}
		if err := review.ValidateReviews(reviews, t.Snapshot.Qualification); err != nil {
			return nil, err
		}

		cred := domain.TerminalCredential{
			ID:          trialID + "-" + string(in.Type),
			TrialID:     trialID,
			Type:        in.Type,
			Number:      terminalNumber(trialID, in.Type, s.now()),
			SubmittedAt: s.now(),
			Reviews:     review.SortedReviewerIDs(reviews),
		}
		out = cred
		return []event{{trialID: trialID, typ: evTerminalDecided,
			payload: terminalDecidedPayload{Credential: cred}}}, nil
	})
	return out, err
}

// GetCredential returns the permanent terminal credential for a trial, if any.
func (s *Service) GetCredential(trialID string) (domain.TerminalCredential, error) {
	t, ok := s.readTrial(trialID)
	if !ok {
		return domain.TerminalCredential{}, domain.New(domain.CodeInvalidSampleCount, "trial %q not found", trialID)
	}
	if t.Credential == nil {
		return domain.TerminalCredential{}, domain.New(domain.CodeInvalidSampleCount,
			"trial %q has no terminal credential", trialID)
	}
	return *t.Credential, nil
}

func reviewID(trialID, reviewerID string) string {
	return trialID + "-review-" + reviewerID
}

func terminalNumber(trialID string, t domain.TerminalType, at int64) string {
	return trialID + "/" + string(t) + "/" + strconv.FormatInt(at, 10)
}
