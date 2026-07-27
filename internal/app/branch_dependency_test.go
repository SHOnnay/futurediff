package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/adapters/githubbranch"
	"github.com/SHOnnay/futurediff/internal/adapters/githubdraft"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

type branchRunner struct{ fake *appFakeGitHub }

func (r *branchRunner) LSRemote(_ context.Context, _, _, branch string, _ []byte) (string, bool, error) {
	r.fake.mu.Lock()
	defer r.fake.mu.Unlock()
	oid := r.fake.refs[branch]
	return oid, oid != "", nil
}
func (r *branchRunner) PushCreateOnly(_ context.Context, _, _, branch, oid string, _ []byte) error {
	r.fake.mu.Lock()
	defer r.fake.mu.Unlock()
	if r.fake.refs[branch] != "" {
		return os.ErrExist
	}
	r.fake.refs[branch] = oid
	return nil
}

func TestBranchPublicationPrecedesBoundDraftPR(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	_ = os.Mkdir(repoPath, 0o700)
	runGit(t, repoPath, "init", "-b", "main")
	_ = os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("current\n"), 0o600)
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "base")
	fake := &appFakeGitHub{refs: map[string]string{"main": strings.Repeat("b", 40)}, token: "secret"}
	store, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t.Setenv("FD_TEST_GITHUB_TOKEN", "secret")
	config := credentials.Config{Version: "0.1", Adapters: []credentials.AdapterIdentity{
		{ID: githubbranch.AdapterID, Version: githubbranch.AdapterVersion, TrustLevel: credentials.TrustBuiltIn, ExecutableDigest: "builtin:" + githubbranch.AdapterID, Enabled: true},
		{ID: githubdraft.AdapterID, Version: githubdraft.AdapterVersion, TrustLevel: credentials.TrustBuiltIn, ExecutableDigest: "builtin:" + githubdraft.AdapterID, Enabled: true},
	}, Credentials: []credentials.Binding{{ID: "github-main", Provider: "github", Source: credentials.SecretSourceRef{Kind: "environment", Reference: "FD_TEST_GITHUB_TOKEN"}, AllowedAdapters: []string{githubbranch.AdapterID, githubdraft.AdapterID}, AllowedOperations: []string{githubbranch.ReadOperation, githubbranch.CommitOperation, githubdraft.ReadOperation, githubdraft.StatusOperation, githubdraft.CommitOperation}, AllowedDestinations: []credentials.DestinationRule{{Scheme: "https", Host: "github.com", PathPrefix: "/acme/app.git"}, {Scheme: "https", Host: "api.github.com", PathPrefix: "/repos/acme/app"}}, Enabled: true}}}
	broker, err := credentials.NewBroker(config, credentials.EnvironmentSource{}, store, store)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}, Verifier: verification.Engine{}, Credentials: broker, GitHub: &githubdraft.Adapter{HTTPClient: &http.Client{Transport: fake}}, GitHubBranch: &githubbranch.Adapter{Runner: &branchRunner{fake: fake}}, CoordinatorID: "test"}
	created, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600)
	if _, err = svc.Seal(created.Transaction.ID); err != nil {
		t.Fatal(err)
	}
	branch, err := svc.PrepareGitHubBranch(context.Background(), created.Transaction.ID, PrepareGitHubBranchRequest{CredentialID: "github-main", Owner: "acme", Repo: "app", Branch: "futurediff/" + created.Transaction.ID, RemoteURL: "https://github.com/acme/app.git"})
	if err != nil {
		t.Fatal(err)
	}
	pr, err := svc.PrepareGitHubDraftPR(context.Background(), created.Transaction.ID, PrepareGitHubDraftPRRequest{CredentialID: "github-main", Input: githubdraft.Input{Owner: "acme", Repo: "app", Base: "main", Title: "Bound change", DependsOnEffectID: branch.EffectID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(pr.DependsOn) != 1 || pr.DependsOn[0] != branch.EffectID {
		t.Fatalf("dependencies=%v", pr.DependsOn)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "basic", PolicyVersion: "policy-test", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	if _, err = svc.Verify(created.Transaction.ID, contract); err != nil {
		t.Fatal(err)
	}
	mat, _ := svc.ApprovalMaterial(created.Transaction.ID)
	digest := mat["transaction_digest"]
	if _, err = svc.Approve(created.Transaction.ID, digest, "test"); err != nil {
		t.Fatal(err)
	}
	view, err := svc.CommitContext(context.Background(), created.Transaction.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if view.Transaction.Status != domain.StateCommitted || fake.postCalls != 1 {
		t.Fatalf("status=%s posts=%d", view.Transaction.Status, fake.postCalls)
	}
	if fake.refs["futurediff/"+created.Transaction.ID] == "" {
		t.Fatal("remote branch not published")
	}
}
