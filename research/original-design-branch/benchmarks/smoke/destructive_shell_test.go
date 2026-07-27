package smoke

import (
	"context"
	"testing"
)

func TestCompareDestructiveShellContainment(t *testing.T) {
	report, err := Runner{}.CompareDestructiveShellContainment(context.Background())
	if err != nil {
		t.Fatalf("compare destructive shell containment: %v", err)
	}
	if !report.DirectRepoDamaged {
		t.Fatal("expected direct baseline to damage the source repo")
	}
	if report.FutureDiffRepoDamaged {
		t.Fatal("expected futurediff path to contain destructive shell effects")
	}
	if report.FutureDiffState != "AWAITING_APPROVAL" {
		t.Fatalf("expected futurediff transaction to await approval, got %s", report.FutureDiffState)
	}
	if !report.StagedPatchContainsDelete {
		t.Fatal("expected staged patch to capture destructive deletion")
	}
}
