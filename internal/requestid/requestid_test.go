package requestid

import "testing"

func TestNormalizeAndNew(t *testing.T) {
	if _, ok := Normalize("short"); ok {
		t.Fatal("short accepted")
	}
	id := New()
	if _, ok := Normalize(id); !ok {
		t.Fatalf("invalid generated id %q", id)
	}
}
