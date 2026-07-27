package smoke

import (
	"context"
	"testing"
)

func TestCompareMigrationFailure(t *testing.T) {
	report, err := CompareMigrationFailure(context.Background())
	if err != nil {
		t.Fatalf("compare migration failure: %v", err)
	}
	if !report.DirectRealDBChanged {
		t.Fatal("expected direct migration failure to leave the real DB changed")
	}
	if report.FutureDiffRealDBChanged {
		t.Fatal("expected futurediff preview failure to keep the real DB unchanged")
	}
	if !report.FutureDiffBlocked {
		t.Fatal("expected futurediff preview failure to block the flow")
	}
	if report.GitHubCalls != 0 {
		t.Fatalf("expected zero github calls, got %d", report.GitHubCalls)
	}
	if report.SlackCalls != 0 {
		t.Fatalf("expected zero slack calls, got %d", report.SlackCalls)
	}
}
