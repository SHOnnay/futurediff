// Package githubbranch publishes an exact, already-approved local Git commit to
// a new FutureDiff-owned GitHub branch. It uses Git smart HTTP with a
// force-with-lease expectation that the destination ref does not yet exist.
package githubbranch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

const (
	AdapterID       = "builtin.github.branch-publish"
	AdapterVersion  = "0.1.0"
	ToolIdentity    = "github.publish_branch"
	ReadOperation   = "github.query_git_ref"
	CommitOperation = "github.publish_branch"
	SupportLevel    = "built_in_v0.1_create_only"
)

type Input struct {
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	RemoteURL  string `json:"remote_url"`
	CommitOID  string `json:"commit_oid"`
	TreeOID    string `json:"tree_oid"`
	Repository string `json:"repository"`
}

type Prepared struct {
	Input            Input             `json:"input"`
	ExpectedAbsent   bool              `json:"expected_absent"`
	RequestDigest    string            `json:"request_digest"`
	ResourceVersions map[string]string `json:"resource_versions"`
}

type Preview struct {
	Provider   string `json:"provider"`
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	CommitOID  string `json:"commit_oid"`
	TreeOID    string `json:"tree_oid"`
	CreateOnly bool   `json:"create_only"`
}

type Receipt struct {
	Branch     string    `json:"branch"`
	CommitOID  string    `json:"commit_oid"`
	RemoteURL  string    `json:"remote_url"`
	Recovered  bool      `json:"recovered"`
	ObservedAt time.Time `json:"observed_at"`
}

type Status string

const (
	StatusNotFound  Status = "not_found"
	StatusCommitted Status = "committed"
	StatusConflict  Status = "conflict"
)

type StatusResult struct {
	Status      Status   `json:"status"`
	Receipt     *Receipt `json:"receipt,omitempty"`
	ObservedOID string   `json:"observed_oid,omitempty"`
}

type ProviderError struct {
	Ambiguous bool
	Class     string
	Err       error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Class
}
func (e *ProviderError) Unwrap() error { return e.Err }

type GitRunner interface {
	LSRemote(ctx context.Context, repository, remoteURL, branch string, token []byte) (string, bool, error)
	PushCreateOnly(ctx context.Context, repository, remoteURL, branch, commitOID string, token []byte) error
}

type Adapter struct {
	Runner GitRunner
	Now    func() time.Time
}

func (a *Adapter) ID() string      { return AdapterID }
func (a *Adapter) Version() string { return AdapterVersion }

func (i Input) Normalize() (Input, error) {
	i.Owner = strings.TrimSpace(i.Owner)
	i.Repo = strings.TrimSpace(i.Repo)
	i.Branch = strings.TrimSpace(i.Branch)
	i.RemoteURL = strings.TrimSpace(i.RemoteURL)
	i.Repository = strings.TrimSpace(i.Repository)
	i.CommitOID = strings.ToLower(strings.TrimSpace(i.CommitOID))
	i.TreeOID = strings.ToLower(strings.TrimSpace(i.TreeOID))
	if i.Owner == "" || i.Repo == "" || i.Repository == "" {
		return Input{}, errors.New("owner, repo, and local repository are required")
	}
	if !safeName(i.Owner) || !safeName(i.Repo) {
		return Input{}, errors.New("invalid GitHub owner or repository")
	}
	if !safeBranch(i.Branch) || !strings.HasPrefix(i.Branch, "futurediff/") {
		return Input{}, errors.New("branch must be a safe futurediff/* branch")
	}
	if !isOID(i.CommitOID) || !isOID(i.TreeOID) {
		return Input{}, errors.New("full commit and tree object ids are required")
	}
	u, err := url.Parse(i.RemoteURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Input{}, errors.New("remote_url must be a credential-free HTTPS URL")
	}
	if u.Port() != "" && u.Port() != "443" {
		return Input{}, errors.New("remote_url must use the default HTTPS port")
	}
	wantPath := "/" + i.Owner + "/" + i.Repo + ".git"
	if strings.TrimSuffix(u.EscapedPath(), "/") != wantPath {
		return Input{}, fmt.Errorf("remote_url path must equal %s", wantPath)
	}
	abs, err := filepath.Abs(i.Repository)
	if err != nil {
		return Input{}, err
	}
	i.Repository = abs
	return i, nil
}

