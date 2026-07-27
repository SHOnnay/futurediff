package releaseverify

import (
	"path/filepath"
	"strings"
	"testing"
)

func FuzzSafeReleaseName(f *testing.F) {
	for _, seed := range []string{"futurediff", "bin/futurediff", "../escape", "/absolute", "a/../../b", "a\\..\\b", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		allowed := safeName(name)
		clean := filepath.Clean(name)
		if allowed && (name == "" || filepath.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator))) {
			t.Fatalf("unsafe name allowed: %q", name)
		}
	})
}
