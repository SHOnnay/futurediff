package demo

import "testing"

func TestRun(t *testing.T) {
	report, err := Run(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !report.LiveCheckoutSafe || report.FinalStatus != "committed" || report.LiveValue == report.FutureValue {
		t.Fatalf("unexpected demo report: %+v", report)
	}
}
