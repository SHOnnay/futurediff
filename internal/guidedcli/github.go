package guidedcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	githubBranchAdapterID = "builtin.github.branch-publish"
	githubDraftAdapterID  = "builtin.github.draft-pull-request"
	maxGitHubPRTitleBytes = 256
	maxGitHubPRBodyBytes  = 64 * 1024
)

type githubFinishOptions struct {
	Enabled      bool
	Remote       string
	Base         string
	Title        string
	Body         string
	CredentialID string
}

type githubTarget struct {
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Remote     string `json:"remote"`
	RemoteURL  string `json:"remote_url"`
	Base       string `json:"base"`
	Head       string `json:"head"`
	Title      string `json:"title"`
	Body       string `json:"body,omitempty"`
	Credential string `json:"credential_id"`
}

func parseFinishOptions(args []string, defaultCredential string) (githubFinishOptions, error) {
	options := githubFinishOptions{Remote: "origin", CredentialID: strings.TrimSpace(defaultCredential)}
	var bodySource string
	providerOptionSeen := false
	positionals := 0
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, inlineValue, hasInlineValue := strings.Cut(arg, "=")
		switch key {
		case "--github":
			if hasInlineValue {
				return options, errors.New("--github does not accept a value")
			}
			options.Enabled = true
		case "--remote", "--base", "--title", "--body", "--body-file", "--github-credential":
			providerOptionSeen = true
			value := inlineValue
			if !hasInlineValue {
				if i+1 >= len(args) {
					return options, fmt.Errorf("%s requires a value", key)
				}
				i++
				value = args[i]
			}
			if value == "" {
				return options, fmt.Errorf("%s requires a value", key)
			}
			switch key {
			case "--remote":
				options.Remote = strings.TrimSpace(value)
			case "--base":
				options.Base = strings.TrimSpace(value)
			case "--title":
				options.Title = strings.TrimSpace(value)
			case "--body":
				if bodySource != "" {
					return options, errors.New("use only one of --body or --body-file")
				}
				bodySource = key
				options.Body = value
			case "--body-file":
				if bodySource != "" {
					return options, errors.New("use only one of --body or --body-file")
				}
				bodySource = key
				info, statErr := os.Lstat(value)
				if statErr != nil {
					return options, fmt.Errorf("inspect GitHub PR body file: %w", statErr)
				}
				if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
					return options, errors.New("GitHub PR body file must be a regular file, not a symlink")
				}
				if info.Size() > maxGitHubPRBodyBytes {
					return options, errors.New("GitHub PR body exceeds 64 KiB")
				}
				data, readErr := os.ReadFile(value)
				if readErr != nil {
					return options, fmt.Errorf("read GitHub PR body: %w", readErr)
				}
				options.Body = string(data)
			case "--github-credential":
				options.CredentialID = strings.TrimSpace(value)
			}
		case "--full", "--yes", "-y":
			if hasInlineValue {
				return options, fmt.Errorf("%s does not accept a value", key)
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return options, fmt.Errorf("unknown finish option %q", arg)
			}
			positionals++
			if positionals > 1 {
				return options, errors.New("finish accepts at most one transaction ID")
			}
		}
	}
	if providerOptionSeen && !options.Enabled {
		return options, errors.New("GitHub options require --github")
	}
	if !options.Enabled {
		return options, nil
	}
	if options.CredentialID == "" {
		return options, errors.New("GitHub publication requires --github-credential or FUTUREDIFF_GITHUB_CREDENTIAL_ID")
	}
	if options.Remote == "" {
		return options, errors.New("GitHub remote name cannot be empty")
	}
	if !safeRemoteName(options.Remote) {
		return options, fmt.Errorf("unsafe Git remote name %q", options.Remote)
	}
	if len(options.Title) > maxGitHubPRTitleBytes {
		return options, errors.New("GitHub pull-request title exceeds 256 characters")
	}
	if len(options.Body) > maxGitHubPRBodyBytes {
		return options, errors.New("GitHub pull-request body exceeds 64 KiB")
	}
	return options, nil
}