func (a *Adapter) Destination(input Input) (string, error) {
	n, err := input.Normalize()
	return n.RemoteURL, err
}

func (a *Adapter) Prepare(ctx context.Context, input Input, token []byte) (Prepared, Preview, error) {
	n, err := input.Normalize()
	if err != nil {
		return Prepared{}, Preview{}, err
	}
	status, err := a.Status(ctx, Prepared{Input: n}, token)
	if err != nil {
		return Prepared{}, Preview{}, err
	}
	if status.Status != StatusNotFound {
		return Prepared{}, Preview{}, &ProviderError{Class: "branch_already_exists", Err: fmt.Errorf("remote branch %s already exists at %s", n.Branch, status.ObservedOID)}
	}
	requestDigest, err := domain.Digest(map[string]any{"remote_url": n.RemoteURL, "branch": n.Branch, "commit_oid": n.CommitOID, "tree_oid": n.TreeOID, "expected_absent": true})
	if err != nil {
		return Prepared{}, Preview{}, err
	}
	resource := "github://" + n.Owner + "/" + n.Repo + "/refs/heads/" + n.Branch
	p := Prepared{Input: n, ExpectedAbsent: true, RequestDigest: requestDigest, ResourceVersions: map[string]string{resource: "absent"}}
	v := Preview{Provider: "github", Repository: n.Owner + "/" + n.Repo, Branch: n.Branch, CommitOID: n.CommitOID, TreeOID: n.TreeOID, CreateOnly: true}
	return p, v, nil
}

func (a *Adapter) VerifyFresh(ctx context.Context, prepared Prepared, token []byte) error {
	status, err := a.Status(ctx, prepared, token)
	if err != nil {
		return err
	}
	if status.Status == StatusNotFound {
		return nil
	}
	if status.Status == StatusCommitted {
		return nil
	}
	return &ProviderError{Class: "stale_resource_version", Err: fmt.Errorf("remote branch %s appeared at %s", prepared.Input.Branch, status.ObservedOID)}
}

func (a *Adapter) Publish(ctx context.Context, prepared Prepared, token []byte) (Receipt, error) {
	if !prepared.ExpectedAbsent {
		return Receipt{}, errors.New("branch publication requires an absent-ref lease")
	}
	if err := a.runner().PushCreateOnly(ctx, prepared.Input.Repository, prepared.Input.RemoteURL, prepared.Input.Branch, prepared.Input.CommitOID, token); err != nil {
		return Receipt{}, &ProviderError{Ambiguous: true, Class: "push_outcome_unknown", Err: fmt.Errorf("Git branch publication outcome is unknown: %w", err)}
	}
	status, err := a.Status(ctx, prepared, token)
	if err != nil {
		return Receipt{}, &ProviderError{Ambiguous: true, Class: "post_push_status_unknown", Err: err}
	}
	if status.Status != StatusCommitted || status.Receipt == nil {
		return Receipt{}, &ProviderError{Ambiguous: true, Class: "post_push_mismatch", Err: fmt.Errorf("remote branch did not resolve to approved commit; observed %s", status.ObservedOID)}
	}
	return *status.Receipt, nil
}

