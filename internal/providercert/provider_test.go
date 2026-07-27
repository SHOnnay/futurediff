package providercert

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/SHOnnay/futurediff/internal/egress"
)

func TestConfirmationRequired(t *testing.T) {
	_, err := Run(context.Background(), Options{Targets: []string{"github"}}, Dependencies{})
	if err == nil {
		t.Fatal("confirmation not required")
	}
}
func TestFakeGitHubAndSlackMutationCertification(t *testing.T) {
	var mu sync.Mutex
	branchDeleted := false
	prClosed := false
	slackDeleted := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		p := r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch {
		case p == "/repos/o/r" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{"default_branch": "main", "full_name": "o/r"})
		case p == "/repos/o/r/git/ref/heads/main":
			json.NewEncoder(w).Encode(map[string]any{"object": map[string]any{"sha": "base"}})
		case p == "/repos/o/r/git/commits/base":
			json.NewEncoder(w).Encode(map[string]any{"tree": map[string]any{"sha": "tree"}})
		case p == "/repos/o/r/git/commits" && r.Method == "POST":
			json.NewEncoder(w).Encode(map[string]any{"sha": "newcommit"})
		case p == "/repos/o/r/git/refs" && r.Method == "POST":
			json.NewEncoder(w).Encode(map[string]any{"ref": "ok"})
		case p == "/repos/o/r/pulls" && r.Method == "POST":
			json.NewEncoder(w).Encode(map[string]any{"number": 1, "html_url": "https://example/pr/1", "draft": true})
		case p == "/repos/o/r/pulls/1" && r.Method == "PATCH":
			prClosed = true
			json.NewEncoder(w).Encode(map[string]any{"state": "closed"})
		case strings.HasPrefix(p, "/repos/o/r/git/refs/heads/") && r.Method == "DELETE":
			branchDeleted = true
			w.WriteHeader(http.StatusNoContent)
		case p == "/api/chat.postMessage":
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "ts": "1.2", "channel": "C1"})
		case p == "/api/chat.delete":
			slackDeleted = true
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()
	os.Setenv("GH_TEST_TOKEN", "secret")
	os.Setenv("SL_TEST_TOKEN", "secret")
	defer os.Unsetenv("GH_TEST_TOKEN")
	defer os.Unsetenv("SL_TEST_TOKEN")
	factory := func(_ egress.Policy) (*http.Client, error) {
		c := srv.Client()
		c.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		return c, nil
	}
	r, err := Run(context.Background(), Options{Targets: []string{"github", "slack"}, Confirmation: ConfirmationPhrase, Nonce: "abc", GitHubOwner: "o", GitHubRepo: "r", GitHubTokenEnv: "GH_TEST_TOKEN", GitHubAPIBase: srv.URL, SlackChannel: "C1", SlackTokenEnv: "SL_TEST_TOKEN", SlackAPIBase: srv.URL + "/api"}, Dependencies{HTTPClientFactory: factory, SkipPolicyValidationForTests: true})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Certified {
		t.Fatalf("not certified: %#v", r)
	}
	if !branchDeleted || !prClosed || !slackDeleted {
		t.Fatalf("cleanup incomplete branch=%t pr=%t slack=%t", branchDeleted, prClosed, slackDeleted)
	}
}
