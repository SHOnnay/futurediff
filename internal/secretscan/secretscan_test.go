package secretscan

import (
	"strings"
	"testing"
)

func TestScanOnlyAddedLinesAndRedacts(t *testing.T) {
	patch := "--- a/config\n+++ b/config\n-old=ghp_123456789012345678901234567890\n+token=ghp_abcdefghijklmnopqrstuvwxyz123456\n context=xoxb-111111111111-222222222222\n"
	report, err := Default().ScanPatch(strings.NewReader(patch))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Blocking || len(report.Findings) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if strings.Contains(report.Findings[0].Preview, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatal("secret leaked in preview")
	}
	if len(report.Findings[0].Fingerprint) != 64 {
		t.Fatal("missing fingerprint")
	}
}

func TestAllowFingerprint(t *testing.T) {
	secret := "ghp_abcdefghijklmnopqrstuvwxyz123456"
	policy := DefaultPolicy()
	policy.AllowedFingerprints = []string{fingerprint(secret)}
	scanner := &Scanner{Policy: policy, Rules: DefaultRules()}
	report, err := scanner.ScanPatch(strings.NewReader("+token=" + secret + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.Blocking {
		t.Fatalf("allowlist failed: %#v", report)
	}
}
