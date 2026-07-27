package credentials

import "testing"

func FuzzDestinationRuleExactBoundary(f *testing.F) {
	rule, err := (DestinationRule{Scheme: "https", Host: "api.github.com", PathPrefix: "/repos"}).Normalize()
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{"https://api.github.com/repos/a/b", "https://api.github.com/repos-evil/a", "http://api.github.com/repos/a", "https://api.github.com.evil.test/repos/a", "https://127.0.0.1/repos/a"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		matched := rule.Matches(raw)
		if matched && raw != "https://api.github.com/repos" && len(raw) >= len("https://api.github.com/repos/") && raw[:len("https://api.github.com/repos/")] != "https://api.github.com/repos/" {
			t.Fatalf("unsafe destination matched: %q", raw)
		}
	})
}
