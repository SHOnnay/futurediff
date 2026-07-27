package outbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecoverAfterAmbiguousSendTimeout(t *testing.T) {
	var (
		mu       sync.Mutex
		messages []message
		sends    int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chat.postMessage":
			mu.Lock()
			sends++
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			messages = append(messages, message{
				Text:     payload["text"].(string),
				TS:       "1712345.100000",
				Metadata: payload["metadata"].(map[string]any),
			})
			mu.Unlock()
			time.Sleep(250 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(slackResponse{OK: true, Channel: payload["channel"].(string), TS: "1712345.100000"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/conversations.history":
			mu.Lock()
			defer mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(slackResponse{OK: true, Messages: messages})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, HTTPClient: &http.Client{Timeout: 100 * time.Millisecond}}
	prepared := client.Prepare(SendRequest{
		Channel:  "C123",
		Text:     "FutureDiff staged notification",
		EffectID: "eff_789",
	})

	if _, err := client.Send(context.Background(), prepared); err == nil {
		t.Fatal("expected timeout from ambiguous slack send")
	}

	receipt, err := client.Recover(context.Background(), prepared)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !receipt.Recovered {
		t.Fatal("expected recovered slack receipt")
	}
	if receipt.TS == "" {
		t.Fatal("expected recovered message timestamp")
	}

	mu.Lock()
	defer mu.Unlock()
	if sends != 1 {
		t.Fatalf("expected one send call, got %d", sends)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one stored message, got %d", len(messages))
	}
}

func TestPrepareMarksBestEffortSupport(t *testing.T) {
	client := Client{}
	prepared := client.Prepare(SendRequest{Channel: "C123", Text: "hello", EffectID: "eff_999"})
	if prepared.SupportLevel != SupportLevelIdempotentBestEffort {
		t.Fatalf("unexpected support level: %s", prepared.SupportLevel)
	}
	if prepared.Fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	metadata := prepared.Payload["metadata"].(map[string]any)
	eventPayload := metadata["event_payload"].(map[string]any)
	if eventPayload["effect_id"] != "eff_999" {
		t.Fatalf("expected effect marker in metadata")
	}
	if !strings.Contains(prepared.Payload["text"].(string), "hello") {
		t.Fatalf("unexpected text payload")
	}
}
