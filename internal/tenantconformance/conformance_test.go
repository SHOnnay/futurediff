package tenantconformance

import "testing"

func TestRun(t *testing.T) {
	r, err := Run(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Conformant || r.Failed != 0 || r.Passed < 13 {
		t.Fatalf("unexpected report: %+v", r)
	}
}