func safeRemoteName(value string) bool {
	if value == "" || strings.HasPrefix(value, "-") || strings.ContainsAny(value, "\x00\r\n\t /\\") {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == '.' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

func resolveGitHubTarget(ctx context.Context, gitBinary string, response Response, id string, options githubFinishOptions) (githubTarget, error) {
	if response.Workspace == nil || response.Workspace.RepositoryRoot == "" {
		return githubTarget{}, errors.New("change has no repository root")
	}
	rawRemote, err := gitOutput(ctx, gitBinary, response.Workspace.RepositoryRoot, "remote", "get-url", options.Remote)
	if err != nil {
		return githubTarget{}, fmt.Errorf("resolve GitHub remote %q: %w", options.Remote, err)
	}
	owner, repo, httpsURL, err := parseGitHubRemote(strings.TrimSpace(rawRemote))
	if err != nil {
		return githubTarget{}, err
	}
	base := options.Base
	if base == "" {
		base = strings.TrimPrefix(response.Workspace.SourceHeadRef, "refs/heads/")
	}
	if !safeGitBranch(base) {
		return githubTarget{}, errors.New("cannot determine a safe GitHub base branch; supply --base <branch>")
	}
	if _, err := gitOutput(ctx, gitBinary, response.Workspace.RepositoryRoot, "check-ref-format", "--branch", base); err != nil {
		return githubTarget{}, fmt.Errorf("invalid GitHub base branch %q: %w", base, err)
	}
	title := options.Title
	if title == "" {
		title = "FutureDiff change " + shortID(id)
	}
	if len(title) > maxGitHubPRTitleBytes {
		return githubTarget{}, errors.New("GitHub pull-request title exceeds 256 characters")
	}
	body := options.Body
	if strings.TrimSpace(body) == "" {
		body = fmt.Sprintf("Reviewed and published through FutureDiff.\n\n- Change ID: `%s`\n- Safe branch: `%s`\n- Exact change verified before approval.", id, "futurediff/"+id)
	}
	if len(body) > maxGitHubPRBodyBytes {
		return githubTarget{}, errors.New("GitHub pull-request body exceeds 64 KiB")
	}
	return githubTarget{
		Owner: owner, Repo: repo, Remote: options.Remote, RemoteURL: httpsURL,
		Base: base, Head: "futurediff/" + id, Title: title, Body: body,
		Credential: options.CredentialID,
	}, nil
}

func parseGitHubRemote(raw string) (owner, repo, httpsURL string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", errors.New("Git remote URL is empty")
	}
	var host, path string
	switch {
	case strings.HasPrefix(raw, "git@github.com:"):
		host = "github.com"
		path = strings.TrimPrefix(raw, "git@github.com:")
	case strings.Contains(raw, "://"):
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil || parsed.Hostname() == "" {
			return "", "", "", fmt.Errorf("unsupported GitHub remote URL %q", raw)
		}
		if parsed.Scheme != "https" && parsed.Scheme != "ssh" {
			return "", "", "", fmt.Errorf("GitHub remote scheme %q is not supported", parsed.Scheme)
		}
		if parsed.User != nil {
			if parsed.Scheme != "ssh" || parsed.User.Username() != "git" || parsed.User.String() != "git" {
				return "", "", "", errors.New("GitHub remote URL must not contain embedded credentials")
			}
		}
		if parsed.Port() != "" && !((parsed.Scheme == "https" && parsed.Port() == "443") || (parsed.Scheme == "ssh" && parsed.Port() == "22")) {
			return "", "", "", errors.New("GitHub remote URL must use the default port")
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || strings.Contains(parsed.EscapedPath(), "%") {
			return "", "", "", errors.New("GitHub remote URL must not contain escaping, query parameters, or fragments")
		}
		host = parsed.Hostname()
		path = parsed.Path
	default:
		return "", "", "", fmt.Errorf("unsupported GitHub remote URL %q", raw)
	}
	if !strings.EqualFold(host, "github.com") {
		return "", "", "", fmt.Errorf("remote host %q is not supported by the GitHub alpha integration", host)
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || !safeGitHubPart(parts[0]) || !safeGitHubPart(parts[1]) {
		return "", "", "", fmt.Errorf("GitHub remote path must be owner/repository, found %q", path)
	}
	owner, repo = parts[0], parts[1]
	return owner, repo, "https://github.com/" + owner + "/" + repo + ".git", nil
}

func safeGitHubPart(value string) bool {
	if value == "" || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == '.' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

func safeGitBranch(value string) bool {
	if value == "" || value == "." || value == "HEAD" || strings.HasPrefix(value, "refs/") || strings.HasPrefix(value, "-") || strings.HasPrefix(value, ".") || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") || strings.Contains(value, "//") || strings.Contains(value, "@{") || strings.HasSuffix(value, ".lock") {
		return false
	}
	for _, r := range value {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	return true
}

func (a *App) prepareGitHubEffects(ctx context.Context, id string, response Response, target githubTarget) (Response, error) {
	if response.Transaction == nil || response.Transaction.Status != "sealed" {
		return Response{}, fmt.Errorf("GitHub publication must be selected while the change is sealed; current status is %q", transactionStatus(response.Transaction))
	}
	branchEffect, draftEffect, err := matchingGitHubEffects(response.Effects, target)
	if err != nil {
		return Response{}, err
	}
	if branchEffect == nil {
		raw, runErr := a.Engine.Run(ctx, "prepare-github-branch", id, target.Credential, target.Owner, target.Repo, target.Head, target.RemoteURL)
		if runErr != nil {
			return Response{}, runErr
		}
		effect, decodeErr := decodeExternalEffect(raw)
		if decodeErr != nil {
			return Response{}, decodeErr
		}
		branchEffect = &effect
	}
	if draftEffect == nil {
		raw, runErr := a.Engine.Run(ctx, "prepare-github-pr", id, target.Credential, target.Owner, target.Repo, target.Head, target.Base, target.Title, target.Body, branchEffect.EffectID)
		if runErr != nil {
			return Response{}, runErr
		}
		if _, decodeErr := decodeExternalEffect(raw); decodeErr != nil {
			return Response{}, decodeErr
		}
	}
	_, updated, err := a.get(ctx, id)
	return updated, err
}

func matchingGitHubEffects(effects []ExternalEffect, target githubTarget) (*ExternalEffect, *ExternalEffect, error) {
	var branch, draft *ExternalEffect
	for i := range effects {
		effect := &effects[i]
		switch effect.AdapterIdentity {
		case githubBranchAdapterID:
			if branch != nil {
				return nil, nil, errors.New("this change contains multiple GitHub branch effects; inspect it with the low-level CLI")
			}
			var input githubBranchInput
			if err := json.Unmarshal([]byte(effect.InputJSON), &input); err != nil {
				return nil, nil, fmt.Errorf("decode prepared GitHub branch effect: %w", err)
			}
			if input.Owner != target.Owner || input.Repo != target.Repo || input.Branch != target.Head || input.RemoteURL != target.RemoteURL || effect.CredentialID != target.Credential {
				return nil, nil, errors.New("this change already contains a different GitHub branch effect; use the low-level CLI to inspect or restart the change")
			}
			branch = effect
		case githubDraftAdapterID:
			if draft != nil {
				return nil, nil, errors.New("this change contains multiple GitHub pull-request effects; inspect it with the low-level CLI")
			}
			var input githubDraftInput
			if err := json.Unmarshal([]byte(effect.InputJSON), &input); err != nil {
				return nil, nil, fmt.Errorf("decode prepared GitHub pull-request effect: %w", err)
			}
			if input.Owner != target.Owner || input.Repo != target.Repo || input.Head != target.Head || input.Base != target.Base || input.Title != target.Title || input.Body != target.Body || effect.CredentialID != target.Credential {
				return nil, nil, errors.New("this change already contains a different GitHub pull-request effect; use the low-level CLI to inspect or restart the change")
			}
			draft = effect
		default:
			return nil, nil, fmt.Errorf("guided GitHub finish cannot commit prepared effect %q; inspect and commit this change with the low-level CLI", effect.AdapterIdentity)
		}
	}
	if draft != nil && branch == nil {
		return nil, nil, errors.New("GitHub pull-request effect exists without its branch dependency")
	}
	if branch != nil && draft != nil {
		var input githubDraftInput
		if err := json.Unmarshal([]byte(draft.InputJSON), &input); err != nil {
			return nil, nil, fmt.Errorf("decode prepared GitHub pull-request dependency: %w", err)
		}
		if input.DependsOnEffectID != branch.EffectID || !slices.Contains(draft.DependsOn, branch.EffectID) {
			return nil, nil, errors.New("GitHub pull-request effect is not bound to the prepared branch effect")
		}
	}
	return branch, draft, nil
}

func hasPreparedEffects(response Response) bool {
	return len(response.Effects) > 0
}

func hasGitHubEffects(response Response) bool {
	for _, effect := range response.Effects {
		if effect.AdapterIdentity == githubBranchAdapterID || effect.AdapterIdentity == githubDraftAdapterID {
			return true
		}
	}
	return false
}

func githubResult(response Response, target githubTarget) GitHubPublishResult {
	result := GitHubPublishResult{Requested: true, Owner: target.Owner, Repo: target.Repo, Branch: target.Head, Base: target.Base, Draft: true}
	effectByID := make(map[string]ExternalEffect, len(response.Effects))
	for _, effect := range response.Effects {
		effectByID[effect.EffectID] = effect
	}
	for _, receipt := range response.Receipts {
		effect, ok := effectByID[receipt.EffectID]
		if !ok || effect.AdapterIdentity != githubDraftAdapterID {
			continue
		}
		result.EffectID = receipt.EffectID
		result.ProviderResourceID = receipt.ProviderResourceID
		if strings.HasPrefix(receipt.ProviderResourceID, "github://") {
			suffix := strings.TrimPrefix(receipt.ProviderResourceID, "github://")
			parts := strings.Split(suffix, "/")
			if len(parts) == 4 && parts[2] == "pulls" && parts[3] != "" && safeGitHubPart(parts[0]) && safeGitHubPart(parts[1]) && allDigits(parts[3]) {
				result.PullRequestURL = "https://github.com/" + parts[0] + "/" + parts[1] + "/pull/" + parts[3]
			}
		}
	}
	if result.PullRequestURL == "" {
		result.PullRequestURL = "https://github.com/" + target.Owner + "/" + target.Repo + "/pulls"
		result.URLIsFallback = true
	}
	return result
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func credentialConfigPath(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if value := strings.TrimSpace(os.Getenv("FUTUREDIFF_CREDENTIAL_CONFIG")); value != "" {
		return value
	}
	return ""
}

func cleanDisplayPath(path string) string {
	if path == "" {
		return "not configured"
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}
