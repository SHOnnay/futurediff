package smoke

import (
	"context"
	"testing"
)

func TestCompareStaleGitHubBase(t *testing.T) {
	report, err := CompareStaleGitHubBase(context.Background())
	if err != nil {
		t.Fatalf("compare stale github base: %v", err)
	}
	if !report.DirectPRCreated {
		t.Fatal("expected direct path to create a pull request on stale base")
	}
	if !report.FutureDiffBlocked {
		t.Fatal("expected futurediff path to block on stale base")
	}
	if report.DirectCreateCalls != 1 {
		t.Fatalf("expected one direct create call, got %d", report.DirectCreateCalls)
	}
	if report.FutureDiffCreateCalls != 0 {
		t.Fatalf("expected zero futurediff create calls on stale base, got %d", report.FutureDiffCreateCalls)
	}
	if report.CurrentBaseSHA != "sha_current" {
		t.Fatalf("unexpected current base sha: %s", report.CurrentBaseSHA)
	}
}
