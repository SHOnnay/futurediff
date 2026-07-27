package slackoutbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestPreparePostAndRecoverByMetadata(t *testing.T) {
	var posted map[string]any
	transport := roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			return response(401, `{"ok":false}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "chat.postMessage") {
			_ = json.NewDecoder(r.Body).Decode(&posted)
			return response(200, `{"ok":true,"channel":"C12345678","ts":"1700.1","message":{"client_msg_id":"ignored"}}`), nil
		}
		body, _ := json.Marshal(map[string]any{"ok": true, "messages": []any{map[string]any{"ts": "1700.1", "client_msg_id": posted["client_msg_id"], "metadata": posted["metadata"]}}})
		return response(200, string(body)), nil
	})
	a := &Adapter{HTTPClient: &http.Client{Transport: transport}}
	p, _, err := a.Prepare("eff_1", Input{Channel: "C12345678", Text: "Build passed"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.Post(context.Background(), p, []byte("secret"))
	if err != nil || r.Timestamp == "" {
		t.Fatalf("receipt=%#v err=%v", r, err)
	}
	if posted["client_msg_id"] == "eff_1" || !strings.Contains(posted["client_msg_id"].(string), "-") {
		t.Fatalf("payload=%#v", posted)
	}
	status, err := a.Status(context.Background(), p, []byte("secret"))
	if err != nil || status.Status != StatusCommitted || status.Receipt == nil {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}

func TestAmbiguousPostIsClassified(t *testing.T) {
	a := &Adapter{HTTPClient: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("reset") })}}
	p, _, _ := a.Prepare("eff_1", Input{Channel: "C12345678", Text: "Build passed"})
	_, err := a.Post(context.Background(), p, []byte("secret"))
	var pe *ProviderError
	if !errors.As(err, &pe) || !pe.Ambiguous {
		t.Fatalf("err=%v", err)
	}
}
