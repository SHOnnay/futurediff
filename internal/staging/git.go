package staging

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

type DirtyPolicy string

const (
	Reject        DirtyPolicy = "reject"
	StageFromHead DirtyPolicy = "stage_from_head"
)

type Manager struct{ RuntimeRoot string }

type InspectResult struct {
	RepositoryRoot string
	GitCommonDir   string
	SourceHeadRef  string
	BaseOID        string
	ObjectFormat   string
	StatusDigest   string
	Dirty          bool
}

func gitEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "LANG=C.UTF-8", "LC_ALL=C.UTF-8",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0",
		"GIT_NO_REPLACE_OBJECTS=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_EXTERNAL_DIFF=", "GIT_DIFF_OPTS=", "GIT_PAGER=cat", "PAGER=cat",
	}
}

func gitCommand(repo string, env []string, args ...string) *exec.Cmd {
	base := []string{"--no-replace-objects", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "diff.external=", "-c", "core.pager=cat"}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Dir = repo
	cmd.Env = env
	return cmd
}

func runGit(repo string, args ...string) ([]byte, error) {
	cmd := gitCommand(repo, gitEnv(), args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %v: %w: %s", args, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
func gitText(repo string, args ...string) (string, error) {
	b, err := runGit(repo, args...)
	return strings.TrimSpace(string(b)), err
}

func (m Manager) Inspect(repository string, policy DirtyPolicy) (InspectResult, error) {
	root, err := gitText(repository, "rev-parse", "--show-toplevel")
	if err != nil {
		return InspectResult{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return InspectResult{}, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return InspectResult{}, err
	}
	common, err := gitText(root, "rev-parse", "--git-common-dir")
	if err != nil {
		return InspectResult{}, err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	common, err = filepath.Abs(common)
	if err != nil {
		return InspectResult{}, err
	}
	base, err := gitText(root, "rev-parse", "HEAD^{commit}")
	if err != nil {
		return InspectResult{}, err
	}
	ref, _ := gitText(root, "symbolic-ref", "-q", "HEAD")
	format, err := gitText(root, "rev-parse", "--show-object-format")
	if err != nil {
		return InspectResult{}, err
	}
	status, err := runGit(root, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return InspectResult{}, err
	}
	dirty := len(status) > 0
	if dirty && policy == Reject {
		return InspectResult{}, errors.New("repository is dirty; use stage_from_head explicitly")
	}
	if err := strictRepositoryChecks(root); err != nil {
		return InspectResult{}, err
	}
	return InspectResult{RepositoryRoot: root, GitCommonDir: common, SourceHeadRef: ref, BaseOID: base, ObjectFormat: format, StatusDigest: domain.SHA256Bytes(status), Dirty: dirty}, nil
}

func strictRepositoryChecks(root string) error {
	staged, err := runGit(root, "ls-files", "--stage", "-z")
	if err != nil {
		return err
	}
	for _, record := range bytes.Split(staged, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		fields := bytes.Fields(record)
		if len(fields) > 0 && (string(fields[0]) == "120000" || string(fields[0]) == "160000") {
			return fmt.Errorf("strict mode rejects tracked symlinks and submodules")
		}
	}
	attrs, err := runGit(root, "check-attr", "-a", "--", ":(top)**")
	if err == nil && bytes.Contains(bytes.ToLower(attrs), []byte("filter:")) {
		return errors.New("strict mode rejects Git content filters")
	}
	for _, scope := range []string{"local", "worktree"} {
		out, _ := runGit(root, "config", "--"+scope, "--get-regexp", "^filter\\..*\\.(clean|smudge|process)$")
		if len(bytes.TrimSpace(out)) > 0 {
			return errors.New("strict mode rejects configured Git filters")
		}
	}
	return nil
}

func (m Manager) Create(transactionID string, inspect InspectResult, policy DirtyPolicy) (domain.Workspace, error) {
	root := filepath.Join(m.RuntimeRoot, "transactions", transactionID)
	workspace := filepath.Join(root, "workspace")
	artifacts := filepath.Join(root, "artifacts")
	if _, err := os.Stat(root); err == nil {
		return domain.Workspace{}, errors.New("transaction runtime path already exists")
	}
	if err := os.MkdirAll(artifacts, 0o700); err != nil {
		return domain.Workspace{}, err
	}
	if _, err := runGit(inspect.RepositoryRoot, "worktree", "add", "--detach", workspace, inspect.BaseOID); err != nil {
		_ = os.RemoveAll(root)
		return domain.Workspace{}, err
	}
	return domain.Workspace{TransactionID: transactionID, RepositoryRoot: inspect.RepositoryRoot, GitCommonDir: inspect.GitCommonDir, SourceHeadRef: inspect.SourceHeadRef, BaseOID: inspect.BaseOID, ObjectFormat: inspect.ObjectFormat, WorkspacePath: workspace, ArtifactsPath: artifacts, DirtyPolicy: string(policy), SourceStatusDigest: inspect.StatusDigest, CreatedAt: time.Now().UTC()}, nil
}

func (m Manager) Capture(workspace domain.Workspace) (domain.Patch, error) {
	if _, err := runGit(workspace.WorkspacePath, "add", "-A"); err != nil {
		return domain.Patch{}, err
	}
	patch, err := runGit(workspace.WorkspacePath, "diff", "--cached", "--binary", "--full-index", "--no-ext-diff")
	if err != nil {
		return domain.Patch{}, err
	}
	tree, err := gitText(workspace.WorkspacePath, "write-tree")
	if err != nil {
		return domain.Patch{}, err
	}
	names, err := runGit(workspace.WorkspacePath, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return domain.Patch{}, err
	}
	var paths []string
	for _, p := range bytes.Split(names, []byte{0}) {
		if len(p) > 0 {
			paths = append(paths, string(p))
		}
	}
	sort.Strings(paths)
	sha := domain.SHA256Bytes(patch)
	patchPath := filepath.Join(workspace.ArtifactsPath, "staged.patch")
	tmp := patchPath + ".tmp"
	if err := os.WriteFile(tmp, patch, 0o600); err != nil {
		return domain.Patch{}, err
	}
	if err := os.Rename(tmp, patchPath); err != nil {
		return domain.Patch{}, err
	}
	material, err := domain.Digest(map[string]any{"format_version": "0.1", "transaction_id": workspace.TransactionID, "base_oid": workspace.BaseOID, "patch_sha256": sha, "staged_tree_oid": tree, "changed_paths": paths})
	if err != nil {
		return domain.Patch{}, err
	}
	return domain.Patch{TransactionID: workspace.TransactionID, PatchPath: patchPath, PatchSHA256: sha, PatchSizeBytes: int64(len(patch)), StagedTreeOID: tree, ChangedPaths: paths, ApprovalMaterialDigest: material, GeneratedAt: time.Now().UTC()}, nil
}

func verifySourcePinned(workspace domain.Workspace) error {
	if workspace.SourceHeadRef == "" {
		return nil
	}
	current, err := gitText(workspace.RepositoryRoot, "rev-parse", "--verify", workspace.SourceHeadRef+"^{commit}")
	if err != nil {
		return err
	}
	if current != workspace.BaseOID {
		return fmt.Errorf("source ref is stale: expected %s got %s", workspace.BaseOID, current)
	}
	return nil
}

func materializedCommitEnvironment(at time.Time) []string {
	stamp := fmt.Sprintf("%d +0000", at.UTC().Unix())
	return append(gitEnv(),
		"GIT_AUTHOR_NAME=FutureDiff",
		"GIT_AUTHOR_EMAIL=futurediff@localhost",
		"GIT_COMMITTER_NAME=FutureDiff",
		"GIT_COMMITTER_EMAIL=futurediff@localhost",
		"GIT_AUTHOR_DATE="+stamp,
		"GIT_COMMITTER_DATE="+stamp,
	)
}

// PredictMaterializedRef creates the deterministic commit object that would be
// published for this transaction, but does not create any reference. Writing a
// content-addressed, unreachable Git object is safe before approval and allows
// provider effects to bind to the exact commit identity.
func (m Manager) PredictMaterializedRef(workspace domain.Workspace, patch domain.Patch) (domain.MaterializedRef, error) {
	raw, err := os.ReadFile(patch.PatchPath)
	if err != nil {
		return domain.MaterializedRef{}, err
	}
	if domain.SHA256Bytes(raw) != patch.PatchSHA256 {
		return domain.MaterializedRef{}, errors.New("stored patch digest mismatch")
	}
	args := []string{"commit-tree", patch.StagedTreeOID, "-p", workspace.BaseOID, "-m", "FutureDiff transaction " + workspace.TransactionID}
	cmd := gitCommand(workspace.RepositoryRoot, materializedCommitEnvironment(patch.GeneratedAt), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return domain.MaterializedRef{}, fmt.Errorf("git commit-tree: %w: %s", err, strings.TrimSpace(string(out)))
	}
	commit := strings.TrimSpace(string(out))
	if commit == "" {
		return domain.MaterializedRef{}, errors.New("git commit-tree returned an empty object id")
	}
	return domain.MaterializedRef{
		TransactionID:    workspace.TransactionID,
		RefName:          "refs/heads/futurediff/" + workspace.TransactionID,
		CommitOID:        commit,
		ResultingTreeOID: patch.StagedTreeOID,
		MaterializedAt:   patch.GeneratedAt.UTC(),
	}, nil
}

func (m Manager) Materialize(workspace domain.Workspace, patch domain.Patch, approvalDigest string) (domain.MaterializedRef, error) {
	if err := verifySourcePinned(workspace); err != nil {
		return domain.MaterializedRef{}, err
	}
	predicted, err := m.PredictMaterializedRef(workspace, patch)
	if err != nil {
		return domain.MaterializedRef{}, err
	}
	integrationRoot := filepath.Join(m.RuntimeRoot, "integrations", workspace.TransactionID)
	integration := filepath.Join(integrationRoot, "workspace")
	if err := os.MkdirAll(integrationRoot, 0o700); err != nil {
		return domain.MaterializedRef{}, err
	}
	defer os.RemoveAll(integrationRoot)
	if _, err := runGit(workspace.RepositoryRoot, "worktree", "add", "--detach", integration, workspace.BaseOID); err != nil {
		return domain.MaterializedRef{}, err
	}
	defer func() {
		_, _ = runGit(workspace.RepositoryRoot, "worktree", "remove", "--force", integration)
		_, _ = runGit(workspace.RepositoryRoot, "worktree", "prune", "--expire", "now")
	}()
	if _, err := runGit(integration, "apply", "--index", "--binary", "--whitespace=nowarn", patch.PatchPath); err != nil {
		return domain.MaterializedRef{}, err
	}
	tree, err := gitText(integration, "write-tree")
	if err != nil {
		return domain.MaterializedRef{}, err
	}
	if tree != patch.StagedTreeOID {
		return domain.MaterializedRef{}, fmt.Errorf("materialized tree mismatch: %s != %s", tree, patch.StagedTreeOID)
	}
	verifiedTree, err := gitText(workspace.RepositoryRoot, "show", "-s", "--format=%T", predicted.CommitOID)
	if err != nil {
		return domain.MaterializedRef{}, err
	}
	if verifiedTree != patch.StagedTreeOID {
		return domain.MaterializedRef{}, fmt.Errorf("predicted commit tree mismatch: %s != %s", verifiedTree, patch.StagedTreeOID)
	}
	zero := strings.Repeat("0", len(predicted.CommitOID))
	if _, err := runGit(workspace.RepositoryRoot, "update-ref", predicted.RefName, predicted.CommitOID, zero); err != nil {
		return domain.MaterializedRef{}, err
	}
	return predicted, nil
}

func (m Manager) InspectIntegrationRef(workspace domain.Workspace, patch domain.Patch) (domain.MaterializedRef, bool, error) {
	ref := "refs/heads/futurediff/" + workspace.TransactionID
	commit, err := gitText(workspace.RepositoryRoot, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return domain.MaterializedRef{}, false, nil
	}
	tree, err := gitText(workspace.RepositoryRoot, "show", "-s", "--format=%T", commit)
	if err != nil {
		return domain.MaterializedRef{}, false, err
	}
	if tree != patch.StagedTreeOID {
		return domain.MaterializedRef{}, true, errors.New("existing integration ref has unexpected tree")
	}
	return domain.MaterializedRef{TransactionID: workspace.TransactionID, RefName: ref, CommitOID: commit, ResultingTreeOID: tree, MaterializedAt: time.Now().UTC()}, true, nil
}

func (m Manager) Abort(workspace domain.Workspace) error {
	if workspace.WorkspacePath == "" {
		return nil
	}
	_, err := runGit(workspace.RepositoryRoot, "worktree", "remove", "--force", workspace.WorkspacePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	_, _ = runGit(workspace.RepositoryRoot, "worktree", "prune", "--expire", "now")
	return nil
}

func DecodeOID(s string) ([]byte, error) { return hex.DecodeString(s) }

func (m Manager) SourcePinned(workspace domain.Workspace) (bool, string, error) {
	if workspace.SourceHeadRef == "" {
		return true, workspace.BaseOID, nil
	}
	current, err := gitText(workspace.RepositoryRoot, "rev-parse", "--verify", workspace.SourceHeadRef+"^{commit}")
	if err != nil {
		return false, "", err
	}
	return current == workspace.BaseOID, current, nil
}
