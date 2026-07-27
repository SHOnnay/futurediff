package quota

import "testing"

func TestDefaultValid(t *testing.T) {
	if err := Validate(Default()); err != nil {
		t.Fatal(err)
	}
	p := Default()
	p.MaxPatchBytes = 0
	if err := Validate(p); err == nil {
		t.Fatal("expected zero rejection")
	}
}
