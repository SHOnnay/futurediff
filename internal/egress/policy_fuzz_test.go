package egress

import (
	"net/http"
	"strings"
	"testing"
)

func FuzzValidateRequestNeverPermitsLookalikeHostOrPath(f *testing.F) {
	seeds := []string{"api.github.com", "api.github.com.evil.test", "API.GITHUB.COM", "127.0.0.1", "[::1]", "api.github.com:444"}
	for _, seed := range seeds {
		f.Add(seed, "/repos/owner/repo", "GET")
	}
	f.Add("api.github.com", "/repos-evil/owner/repo", "POST")
	policy := Policy{Rules: []Rule{{Host: "api.github.com", Port: "443", PathPrefixes: []string{"/repos"}, Methods: []string{"GET", "POST"}}}}
	transport, err := NewTransport(policy)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, host, path, method string) {
		req, err := http.NewRequest(method, "https://"+host+path, nil)
		if err != nil {
			return
		}
		allowed := transport.ValidateRequest(req) == nil
		if allowed && (!strings.EqualFold(req.URL.Hostname(), "api.github.com") || !(req.URL.EscapedPath() == "/repos" || len(req.URL.EscapedPath()) > 7 && req.URL.EscapedPath()[:7] == "/repos/")) {
			t.Fatalf("unexpected request allowed: %s", req.URL.String())
		}
	})
}