func (a *Adapter) Status(ctx context.Context, prepared Prepared, token []byte) (StatusResult, error) {
	n, err := prepared.Input.Normalize()
	if err != nil {
		return StatusResult{}, err
	}
	oid, found, err := a.runner().LSRemote(ctx, n.Repository, n.RemoteURL, n.Branch, token)
	if err != nil {
		return StatusResult{}, &ProviderError{Ambiguous: true, Class: "status_unknown", Err: err}
	}
	if !found {
		return StatusResult{Status: StatusNotFound}, nil
	}
	if strings.EqualFold(oid, n.CommitOID) {
		now := time.Now().UTC()
		if a.Now != nil {
			now = a.Now().UTC()
		}
		r := Receipt{Branch: n.Branch, CommitOID: n.CommitOID, RemoteURL: n.RemoteURL, Recovered: true, ObservedAt: now}
		return StatusResult{Status: StatusCommitted, Receipt: &r, ObservedOID: oid}, nil
	}
	return StatusResult{Status: StatusConflict, ObservedOID: oid}, nil
}

func (a *Adapter) runner() GitRunner {
	if a.Runner != nil {
		return a.Runner
	}
	return SecureGitRunner{}
}

type SecureGitRunner struct{}

func (SecureGitRunner) LSRemote(ctx context.Context, repository, remoteURL, branch string, token []byte) (string, bool, error) {
	out, code, err := secureGit(ctx, repository, token, "ls-remote", "--exit-code", remoteURL, "refs/heads/"+branch)
	if code == 2 {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return "", false, errors.New("git ls-remote returned malformed output")
	}
	return strings.ToLower(fields[0]), true, nil
}
func (SecureGitRunner) PushCreateOnly(ctx context.Context, repository, remoteURL, branch, commitOID string, token []byte) error {
	_, _, err := secureGit(ctx, repository, token, "push", "--porcelain", "--force-with-lease=refs/heads/"+branch+":", remoteURL, commitOID+":refs/heads/"+branch)
	return err
}

func secureGit(ctx context.Context, repository string, token []byte, args ...string) ([]byte, int, error) {
	dir, err := os.MkdirTemp("", "futurediff-askpass-")
	if err != nil {
		return nil, -1, err
	}
	defer os.RemoveAll(dir)
	helper := filepath.Join(dir, "askpass.sh")
	script := "#!/bin/sh\ncase \"$1\" in\n  *Username*) printf '%s\\n' 'x-access-token' ;;\n  *) cat /dev/fd/3 ;;\nesac\n"
	if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
		return nil, -1, err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, -1, err
	}
	defer r.Close()
	go func() { _, _ = w.Write(token); _ = w.Close() }()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "credential.helper=", "-c", "http.followRedirects=false"}, args...)...)
	cmd.Dir = repository
	cmd.ExtraFiles = []*os.File{r}
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "LANG=C", "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=" + helper, "SSH_ASKPASS=" + helper}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		code = -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		return stdout.Bytes(), code, fmt.Errorf("git %s failed: %s", args[0], sanitize(stderr.String(), token))
	}
	return stdout.Bytes(), code, nil
}
func sanitize(s string, token []byte) string {
	if len(token) > 0 {
		s = strings.ReplaceAll(s, string(token), "[REDACTED]")
	}
	return strings.TrimSpace(s)
}
func safeName(v string) bool {
	if v == "" || strings.HasPrefix(v, ".") || strings.HasSuffix(v, ".") || strings.Contains(v, "..") || strings.ContainsAny(v, " /\\@?#") {
		return false
	}
	for _, r := range v {
		if !(r == '-' || r == '_' || r == '.' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}
func safeBranch(v string) bool {
	if v == "" || v == "@" || strings.HasPrefix(v, "-") || strings.HasPrefix(v, "/") || strings.HasSuffix(v, "/") || strings.HasSuffix(v, ".") || strings.HasSuffix(v, ".lock") || strings.Contains(v, "..") || strings.Contains(v, "//") || strings.Contains(v, "@{") || strings.ContainsAny(v, " ~^:?*[\\") {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func isOID(v string) bool {
	if len(v) != 40 && len(v) != 64 {
		return false
	}
	for _, r := range v {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}
