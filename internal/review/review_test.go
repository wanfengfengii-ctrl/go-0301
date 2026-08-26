package review_test

import (
	"testing"

	"seed-vault-viability-release/internal/domain"
	"seed-vault-viability-release/internal/review"
)

func TestValidateReviewsDistinctQualified(t *testing.T) {
	reviews := []domain.Review{
		{ReviewerID: "r1", Qualification: "qualified"},
		{ReviewerID: "r2", Qualification: "qualified"},
	}
	rules := domain.QualificationRules{MinDistinctReviewers: 2, RequiredRoles: []string{"qualified"}}
	if err := review.ValidateReviews(reviews, rules); err != nil {
		t.Fatalf("valid reviews rejected: %v", err)
	}
}

func TestValidateReviewsDuplicate(t *testing.T) {
	reviews := []domain.Review{
		{ReviewerID: "r1", Qualification: "qualified"},
		{ReviewerID: "r1", Qualification: "qualified"},
	}
	rules := domain.QualificationRules{MinDistinctReviewers: 2, RequiredRoles: []string{"qualified"}}
	if err := review.ValidateReviews(reviews, rules); !domain.IsCode(err, domain.CodeMissingControl) {
		t.Fatalf("got %v, want MISSING_CONTROL", err)
	}
}

func TestValidateReviewsUnqualified(t *testing.T) {
	reviews := []domain.Review{
		{ReviewerID: "r1", Qualification: "qualified"},
		{ReviewerID: "r2", Qualification: "trainee"},
	}
	rules := domain.QualificationRules{MinDistinctReviewers: 2, RequiredRoles: []string{"qualified"}}
	if err := review.ValidateReviews(reviews, rules); !domain.IsCode(err, domain.CodeMissingControl) {
		t.Fatalf("got %v, want MISSING_CONTROL", err)
	}
}

func TestTerminalSingleWinner(t *testing.T) {
	s := review.NewSlot()
	reviews := []domain.Review{
		{ReviewerID: "r1", Qualification: "qualified"},
		{ReviewerID: "r2", Qualification: "qualified"},
	}
	cred, err := s.Decide("trial-1", domain.TerminalRelease, reviews, 100)
	if err != nil {
		t.Fatalf("first decide: %v", err)
	}
	if cred.Type != domain.TerminalRelease {
		t.Fatalf("type = %q, want release", cred.Type)
	}
	// a second terminal request for the same trial must lose
	if _, err := s.Decide("trial-1", domain.TerminalVoid, reviews, 101); !domain.IsCode(err, domain.CodeTerminalAlreadyDecided) {
		t.Fatalf("got %v, want TERMINAL_ALREADY_DECIDED", err)
	}
	if got, ok := s.Credential("trial-1"); !ok || got.Type != domain.TerminalRelease {
		t.Fatalf("credential not preserved as release")
	}
}
