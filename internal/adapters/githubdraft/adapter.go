// Package githubdraft implements the first built-in FutureDiff external
// adapter: preparation and effectively-once creation of a GitHub draft pull
// request. It never owns or persists credential material.
package githubdraft

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

const (
	AdapterID       = "builtin.github.draft-pull-request"
	AdapterVersion  = "0.1.0"
	ToolIdentity    = "github.create_draft_pull_request"
	ReadOperation   = "github.read_refs"
	StatusOperation = "github.query_pull_requests"
	CommitOperation = "github.create_draft_pull_request"
	SupportLevel    = "exact_payload_with_ref_freshness_check"
)

var repoPart = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

type Input struct {
	Owner             string `json:"owner"`
	Repo              string `json:"repo"`
	Title             string `json:"title"`
	Body              string `json:"body,omitempty"`
	Head              string `json:"head"`
	Base              string `json:"base"`
	ExpectedHeadSHA   string `json:"expected_head_sha,omitempty"`
	DependsOnEffectID string `json:"depends_on_effect_id,omitempty"`
}

type Prepared struct {
	Input            Input             `json:"input"`
	EffectMarker     string            `json:"effect_marker"`
	RenderedBody     string            `json:"rendered_body"`
	ExpectedHeadSHA  string            `json:"expected_head_sha"`
	ExpectedBaseSHA  string            `json:"expected_base_sha"`
	ResourceVersions map[string]string `json:"resource_versions"`
	RequestDigest    string            `json:"request_digest"`
}

type Preview struct {
	Provider              string `json:"provider"`
	Repository            string `json:"repository"`
	Title                 string `json:"title"`
	Body                  string `json:"body"`
	Head                  string `json:"head"`
	Base                  string `json:"base"`
	ExpectedHeadSHA       string `json:"expected_head_sha"`
	ExpectedBaseSHA       string `json:"expected_base_sha"`
	Draft                 bool   `json:"draft"`
	SupportLevel          string `json:"support_level"`
	LocalPatchPublished   bool   `json:"local_patch_published"`
	HeadBindingLimitation string `json:"head_binding_limitation"`
}

