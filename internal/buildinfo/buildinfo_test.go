package buildinfo

import "testing"

func TestCurrent(t *testing.T) {
	got := Current()
	if got.Version == "" || got.GoVersion == "" || got.Platform == "" {
		t.Fatalf("incomplete build info: %+v", got)
	}
}
