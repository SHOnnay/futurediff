package slo

import "testing"

func TestValidate(t *testing.T) {
	if err := Validate(Policy{Version: Version}); err != nil {
		t.Fatal(err)
	}
	if err := Validate(Policy{Version: Version, MaximumUnresolved: -1}); err == nil {
		t.Fatal("expected error")
	}
}