type Receipt struct {
	PullNumber int       `json:"pull_number"`
	NodeID     string    `json:"node_id,omitempty"`
	HTMLURL    string    `json:"html_url"`
	HeadSHA    string    `json:"head_sha,omitempty"`
	BaseSHA    string    `json:"base_sha,omitempty"`
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
		return "github provider error"
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

func (a *Adapter) Destination(input Input) (string, error) {
	normalized, err := input.Normalize()
	if err != nil {
		return "", err
	}
	return a.endpoint("repos", normalized.Owner, normalized.Repo, "pulls"), nil
}

func (a *Adapter) ReadDestination(input Input) (string, error) {
	normalized, err := input.Normalize()
	if err != nil {
		return "", err
	}
	return a.endpoint("repos", normalized.Owner, normalized.Repo, "git", "ref", "heads"), nil
}

func (i Input) Normalize() (Input, error) {
	i.Owner = strings.TrimSpace(i.Owner)
	i.Repo = strings.TrimSpace(i.Repo)
	i.Title = strings.TrimSpace(i.Title)
	i.Head = strings.TrimSpace(i.Head)
	i.Base = strings.TrimSpace(i.Base)
	i.ExpectedHeadSHA = strings.ToLower(strings.TrimSpace(i.ExpectedHeadSHA))
	i.DependsOnEffectID = strings.TrimSpace(i.DependsOnEffectID)
	if !repoPart.MatchString(i.Owner) || !repoPart.MatchString(i.Repo) {
		return Input{}, errors.New("github owner and repository must use safe GitHub name characters")
	}
	if i.Title == "" || len(i.Title) > 256 {
		return Input{}, errors.New("pull-request title must be 1-256 characters")
	}
	if i.Head == "" || i.Base == "" || len(i.Head) > 255 || len(i.Base) > 255 {
		return Input{}, errors.New("head and base branches are required and must be at most 255 characters")
	}
	for _, branch := range []string{i.Head, i.Base} {
		if !safeBranch(branch) {
			return Input{}, fmt.Errorf("unsafe Git branch name %q", branch)
		}
	}
	if i.ExpectedHeadSHA != "" && !isFullSHA(i.ExpectedHeadSHA) {
		return Input{}, errors.New("expected_head_sha must be a full Git object id")
	}
	if (i.ExpectedHeadSHA == "") != (i.DependsOnEffectID == "") {
		return Input{}, errors.New("expected_head_sha and depends_on_effect_id must be provided together")
	}
	if len(i.Body) > 64*1024 {
		return Input{}, errors.New("pull-request body exceeds 64 KiB")
	}
	return i, nil
}

func (i Input) Validate() error {
	_, err := i.Normalize()
	return err
}
func (a *Adapter) Prepare(ctx context.Context, effectID string, input Input, token []byte) (Prepared, Preview, error) {
	if strings.TrimSpace(effectID) == "" {
		return Prepared{}, Preview{}, errors.New("effect id is required")
	}
	normalized, err := input.Normalize()
	if err != nil {
		return Prepared{}, Preview{}, err
	}
	input = normalized
	headSHA := input.ExpectedHeadSHA
	if headSHA == "" {
		headSHA, err = a.readRef(ctx, input, input.Head, token)
		if err != nil {
			return Prepared{}, Preview{}, err
		}
	}
	baseSHA, err := a.readRef(ctx, input, input.Base, token)
	if err != nil {
		return Prepared{}, Preview{}, err
	}
	marker := "<!-- futurediff-effect:" + effectID + " -->"
	body := strings.TrimSpace(input.Body)
	if body == "" {
		body = marker
	} else if !strings.Contains(body, marker) {
		body += "\n\n" + marker
	}
	payload := createPayload{Title: input.Title, Head: input.Head, Base: input.Base, Body: body, Draft: true}
	requestDigest, err := domain.Digest(payload)
	if err != nil {
		return Prepared{}, Preview{}, err
	}
	resources := map[string]string{
		fmt.Sprintf("github://%s/%s/refs/heads/%s", input.Owner, input.Repo, input.Head): headSHA,
		fmt.Sprintf("github://%s/%s/refs/heads/%s", input.Owner, input.Repo, input.Base): baseSHA,
	}
	prepared := Prepared{Input: input, EffectMarker: marker, RenderedBody: body, ExpectedHeadSHA: headSHA, ExpectedBaseSHA: baseSHA, ResourceVersions: resources, RequestDigest: requestDigest}
	publishedByDependency := input.DependsOnEffectID != ""
	limitation := "existing remote head is pinned"
	if publishedByDependency {
		limitation = "head is bound to the exact commit published by the declared dependency"
	}
	preview := Preview{Provider: "github", Repository: input.Owner + "/" + input.Repo, Title: input.Title, Body: body, Head: input.Head, Base: input.Base, ExpectedHeadSHA: headSHA, ExpectedBaseSHA: baseSHA, Draft: true, SupportLevel: SupportLevel, LocalPatchPublished: publishedByDependency, HeadBindingLimitation: limitation}
	return prepared, preview, nil
}

// VerifyPreCommit permits a dependency-bound head to remain absent before the
// branch-publication effect commits. If it already exists, it must already
// equal the approved commit. The base branch is always required and pinned.
func (a *Adapter) VerifyPreCommit(ctx context.Context, prepared Prepared, token []byte) error {
	baseSHA, err := a.readRef(ctx, prepared.Input, prepared.Input.Base, token)
	if err != nil {
		return err
	}
	if baseSHA != prepared.ExpectedBaseSHA {
		return &ProviderError{Class: "stale_resource_version", Err: fmt.Errorf("github base ref changed: %s->%s", prepared.ExpectedBaseSHA, baseSHA)}
	}
	if prepared.Input.DependsOnEffectID == "" {
		headSHA, err := a.readRef(ctx, prepared.Input, prepared.Input.Head, token)
		if err != nil {
			return err
		}
		if headSHA != prepared.ExpectedHeadSHA {
			return &ProviderError{Class: "stale_resource_version", Err: fmt.Errorf("github head ref changed: %s->%s", prepared.ExpectedHeadSHA, headSHA)}
		}
		return nil
	}
	headSHA, found, err := a.readRefOptional(ctx, prepared.Input, prepared.Input.Head, token)
	if err != nil {
		return err
	}
	if found && headSHA != prepared.ExpectedHeadSHA {
		return &ProviderError{Class: "stale_resource_version", Err: fmt.Errorf("dependency-bound head exists at unexpected SHA %s", headSHA)}
	}
	return nil
}

func (a *Adapter) VerifyFresh(ctx context.Context, prepared Prepared, token []byte) error {
	headSHA, err := a.readRef(ctx, prepared.Input, prepared.Input.Head, token)
	if err != nil {
		return err
	}
	baseSHA, err := a.readRef(ctx, prepared.Input, prepared.Input.Base, token)
	if err != nil {
		return err
	}
	if headSHA != prepared.ExpectedHeadSHA || baseSHA != prepared.ExpectedBaseSHA {
		return &ProviderError{Class: "stale_resource_version", Err: fmt.Errorf("github refs changed: head %s->%s base %s->%s", prepared.ExpectedHeadSHA, headSHA, prepared.ExpectedBaseSHA, baseSHA)}
	}
	return nil
}

func (a *Adapter) Create(ctx context.Context, prepared Prepared, token []byte) (Receipt, error) {
	payload := createPayload{Title: prepared.Input.Title, Head: prepared.Input.Head, Base: prepared.Input.Base, Body: prepared.RenderedBody, Draft: true}
	body, err := json.Marshal(payload)
	if err != nil {
		return Receipt{}, err
	}
	endpoint := a.endpoint("repos", prepared.Input.Owner, prepared.Input.Repo, "pulls")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Receipt{}, err
	}
	a.applyHeaders(req, token)
	resp, err := a.client().Do(req)
	if err != nil {
		return Receipt{}, &ProviderError{Ambiguous: true, Class: "transport_unknown", Err: fmt.Errorf("github create draft pull request outcome is unknown: %w", err)}
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return Receipt{}, &ProviderError{Ambiguous: true, StatusCode: resp.StatusCode, Class: "response_read_unknown", Err: fmt.Errorf("github response outcome is unknown: %w", readErr)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Receipt{}, &ProviderError{StatusCode: resp.StatusCode, Class: "provider_rejected", Err: fmt.Errorf("github rejected draft pull request with status %d: %s", resp.StatusCode, safeProviderMessage(responseBody))}
	}
	var p pullResponse
	if err := json.Unmarshal(responseBody, &p); err != nil {
		return Receipt{}, &ProviderError{Ambiguous: true, StatusCode: resp.StatusCode, Class: "response_decode_unknown", Err: fmt.Errorf("github created a resource but response decoding failed: %w", err)}
	}
	if p.Number <= 0 || p.HTMLURL == "" {
		return Receipt{}, &ProviderError{Ambiguous: true, StatusCode: resp.StatusCode, Class: "incomplete_receipt_unknown", Err: errors.New("github returned an incomplete creation receipt")}
	}
	return a.receiptFromPull(p, false), nil
}

func (a *Adapter) Status(ctx context.Context, prepared Prepared, token []byte) (StatusResult, error) {
	q := url.Values{}
	q.Set("state", "all")
	q.Set("head", prepared.Input.Owner+":"+prepared.Input.Head)
	q.Set("base", prepared.Input.Base)
	endpoint := a.endpoint("repos", prepared.Input.Owner, prepared.Input.Repo, "pulls") + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return StatusResult{}, err
	}
	a.applyHeaders(req, token)
	resp, err := a.client().Do(req)
	if err != nil {
		return StatusResult{}, &ProviderError{Ambiguous: true, Class: "status_transport_error", Err: fmt.Errorf("github status query failed: %w", err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return StatusResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return StatusResult{}, &ProviderError{StatusCode: resp.StatusCode, Class: "status_rejected", Err: fmt.Errorf("github status query returned %d: %s", resp.StatusCode, safeProviderMessage(body))}
	}
	var pulls []pullResponse
	if err := json.Unmarshal(body, &pulls); err != nil {
		return StatusResult{}, fmt.Errorf("decode github status response: %w", err)
	}
	sort.Slice(pulls, func(i, j int) bool { return pulls[i].Number < pulls[j].Number })
	for _, p := range pulls {
		if p.Title != prepared.Input.Title || p.Head.Ref != prepared.Input.Head || p.Base.Ref != prepared.Input.Base || !strings.Contains(p.Body, prepared.EffectMarker) {
			continue
		}
		r := a.receiptFromPull(p, true)
		return StatusResult{Status: StatusCommitted, Receipt: &r}, nil
	}
	return StatusResult{Status: StatusNotFound}, nil
}

func (a *Adapter) readRefOptional(ctx context.Context, input Input, branch string, token []byte) (string, bool, error) {
	sha, err := a.readRef(ctx, input, branch, token)
	if err == nil {
		return sha, true, nil
	}
	var providerErr *ProviderError
	if errors.As(err, &providerErr) && providerErr.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	return "", false, err
}

func (a *Adapter) readRef(ctx context.Context, input Input, branch string, token []byte) (string, error) {
	endpoint := a.endpoint("repos", input.Owner, input.Repo, "git", "ref", "heads", branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	a.applyHeaders(req, token)
	resp, err := a.client().Do(req)
	if err != nil {
		return "", &ProviderError{Ambiguous: true, Class: "ref_transport_error", Err: fmt.Errorf("github ref query failed: %w", err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &ProviderError{StatusCode: resp.StatusCode, Class: "ref_rejected", Err: fmt.Errorf("github ref query returned %d: %s", resp.StatusCode, safeProviderMessage(body))}
	}
	var result struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode github ref response: %w", err)
	}
	if !isFullSHA(result.Object.SHA) {
		return "", errors.New("github ref response did not contain a full object SHA")
	}
	return strings.ToLower(result.Object.SHA), nil
}

func (a *Adapter) receiptFromPull(p pullResponse, recovered bool) Receipt {
	now := time.Now().UTC()
	if a.Now != nil {
		now = a.Now().UTC()
	}
	return Receipt{PullNumber: p.Number, NodeID: p.NodeID, HTMLURL: p.HTMLURL, HeadSHA: p.Head.SHA, BaseSHA: p.Base.SHA, Recovered: recovered, ObservedAt: now}
}

func (a *Adapter) endpoint(parts ...string) string {
	base := strings.TrimRight(strings.TrimSpace(a.BaseURL), "/")
	if base == "" {
		base = "https://api.github.com"
	}
	encoded := make([]string, 0, len(parts))
	for _, part := range parts {
		encoded = append(encoded, url.PathEscape(part))
	}
	return base + "/" + path.Join(encoded...)
}

func (a *Adapter) client() *http.Client {
	if a.HTTPClient != nil {
		clone := *a.HTTPClient
		clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
		if clone.Timeout == 0 {
			clone.Timeout = 30 * time.Second
		}
		return &clone
	}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     30 * time.Second,
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

func (a *Adapter) applyHeaders(req *http.Request, token []byte) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "FutureDiff/0.10")
	if len(token) > 0 {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
}

type createPayload struct {
	Title string `json:"title"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Body  string `json:"body"`
	Draft bool   `json:"draft"`
}

type pullResponse struct {
	Number  int    `json:"number"`
	NodeID  string `json:"node_id"`
	HTMLURL string `json:"html_url"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Draft   bool   `json:"draft"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`
}

func safeBranch(branch string) bool {
	if branch == "" || branch == "@" || strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") || strings.HasSuffix(branch, ".lock") || strings.Contains(branch, "..") || strings.Contains(branch, "//") || strings.Contains(branch, "@{") || strings.ContainsAny(branch, " ~^:?*[\\") {
		return false
	}
	for _, r := range branch {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func isFullSHA(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func safeProviderMessage(body []byte) string {
	message := strings.TrimSpace(string(body))
	if len(message) > 1024 {
		message = message[:1024] + "…"
	}
	return message
}
