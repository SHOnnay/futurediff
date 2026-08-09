package githubdraft

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type fakeTransport struct {
	mu          sync.Mutex
	refs        map[string]string
	pulls       []pullResponse
	postCalls   int
	postMode    string
	token       string
	lastPayload createPayload
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.URL.Scheme != "https" || req.URL.Host != "api.github.com" {
		return nil, errors.New("unexpected destination")
	}
	if req.Header.Get("Authorization") != "Bearer "+f.token {
		return response(401, `{"message":"bad token"}`), nil
	}
	pathValue := req.URL.EscapedPath()
	switch {
	case req.Method == http.MethodGet && strings.Contains(pathValue, "/git/ref/heads/"):
		encoded := pathValue[strings.Index(pathValue, "/git/ref/heads/")+len("/git/ref/heads/"):]
		branch, _ := url.PathUnescape(encoded)
		sha := f.refs[branch]
		if sha == "" {
			return response(404, `{"message":"not found"}`), nil
		}
		return response(200, `{"object":{"sha":"`+sha+`"}}`), nil
	case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/pulls"):
		b, _ := json.Marshal(f.pulls)
		return response(200, string(b)), nil
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/pulls"):
		f.postCalls++
		body, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(body, &f.lastPayload); err != nil {
			return response(400, `{"message":"bad payload"}`), nil
		}
		if f.postMode == "reject" {
			return response(422, `{"message":"validation failed"}`), nil
		}
		p := pullResponse{Number: 431, NodeID: "PR_node_431", HTMLURL: "https://github.com/acme/app/pull/431", Title: f.lastPayload.Title, Body: f.lastPayload.Body, Draft: f.lastPayload.Draft}
		p.Head.Ref, p.Head.SHA = f.lastPayload.Head, f.refs[f.lastPayload.Head]
		p.Base.Ref, p.Base.SHA = f.lastPayload.Base, f.refs[f.lastPayload.Base]
		f.pulls = append(f.pulls, p)
		if f.postMode == "ambiguous" {
			return nil, errors.New("connection reset after request write")
		}
		b, _ := json.Marshal(p)
		return response(201, string(b)), nil
	default:
		return response(404, `{"message":"unexpected"}`), nil
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func adapterForFake(f *fakeTransport) *Adapter {
	return &Adapter{HTTPClient: &http.Client{Transport: f}}
}

func TestPrepareCreateAndStatus(t *testing.T) {
	f := &fakeTransport{refs: map[string]string{"feature/test": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "secret"}
	a := adapterForFake(f)
	input := Input{Owner: "acme", Repo: "app", Title: "Safe change", Body: "Prepared by FutureDiff", Head: "feature/test", Base: "main"}
	prepared, preview, err := a.Prepare(context.Background(), "eff_123", input, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Draft || !strings.Contains(prepared.RenderedBody, "futurediff-effect:eff_123") {
		t.Fatalf("unexpected preview/prepared: %#v %#v", preview, prepared)
	}
	status, err := a.Status(context.Background(), prepared, []byte("secret"))
	if err != nil || status.Status != StatusNotFound {
		t.Fatalf("status before create: %#v %v", status, err)
	}
	receipt, err := a.Create(context.Background(), prepared, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.PullNumber != 431 || f.postCalls != 1 || !f.lastPayload.Draft {
		t.Fatalf("unexpected create: %#v calls=%d payload=%#v", receipt, f.postCalls, f.lastPayload)
	}
	status, err = a.Status(context.Background(), prepared, []byte("secret"))
	if err != nil || status.Status != StatusCommitted || status.Receipt == nil {
		t.Fatalf("status after create: %#v %v", status, err)
	}
}

func TestAmbiguousCreateCanBeRecoveredWithoutSecondPost(t *testing.T) {
	f := &fakeTransport{refs: map[string]string{"feature/test": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "secret", postMode: "ambiguous"}
	a := adapterForFake(f)
	prepared, _, err := a.Prepare(context.Background(), "eff_ambiguous", Input{Owner: "acme", Repo: "app", Title: "Safe change", Head: "feature/test", Base: "main"}, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Create(context.Background(), prepared, []byte("secret"))
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || !providerErr.Ambiguous {
		t.Fatalf("expected ambiguous provider error, got %v", err)
	}
	status, err := a.Status(context.Background(), prepared, []byte("secret"))
	if err != nil || status.Status != StatusCommitted {
		t.Fatalf("recovery status: %#v %v", status, err)
	}
	if f.postCalls != 1 {
		t.Fatalf("expected one mutation call, got %d", f.postCalls)
	}
}

func TestFreshnessAndInputValidation(t *testing.T) {
	f := &fakeTransport{refs: map[string]string{"feature/test": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "secret"}
	a := adapterForFake(f)
	prepared, _, err := a.Prepare(context.Background(), "eff_stale", Input{Owner: "acme", Repo: "app", Title: "Safe change", Head: "feature/test", Base: "main"}, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	f.refs["main"] = strings.Repeat("c", 40)
	err = a.VerifyFresh(context.Background(), prepared, []byte("secret"))
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Class != "stale_resource_version" {
		t.Fatalf("expected stale version error, got %v", err)
	}
	bad := []string{"feature branch", "../main", "a..b", "a@{b", "refs/x.lock", "a~b"}
	for _, branch := range bad {
		if err := (Input{Owner: "acme", Repo: "app", Title: "x", Head: branch, Base: "main"}).Validate(); err == nil {
			t.Fatalf("expected unsafe branch %q to fail", branch)
		}
	}
}

func TestStatusIgnoresNonMatchingPullRequests(t *testing.T) {
	// Pre-existing PRs with different head/base/title must not be treated as
	// this effect's outcome; only an exact marker match counts.
	f := &fakeTransport{refs: map[string]string{"feature/test": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "secret"}
	f.pulls = []pullResponse{
		{Number: 100, Title: "Unrelated PR", Draft: true, Head: struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		}{Ref: "feature/other", SHA: strings.Repeat("d", 40)}, Base: struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		}{Ref: "main", SHA: strings.Repeat("b", 40)}},
		{Number: 101, Title: "FutureDiff change xyz", Draft: false, Head: struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		}{Ref: "feature/test", SHA: strings.Repeat("a", 40)}, Base: struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		}{Ref: "main", SHA: strings.Repeat("b", 40)}},
	}
	a := adapterForFake(f)
	prepared, _, err := a.Prepare(context.Background(), "eff_123", Input{Owner: "acme", Repo: "app", Title: "Safe change", Body: "Prepared by FutureDiff", Head: "feature/test", Base: "main"}, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	status, err := a.Status(context.Background(), prepared, []byte("secret"))
	if err != nil || status.Status != StatusNotFound {
		t.Fatalf("expected not_found despite unrelated PRs, got %#v err=%v", status, err)
	}
	receipt, err := a.Create(context.Background(), prepared, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.PullNumber != 431 || f.postCalls != 1 {
		t.Fatalf("create: %#v calls=%d", receipt, f.postCalls)
	}
}
