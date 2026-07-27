package providercert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/egress"
)

const ConfirmationPhrase = "I_UNDERSTAND_FUTUREDIFF_WILL_CREATE_AND_CLEAN_UP_TEST_RESOURCES"

type Status string

const (
	Pass    Status = "pass"
	Fail    Status = "fail"
	Blocked Status = "blocked"
	Skip    Status = "skip"
)

type Check struct {
	ID       string         `json:"id"`
	Status   Status         `json:"status"`
	Detail   string         `json:"detail"`
	Evidence map[string]any `json:"evidence,omitempty"`
}
type Target struct {
	Name      string  `json:"name"`
	Checks    []Check `json:"checks"`
	Certified bool    `json:"certified"`
}
type Report struct {
	FormatVersion string    `json:"format_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	Nonce         string    `json:"nonce"`
	Targets       []Target  `json:"targets"`
	Certified     bool      `json:"certified"`
}
type Options struct {
	Targets        []string
	Confirmation   string
	Nonce          string
	GitHubOwner    string
	GitHubRepo     string
	GitHubTokenEnv string
	GitHubAPIBase  string
	SlackChannel   string
	SlackTokenEnv  string
	SlackAPIBase   string
}
type Dependencies struct {
	HTTPClientFactory            func(egress.Policy) (*http.Client, error)
	SkipPolicyValidationForTests bool
}

func Run(ctx context.Context, o Options, d Dependencies) (Report, error) {
	if o.Confirmation != ConfirmationPhrase {
		return Report{}, errors.New("explicit provider-mutation confirmation phrase is required")
	}
	if o.Nonce == "" {
		o.Nonce = fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	if d.HTTPClientFactory == nil {
		d.HTTPClientFactory = egress.NewClient
	}
	requested := map[string]bool{}
	for _, v := range o.Targets {
		for _, x := range strings.Split(v, ",") {
			x = strings.TrimSpace(strings.ToLower(x))
			if x != "" {
				requested[x] = true
			}
		}
	}
	if len(requested) == 0 {
		return Report{}, errors.New("at least one target is required")
	}
	r := Report{FormatVersion: "0.1", GeneratedAt: time.Now().UTC(), Nonce: o.Nonce}
	for _, name := range []string{"github", "slack"} {
		t := Target{Name: name}
		if !requested[name] {
			t.Checks = []Check{{ID: "not_requested", Status: Skip, Detail: "target not requested"}}
			r.Targets = append(r.Targets, t)
			continue
		}
		switch name {
		case "github":
			t.Checks = certifyGitHub(ctx, o, d)
		case "slack":
			t.Checks = certifySlack(ctx, o, d)
		}
		t.Certified = true
		for _, c := range t.Checks {
			if c.Status == Fail || c.Status == Blocked {
				t.Certified = false
			}
		}
		r.Targets = append(r.Targets, t)
	}
	r.Certified = true
	for _, t := range r.Targets {
		if requested[t.Name] && !t.Certified {
			r.Certified = false
		}
	}
	return r, nil
}

func certifyGitHub(ctx context.Context, o Options, d Dependencies) []Check {
	if o.GitHubOwner == "" || o.GitHubRepo == "" || o.GitHubTokenEnv == "" {
		return []Check{{ID: "github_prerequisites", Status: Blocked, Detail: "owner, repo, and token environment name are required"}}
	}
	token := os.Getenv(o.GitHubTokenEnv)
	if token == "" {
		return []Check{{ID: "github_token", Status: Blocked, Detail: "token environment variable is empty"}}
	}
	base := nonEmpty(o.GitHubAPIBase, "https://api.github.com")
	var policy egress.Policy
	if !d.SkipPolicyValidationForTests {
		rule, err := egress.RuleFromBase(base, http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete)
		if err != nil {
			return []Check{{ID: "github_egress", Status: Fail, Detail: err.Error()}}
		}
		policy = egress.Policy{Rules: []egress.Rule{rule}}
	}
	client, err := d.HTTPClientFactory(policy)
	if err != nil {
		return []Check{{ID: "github_client", Status: Fail, Detail: err.Error()}}
	}
	prefix := strings.TrimRight(base, "/") + "/repos/" + url.PathEscape(o.GitHubOwner) + "/" + url.PathEscape(o.GitHubRepo)
	headers := map[string]string{"Authorization": "Bearer " + token, "Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}
	var repo struct {
		DefaultBranch string `json:"default_branch"`
		FullName      string `json:"full_name"`
	}
	if err := doJSON(ctx, client, http.MethodGet, prefix, nil, headers, &repo); err != nil {
		return []Check{{ID: "github_repository", Status: Fail, Detail: safeErr(err)}}
	}
	branch := "futurediff-cert/" + sanitizeNonce(o.Nonce)
	refPath := prefix + "/git/ref/heads/" + strings.ReplaceAll(url.PathEscape(repo.DefaultBranch), "%2F", "/")
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := doJSON(ctx, client, http.MethodGet, refPath, nil, headers, &ref); err != nil {
		return []Check{{ID: "github_base_ref", Status: Fail, Detail: safeErr(err)}}
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := doJSON(ctx, client, http.MethodGet, prefix+"/git/commits/"+ref.Object.SHA, nil, headers, &commit); err != nil {
		return []Check{{ID: "github_base_commit", Status: Fail, Detail: safeErr(err)}}
	}
	var createdCommit struct {
		SHA string `json:"sha"`
	}
	payload := map[string]any{"message": "FutureDiff provider certification " + o.Nonce, "tree": commit.Tree.SHA, "parents": []string{ref.Object.SHA}}
	if err := doJSON(ctx, client, http.MethodPost, prefix+"/git/commits", payload, headers, &createdCommit); err != nil {
		return []Check{{ID: "github_create_commit", Status: Fail, Detail: safeErr(err)}}
	}
	cleanupChecks := []Check{}
	branchCreated := false
	prNumber := 0
	defer func() { _ = branchCreated; _ = prNumber }()
	if err := doJSON(ctx, client, http.MethodPost, prefix+"/git/refs", map[string]any{"ref": "refs/heads/" + branch, "sha": createdCommit.SHA}, headers, &map[string]any{}); err != nil {
		return []Check{{ID: "github_create_branch", Status: Fail, Detail: safeErr(err)}}
	}
	branchCreated = true
	var pr struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		Draft   bool   `json:"draft"`
	}
	prPayload := map[string]any{"title": "FutureDiff certification " + o.Nonce, "head": branch, "base": repo.DefaultBranch, "body": "Disposable FutureDiff provider certification. Marker: " + o.Nonce, "draft": true}
	prErr := doJSON(ctx, client, http.MethodPost, prefix+"/pulls", prPayload, headers, &pr)
	if prErr == nil {
		prNumber = pr.Number
	}
	checks := []Check{{ID: "github_create_branch", Status: Pass, Detail: "create-only certification branch created", Evidence: map[string]any{"branch": branch, "commit_sha": createdCommit.SHA}}}
	if prErr != nil {
		checks = append(checks, Check{ID: "github_create_draft_pr", Status: Fail, Detail: safeErr(prErr)})
	} else {
		checks = append(checks, Check{ID: "github_create_draft_pr", Status: Pass, Detail: "draft pull request created", Evidence: map[string]any{"number": pr.Number, "draft": pr.Draft, "url": pr.HTMLURL}})
	}
	if prNumber > 0 {
		err = doJSON(ctx, client, http.MethodPatch, fmt.Sprintf("%s/pulls/%d", prefix, prNumber), map[string]any{"state": "closed"}, headers, &map[string]any{})
		if err != nil {
			cleanupChecks = append(cleanupChecks, Check{ID: "github_close_pr", Status: Fail, Detail: safeErr(err)})
		} else {
			cleanupChecks = append(cleanupChecks, Check{ID: "github_close_pr", Status: Pass, Detail: "draft pull request closed"})
		}
	}
	if branchCreated {
		deleteURL := prefix + "/git/refs/heads/" + strings.ReplaceAll(url.PathEscape(branch), "%2F", "/")
		err = doNoContent(ctx, client, http.MethodDelete, deleteURL, headers)
		if err != nil {
			cleanupChecks = append(cleanupChecks, Check{ID: "github_delete_branch", Status: Fail, Detail: safeErr(err)})
		} else {
			cleanupChecks = append(cleanupChecks, Check{ID: "github_delete_branch", Status: Pass, Detail: "certification branch deleted"})
		}
	}
	return append(checks, cleanupChecks...)
}

func certifySlack(ctx context.Context, o Options, d Dependencies) []Check {
	if o.SlackChannel == "" || o.SlackTokenEnv == "" {
		return []Check{{ID: "slack_prerequisites", Status: Blocked, Detail: "channel and token environment name are required"}}
	}
	token := os.Getenv(o.SlackTokenEnv)
	if token == "" {
		return []Check{{ID: "slack_token", Status: Blocked, Detail: "token environment variable is empty"}}
	}
	base := nonEmpty(o.SlackAPIBase, "https://slack.com/api")
	var policy egress.Policy
	if !d.SkipPolicyValidationForTests {
		rule, err := egress.RuleFromBase(base, http.MethodPost)
		if err != nil {
			return []Check{{ID: "slack_egress", Status: Fail, Detail: err.Error()}}
		}
		policy = egress.Policy{Rules: []egress.Rule{rule}}
	}
	client, err := d.HTTPClientFactory(policy)
	if err != nil {
		return []Check{{ID: "slack_client", Status: Fail, Detail: err.Error()}}
	}
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json; charset=utf-8"}
	marker := "FutureDiff provider certification " + o.Nonce
	var post struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		TS      string `json:"ts"`
		Channel string `json:"channel"`
	}
	if err := doJSON(ctx, client, http.MethodPost, strings.TrimRight(base, "/")+"/chat.postMessage", map[string]any{"channel": o.SlackChannel, "text": marker}, headers, &post); err != nil {
		return []Check{{ID: "slack_post_message", Status: Fail, Detail: safeErr(err)}}
	}
	if !post.OK {
		return []Check{{ID: "slack_post_message", Status: Fail, Detail: "Slack error: " + post.Error}}
	}
	checks := []Check{{ID: "slack_post_message", Status: Pass, Detail: "certification message posted", Evidence: map[string]any{"channel": post.Channel, "ts": post.TS}}}
	var del struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	err = doJSON(ctx, client, http.MethodPost, strings.TrimRight(base, "/")+"/chat.delete", map[string]any{"channel": post.Channel, "ts": post.TS}, headers, &del)
	if err != nil || !del.OK {
		detail := "Slack delete failed"
		if err != nil {
			detail = safeErr(err)
		} else if del.Error != "" {
			detail += " : " + del.Error
		}
		checks = append(checks, Check{ID: "slack_delete_message", Status: Fail, Detail: detail})
	} else {
		checks = append(checks, Check{ID: "slack_delete_message", Status: Pass, Detail: "certification message deleted"})
	}
	return checks
}

func doJSON(ctx context.Context, c *http.Client, method, endpoint string, payload any, headers map[string]string, out any) error {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if payload != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider HTTP %d: %s", resp.StatusCode, truncate(string(b)))
	}
	if out != nil && len(bytes.TrimSpace(b)) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}
func doNoContent(ctx context.Context, c *http.Client, method, endpoint string, headers map[string]string) error {
	return doJSON(ctx, c, method, endpoint, nil, headers, nil)
}
func sanitizeNonce(v string) string {
	var b strings.Builder
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		s = "run"
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
func nonEmpty(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}
func safeErr(err error) string {
	if err == nil {
		return ""
	}
	return truncate(err.Error())
}
func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 512 {
		return s[:512]
	}
	return s
}
func WriteJSON(path string, r Report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(b)
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
