package threatmodel

import (
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	r, err := Run(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Secure || r.Failed != 0 || r.Passed < 7 || r.Digest == "" {
		t.Fatalf("report=%+v", r)
	}
}
