package smoke

import (
	"context"
	"testing"
)

func TestCompareDuplicateAPIRetry(t *testing.T) {
	report, err := CompareDuplicateAPIRetry(context.Background())
	if err != nil {
		t.Fatalf("compare duplicate api retry: %v", err)
	}
	if report.DirectPullCount < 2 {
		t.Fatalf("expected direct retry path to create duplicates, got %d", report.DirectPullCount)
	}
	if report.FutureDiffPullCount != 1 {
		t.Fatalf("expected futurediff path to resolve to one durable pull request, got %d", report.FutureDiffPullCount)
	}
	if !report.FutureDiffRecovered {
		t.Fatal("expected futurediff path to recover prior receipt")
	}
}
