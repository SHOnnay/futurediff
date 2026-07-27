package outbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const SupportLevelIdempotentBestEffort = "idempotent_best_effort"

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
}

type SendRequest struct {
	Channel  string
	Text     string
	ThreadTS string
	EffectID string
}

type PreparedMessage struct {
	Request      SendRequest
	Fingerprint  string
	SupportLevel string
	Payload      map[string]any
}

type Receipt struct {
	ChannelID string
	TS        string
	Recovered bool
}

type slackResponse struct {
	OK       bool      `json:"ok"`
	Channel  string    `json:"channel"`
	TS       string    `json:"ts"`
	Error    string    `json:"error"`
	Messages []message `json:"messages,omitempty"`
}

type message struct {
	Text     string         `json:"text"`
	TS       string         `json:"ts"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (c Client) Prepare(req SendRequest) PreparedMessage {
	payload := map[string]any{
		"channel": req.Channel,
		"text":    req.Text,
		"metadata": map[string]any{
			"event_type": "futurediff_outbox",
			"event_payload": map[string]any{
				"effect_id": req.EffectID,
			},
		},
	}
	if strings.TrimSpace(req.ThreadTS) != "" {
		payload["thread_ts"] = req.ThreadTS
	}
	bytes, _ := json.Marshal(payload)
	fingerprint := sha256.Sum256(bytes)
	return PreparedMessage{
		Request:      req,
		Fingerprint:  hex.EncodeToString(fingerprint[:]),
		SupportLevel: SupportLevelIdempotentBestEffort,
		Payload:      payload,
	}
}

func (c Client) Send(ctx context.Context, prepared PreparedMessage) (*Receipt, error) {
	body, err := json.Marshal(prepared.Payload)
	if err != nil {
		return nil, fmt.Errorf("marshal slack payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL(), "/")+"/api/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build slack request: %w", err)
	}
	c.applyHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("send slack message: %w", err)
	}
	defer resp.Body.Close()
	var decoded slackResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode slack response: %w", err)
	}
	if !decoded.OK {
		return nil, fmt.Errorf("slack post failed: %s", decoded.Error)
	}
	return &Receipt{ChannelID: decoded.Channel, TS: decoded.TS}, nil
}

func (c Client) Recover(ctx context.Context, prepared PreparedMessage) (*Receipt, error) {
	query := url.Values{}
	query.Set("channel", prepared.Request.Channel)
	if strings.TrimSpace(prepared.Request.ThreadTS) != "" {
		query.Set("latest", prepared.Request.ThreadTS)
	}
	endpoint := strings.TrimRight(c.baseURL(), "/") + "/api/conversations.history?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build slack recover request: %w", err)
	}
	c.applyHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("recover slack message: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read slack recover response: %w", err)
	}
	var decoded slackResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode slack recover response: %w", err)
	}
	if !decoded.OK {
		return nil, fmt.Errorf("slack history failed: %s", decoded.Error)
	}
	for _, candidate := range decoded.Messages {
		if candidate.Text != prepared.Request.Text {
			continue
		}
		metadata := candidate.Metadata
		eventPayload, _ := metadata["event_payload"].(map[string]any)
		if effectID, _ := eventPayload["effect_id"].(string); effectID == prepared.Request.EffectID {
			return &Receipt{ChannelID: prepared.Request.Channel, TS: candidate.TS, Recovered: true}, nil
		}
	}
	return nil, fmt.Errorf("no matching slack message found during recovery")
}

func (c Client) baseURL() string {
	if strings.TrimSpace(c.BaseURL) == "" {
		return "https://slack.com"
	}
	return c.BaseURL
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c Client) applyHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}
