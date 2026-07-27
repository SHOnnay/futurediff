// Package slackoutbox implements a durable prepared-message effect for Slack.
// The exact channel, text, client message id, and FutureDiff metadata are bound
// into approval before chat.postMessage is released.
package slackoutbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

const (
	AdapterID       = "builtin.slack.message-outbox"
	AdapterVersion  = "0.1.0"
	ToolIdentity    = "slack.post_message"
	StatusOperation = "slack.query_channel_history"
	CommitOperation = "slack.post_message"
	SupportLevel    = "durable_outbox_with_metadata_reconciliation"
)

var channelID = regexp.MustCompile(`^[A-Z][A-Z0-9]{8,30}$`)

type Input struct {
	Channel   string   `json:"channel"`
	Text      string   `json:"text"`
	DependsOn []string `json:"depends_on,omitempty"`
}

type metadata struct {
	EventType    string            `json:"event_type"`
	EventPayload map[string]string `json:"event_payload"`
}

type postPayload struct {
	Channel     string   `json:"channel"`
	Text        string   `json:"text"`
	ClientMsgID string   `json:"client_msg_id"`
	Metadata    metadata `json:"metadata"`
}

type Prepared struct {
	Input         Input       `json:"input"`
	Payload       postPayload `json:"payload"`
	RequestDigest string      `json:"request_digest"`
}

type Preview struct {
	Provider    string `json:"provider"`
	Channel     string `json:"channel"`
	Text        string `json:"text"`
	ClientMsgID string `json:"client_msg_id"`
	MetadataKey string `json:"metadata_key"`
}

type Receipt struct {
	Channel    string    `json:"channel"`
	Timestamp  string    `json:"timestamp"`
	MessageID  string    `json:"message_id,omitempty"`
	Recovered  bool      `json:"recovered"`
	ObservedAt time.Time `json:"observed_at"`
}

type Status string

const (
	StatusNotFound  Status = "not_found"
	StatusCommitted Status = "committed"
)

type StatusResult struct {
	Status  Status   `json:"status"`
	Receipt *Receipt `json:"receipt,omitempty"`
}

type ProviderError struct {
	Ambiguous  bool
	StatusCode int
	Class      string
	Err        error
}

func (e *ProviderError) Error() string {
	if e == nil || e.Err == nil {
		return "slack provider error"
	}
	return e.Err.Error()
}
func (e *ProviderError) Unwrap() error { return e.Err }

type Adapter struct {
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}

func (a *Adapter) ID() string      { return AdapterID }
func (a *Adapter) Version() string { return AdapterVersion }

func (i Input) Normalize() (Input, error) {
	i.Channel = strings.ToUpper(strings.TrimSpace(i.Channel))
	i.Text = strings.TrimSpace(i.Text)
	if !channelID.MatchString(i.Channel) {
		return Input{}, errors.New("Slack channel must be a channel ID, not a name")
	}
	if i.Text == "" || len(i.Text) > 40000 {
		return Input{}, errors.New("Slack text must be 1-40000 characters")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(i.DependsOn))
	for _, d := range i.DependsOn {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			return Input{}, errors.New("Slack dependencies must be unique and non-empty")
		}
		seen[d] = true
		out = append(out, d)
	}
	i.DependsOn = out
	return i, nil
}
func (a *Adapter) CommitDestination() (string, error) { return a.endpoint("chat.postMessage") }
func (a *Adapter) StatusDestination() (string, error) { return a.endpoint("conversations.history") }

func (a *Adapter) Prepare(effectID string, input Input) (Prepared, Preview, error) {
	if strings.TrimSpace(effectID) == "" {
		return Prepared{}, Preview{}, errors.New("effect id is required")
	}
	n, err := input.Normalize()
	if err != nil {
		return Prepared{}, Preview{}, err
	}
	payload := postPayload{Channel: n.Channel, Text: n.Text, ClientMsgID: clientMessageID(effectID), Metadata: metadata{EventType: "futurediff_effect", EventPayload: map[string]string{"effect_id": effectID}}}
	digest, err := domain.Digest(payload)
	if err != nil {
		return Prepared{}, Preview{}, err
	}
	return Prepared{Input: n, Payload: payload, RequestDigest: digest}, Preview{Provider: "slack", Channel: n.Channel, Text: n.Text, ClientMsgID: payload.ClientMsgID, MetadataKey: "futurediff_effect:" + effectID}, nil
}

