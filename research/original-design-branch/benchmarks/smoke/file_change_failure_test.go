package smoke

import (
	"context"
	"testing"
)

func TestCompareFileChangeFailure(t *testing.T) {
	report, err := Runner{}.CompareFileChangeFailure(context.Background())
	if err != nil {
		t.Fatalf("compare file change failure: %v", err)
	}
	if !report.DirectRepoChanged {
		t.Fatal("expected direct baseline to leave repo changed after failure")
	}
	if report.FutureDiffRepoChanged {
		t.Fatal("expected futurediff path to keep repo unchanged after failed verification")
	}
	if report.FutureDiffState != "ABORTED" {
		t.Fatalf("expected futurediff transaction to abort, got %s", report.FutureDiffState)
	}
	if report.DirectDuration <= 0 {
		t.Fatal("expected direct duration to be recorded")
	}
	if report.FutureDiffDuration <= 0 {
		t.Fatal("expected futurediff duration to be recorded")
	}
}