func (a *Adapter) Post(ctx context.Context, p Prepared, token []byte) (Receipt, error) {
	body, err := json.Marshal(p.Payload)
	if err != nil {
		return Receipt{}, err
	}
	endpoint, err := a.CommitDestination()
	if err != nil {
		return Receipt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Receipt{}, err
	}
	a.headers(req, token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := a.client().Do(req)
	if err != nil {
		return Receipt{}, &ProviderError{Ambiguous: true, Class: "transport_unknown", Err: fmt.Errorf("Slack message outcome is unknown: %w", err)}
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return Receipt{}, &ProviderError{Ambiguous: true, StatusCode: resp.StatusCode, Class: "response_read_unknown", Err: readErr}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Receipt{}, &ProviderError{StatusCode: resp.StatusCode, Class: "provider_rejected", Err: fmt.Errorf("Slack HTTP %d: %s", resp.StatusCode, safe(raw))}
	}
	var out struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Channel string `json:"channel"`
		TS      string `json:"ts"`
		Message struct {
			ClientMsgID string `json:"client_msg_id"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Receipt{}, &ProviderError{Ambiguous: true, StatusCode: resp.StatusCode, Class: "response_decode_unknown", Err: err}
	}
	if !out.OK {
		return Receipt{}, &ProviderError{StatusCode: resp.StatusCode, Class: "provider_rejected", Err: fmt.Errorf("Slack rejected message: %s", out.Error)}
	}
	if out.Channel == "" || out.TS == "" {
		return Receipt{}, &ProviderError{Ambiguous: true, StatusCode: resp.StatusCode, Class: "incomplete_receipt_unknown", Err: errors.New("Slack returned incomplete receipt")}
	}
	return Receipt{Channel: out.Channel, Timestamp: out.TS, MessageID: out.Message.ClientMsgID, ObservedAt: a.now()}, nil
}

func (a *Adapter) Status(ctx context.Context, p Prepared, token []byte) (StatusResult, error) {
	base, err := a.StatusDestination()
	if err != nil {
		return StatusResult{}, err
	}
	q := url.Values{}
	q.Set("channel", p.Input.Channel)
	q.Set("limit", "100")
	q.Set("include_all_metadata", "true")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return StatusResult{}, err
	}
	a.headers(req, token)
	resp, err := a.client().Do(req)
	if err != nil {
		return StatusResult{}, &ProviderError{Ambiguous: true, Class: "status_transport_error", Err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return StatusResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return StatusResult{}, &ProviderError{StatusCode: resp.StatusCode, Class: "status_rejected", Err: fmt.Errorf("Slack history HTTP %d: %s", resp.StatusCode, safe(raw))}
	}
	var out struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		Messages []struct {
			TS          string   `json:"ts"`
			ClientMsgID string   `json:"client_msg_id"`
			Metadata    metadata `json:"metadata"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return StatusResult{}, err
	}
	if !out.OK {
		return StatusResult{}, &ProviderError{Class: "status_rejected", Err: fmt.Errorf("Slack history rejected: %s", out.Error)}
	}
	for _, m := range out.Messages {
		if m.ClientMsgID == p.Payload.ClientMsgID || m.Metadata.EventType == "futurediff_effect" && m.Metadata.EventPayload["effect_id"] == p.Payload.Metadata.EventPayload["effect_id"] {
			r := Receipt{Channel: p.Input.Channel, Timestamp: m.TS, MessageID: m.ClientMsgID, Recovered: true, ObservedAt: a.now()}
			return StatusResult{Status: StatusCommitted, Receipt: &r}, nil
		}
	}
	return StatusResult{Status: StatusNotFound}, nil
}

func (a *Adapter) endpoint(method string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(a.BaseURL), "/")
	if base == "" {
		base = "https://slack.com/api"
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Slack API base must be a credential-free HTTPS URL")
	}
	return base + "/" + method, nil
}
func (a *Adapter) headers(req *http.Request, token []byte) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "FutureDiff/0.12")
	if len(token) > 0 {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
}
func (a *Adapter) client() *http.Client {
	if a.HTTPClient != nil {
		c := *a.HTTPClient
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		if c.Timeout == 0 {
			c.Timeout = 30 * time.Second
		}
		return &c
	}
	tr := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 30 * time.Second}
	return &http.Client{Transport: tr, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func (a *Adapter) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}
func clientMessageID(effectID string) string {
	sum := sha256.Sum256([]byte(effectID))
	h := fmt.Sprintf("%x", sum[:16])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
func safe(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 1024 {
		s = s[:1024] + "…"
	}
	return s
}
