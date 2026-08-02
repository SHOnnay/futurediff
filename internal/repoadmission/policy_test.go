package repoadmission

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/staging"
)

func TestPolicyContainment(t *testing.T) {
	root, in := testRepository(t)
	p := strictPolicy(t, root)
	if d := p.Evaluate(in, staging.Reject); !d.Allowed {
		t.Fatalf("expected repository to be admitted: %+v", d)
	}

	outside, outsideIn := testRepository(t)
	if canonicalPath(outside) == canonicalPath(root) {
		t.Fatal("test roots unexpectedly match")
	}
	if d := p.Evaluate(outsideIn, staging.Reject); d.Allowed || !containsCode(d, "repository_outside_allowed_roots") {
		t.Fatalf("outside repository admitted or wrong reason: %+v", d)
	}
}

func TestLegacyPolicyRemainsCompatible(t *testing.T) {
	root := t.TempDir()
	p := Policy{Version: legacyVersion, PolicyID: "legacy", AllowedRoots: []string{root}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	in := staging.InspectResult{
		RepositoryRoot: filepath.Join(root, "not-created"),
		ObjectFormat:   "sha1",
		SourceHeadRef:  "refs/heads/main",
	}
	if d := p.Evaluate(in, staging.Reject); !d.Allowed {
		t.Fatalf("legacy policy unexpectedly received 0.2 metadata checks: %+v", d)
	}
}

func TestStrictPolicyDefaults(t *testing.T) {
	root, in := testRepository(t)
	p := strictPolicy(t, root)
	if !reflect.DeepEqual(p.AllowedObjectFormats, []string{"sha1", "sha256"}) {
		t.Fatalf("object formats = %v", p.AllowedObjectFormats)
	}
	if !reflect.DeepEqual(p.AllowedRefFormats, []string{"files"}) {
		t.Fatalf("ref formats = %v", p.AllowedRefFormats)
	}
	if !reflect.DeepEqual(p.AllowedHeadPrefixes, []string{"refs/heads/"}) {
		t.Fatalf("head prefixes = %v", p.AllowedHeadPrefixes)
	}
	if d := p.Evaluate(in, staging.Reject); !d.Allowed || d.Facts.RefFormat != "files" {
		t.Fatalf("strict default rejected ordinary repository: %+v", d)
	}
}

func TestStrictPolicyRejectsMissingCommonDir(t *testing.T) {
	root, in := testRepository(t)
	p := strictPolicy(t, root)
	in.GitCommonDir = ""
	d := p.Evaluate(in, staging.Reject)
	if d.Allowed || !containsCode(d, "git_common_dir_missing") {
		t.Fatalf("missing common dir not rejected: %+v", d)
	}
}

func TestStrictPolicyRejectsCommonDirOutsideRootsAndLinkedWorktree(t *testing.T) {
	root, in := testRepository(t)
	p := strictPolicy(t, root)
	outside := t.TempDir()
	common := filepath.Join(outside, "common.git")
	makeGitCommonDir(t, common)
	in.GitCommonDir = common
	d := p.Evaluate(in, staging.Reject)
	for _, code := range []string{"git_common_dir_outside_allowed_roots", "linked_worktree_not_allowed"} {
		if !containsCode(d, code) {
			t.Fatalf("missing %s: %+v", code, d)
		}
	}
}

func TestStrictPolicyCanExplicitlyAllowLinkedWorktreeInsideRoot(t *testing.T) {
	root, in := testRepository(t)
	common := filepath.Join(root, ".git-shared")
	makeGitCommonDir(t, common)
	in.GitCommonDir = common
	p := strictPolicy(t, root)
	p.AllowLinkedWorktree = true
	if d := p.Evaluate(in, staging.Reject); !d.Allowed || !d.Facts.LinkedWorktree {
		t.Fatalf("explicit linked-worktree allowance failed: %+v", d)
	}
}

func TestStrictPolicyRejectsHistoryShapingMetadata(t *testing.T) {
	tests := []struct {
		name string
		code string
		add  func(t *testing.T, common string)
	}{
		{
			name: "shallow",
			code: "shallow_repository_not_allowed",
			add: func(t *testing.T, common string) {
				writeFile(t, filepath.Join(common, "shallow"), "0123456789012345678901234567890123456789\n", 0o600)
			},
		},
		{
			name: "loose replace ref",
			code: "replace_refs_not_allowed",
			add: func(t *testing.T, common string) {
				writeFile(t, filepath.Join(common, "refs", "replace", strings.Repeat("a", 40)), strings.Repeat("b", 40)+"\n", 0o600)
			},
		},
		{
			name: "packed replace ref",
			code: "replace_refs_not_allowed",
			add: func(t *testing.T, common string) {
				writeFile(t, filepath.Join(common, "packed-refs"), strings.Repeat("a", 40)+" refs/replace/"+strings.Repeat("b", 40)+"\n", 0o600)
			},
		},
		{
			name: "grafts",
			code: "grafts_not_allowed",
			add: func(t *testing.T, common string) {
				writeFile(t, filepath.Join(common, "info", "grafts"), strings.Repeat("a", 40)+"\n", 0o600)
			},
		},
		{
			name: "alternates",
			code: "alternate_object_database_not_allowed",
			add: func(t *testing.T, common string) {
				writeFile(t, filepath.Join(common, "objects", "info", "alternates"), "/tmp/other-objects\n", 0o600)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, in := testRepository(t)
			tt.add(t, in.GitCommonDir)
			p := strictPolicy(t, root)
			d := p.Evaluate(in, staging.Reject)
			if d.Allowed || !containsCode(d, tt.code) {
				t.Fatalf("metadata not rejected: %+v", d)
			}
		})
	}
}

func TestStrictPolicyRejectsSymlinkedObjectDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root, in := testRepository(t)
	if err := os.RemoveAll(filepath.Join(in.GitCommonDir, "objects")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "external-objects")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(in.GitCommonDir, "objects")); err != nil {
		t.Fatal(err)
	}
	p := strictPolicy(t, root)
	d := p.Evaluate(in, staging.Reject)
	if d.Allowed || !containsCode(d, "symlinked_object_directory_not_allowed") || !d.Facts.SymlinkedObjectDirectory {
		t.Fatalf("symlinked objects not rejected: %+v", d)
	}
}

func TestStrictPolicyRejectsReftableUnlessAllowed(t *testing.T) {
	root, in := testRepository(t)
	if err := os.MkdirAll(filepath.Join(in.GitCommonDir, "reftable"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := strictPolicy(t, root)
	if d := p.Evaluate(in, staging.Reject); d.Allowed || !containsCode(d, "ref_format_not_allowed") {
		t.Fatalf("reftable not rejected: %+v", d)
	}
	p.AllowedRefFormats = []string{"files", "reftable"}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if d := p.Evaluate(in, staging.Reject); !d.Allowed || d.Facts.RefFormat != "reftable" {
		t.Fatalf("explicit reftable allowance failed: %+v", d)
	}
}

func TestStrictPolicyRejectsNonLocalHead(t *testing.T) {
	root, in := testRepository(t)
	p := strictPolicy(t, root)
	in.SourceHeadRef = "refs/remotes/origin/main"
	d := p.Evaluate(in, staging.Reject)
	if d.Allowed || !containsCode(d, "head_ref_not_allowed") {
		t.Fatalf("non-local HEAD not rejected: %+v", d)
	}
}

func TestStrictPolicyHeadNamespaceCanBeNarrowed(t *testing.T) {
	root, in := testRepository(t)
	p := Policy{
		Version:             Version,
		PolicyID:            "narrow",
		AllowedRoots:        []string{root},
		AllowedHeadPrefixes: []string{"refs/heads/review/"},
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if d := p.Evaluate(in, staging.Reject); d.Allowed || !containsCode(d, "head_ref_not_allowed") {
		t.Fatalf("main admitted outside narrowed namespace: %+v", d)
	}
	in.SourceHeadRef = "refs/heads/review/change-1"
	if d := p.Evaluate(in, staging.Reject); !d.Allowed {
		t.Fatalf("permitted namespace rejected: %+v", d)
	}
}

func TestDecisionReasonsAreDeterministic(t *testing.T) {
	root, in := testRepository(t)
	p := strictPolicy(t, root)
	in.SourceHeadRef = "refs/remotes/origin/main"
	writeFile(t, filepath.Join(in.GitCommonDir, "shallow"), "x\n", 0o600)
	first := p.Evaluate(in, staging.StageFromHead)
	second := p.Evaluate(in, staging.StageFromHead)
	if !reflect.DeepEqual(first.ReasonCodes, second.ReasonCodes) || !reflect.DeepEqual(first.Reasons, second.Reasons) {
		t.Fatalf("nondeterministic decisions:\n%+v\n%+v", first, second)
	}
}

func TestLoadRejectsPolicySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "policy.json")
	writePolicyFile(t, target, root, 0o600)
	link := filepath.Join(root, "policy-link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("policy symlink accepted: %v", err)
	}
}

func TestLoadRejectsWritablePolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode check is not used on Windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, "policy.json")
	writePolicyFile(t, path, root, 0o600)
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "writable") {
		t.Fatalf("broadly writable policy accepted: %v", err)
	}
}

func TestLoadRejectsOversizedAndTrailingJSON(t *testing.T) {
	root := t.TempDir()
	oversized := filepath.Join(root, "oversized.json")
	writeFile(t, oversized, strings.Repeat("x", maxPolicySize+1), 0o600)
	if _, err := Load(oversized); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized policy accepted: %v", err)
	}

	trailing := filepath.Join(root, "trailing.json")
	base := Policy{Version: Version, PolicyID: "p", AllowedRoots: []string{root}}
	b, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, trailing, string(b)+" {}", 0o600)
	if _, err := Load(trailing); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing JSON accepted: %v", err)
	}
}
func TestLoadRejectsUnknownFields(t *testing.T) {
	root := t.TempDir()
	unknown := filepath.Join(root, "unknown-field.json")
	writeFile(t, unknown, `{"version":"0.2","policy_id":"p","allowed_roots":["`+root+`"],"unexpected_field":true}`, 0o600)
	if _, err := Load(unknown); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("policy with unknown field accepted: %v", err)
	}
}

func TestValidateRejectsUnavailableRootAndUnsafePrefix(t *testing.T) {
	p := Policy{Version: Version, PolicyID: "p", AllowedRoots: []string{filepath.Join(t.TempDir(), "missing")}}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("missing root accepted: %v", err)
	}

	root := t.TempDir()
	p = Policy{Version: Version, PolicyID: "p", AllowedRoots: []string{root}, AllowedHeadPrefixes: []string{"refs/heads/good/../bad"}}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("unsafe head prefix accepted: %v", err)
	}
}

func TestStableDefaultForPath(t *testing.T) {
	p, err := StableDefaultForPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != Version || p.PolicyID != "stable-default-v0.2" {
		t.Fatalf("unexpected stable policy identity: %+v", p)
	}
	if len(p.AllowedRoots) != 1 || !filepath.IsAbs(p.AllowedRoots[0]) {
		t.Fatalf("unexpected stable policy roots: %v", p.AllowedRoots)
	}
	if p.AllowDetachedHead || p.AllowStageFromHead || p.AllowReplaceRefs || p.AllowGrafts || p.AllowShallowRepository || p.AllowAlternateObjectDatabase {
		t.Fatalf("stable policy unexpectedly enabled an opt-out: %+v", p)
	}
}

func TestStrictPolicyRejectsMissingObjectDirectory(t *testing.T) {
	root, in := testRepository(t)
	if err := os.RemoveAll(filepath.Join(in.GitCommonDir, "objects")); err != nil {
		t.Fatal(err)
	}
	p := strictPolicy(t, root)
	d := p.Evaluate(in, staging.Reject)
	if d.Allowed || !containsCode(d, "git_objects_unavailable") {
		t.Fatalf("missing object directory not rejected: %+v", d)
	}
}

func TestStrictPolicyRejectsMalformedMetadata(t *testing.T) {
	tests := []struct {
		name string
		add  func(t *testing.T, common string)
	}{
		{
			name: "refs symlink",
			add: func(t *testing.T, common string) {
				if runtime.GOOS == "windows" {
					t.Skip("symlink privileges vary on Windows")
				}
				refs := filepath.Join(common, "refs")
				if err := os.RemoveAll(refs); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(filepath.Dir(common), "external-refs")
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, refs); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "shallow directory",
			add: func(t *testing.T, common string) {
				if err := os.MkdirAll(filepath.Join(common, "shallow"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "malformed packed refs",
			add: func(t *testing.T, common string) {
				writeFile(t, filepath.Join(common, "packed-refs"), "not-a-valid-packed-ref-line\n", 0o600)
			},
		},
		{
			name: "grafts directory",
			add: func(t *testing.T, common string) {
				if err := os.MkdirAll(filepath.Join(common, "info", "grafts"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "alternates directory",
			add: func(t *testing.T, common string) {
				if err := os.MkdirAll(filepath.Join(common, "objects", "info", "alternates"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, in := testRepository(t)
			tt.add(t, in.GitCommonDir)
			p := strictPolicy(t, root)
			d := p.Evaluate(in, staging.Reject)
			if d.Allowed || !containsCode(d, "git_metadata_invalid") {
				t.Fatalf("malformed metadata not rejected: %+v", d)
			}
		})
	}
}

func TestStrictPolicyRejectsSymlinkedReplaceNamespace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root, in := testRepository(t)
	target := filepath.Join(root, "replacement-refs")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	replaceRoot := filepath.Join(in.GitCommonDir, "refs", "replace")
	if err := os.Symlink(target, replaceRoot); err != nil {
		t.Fatal(err)
	}
	p := strictPolicy(t, root)
	d := p.Evaluate(in, staging.Reject)
	if d.Allowed || !containsCode(d, "git_metadata_invalid") {
		t.Fatalf("symlinked replace namespace not rejected: %+v", d)
	}
}

func strictPolicy(t *testing.T, root string) Policy {
	t.Helper()
	p, err := StableDefault(root)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func testRepository(t *testing.T) (string, staging.InspectResult) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	common := filepath.Join(repo, ".git")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	makeGitCommonDir(t, common)
	return root, staging.InspectResult{
		RepositoryRoot: repo,
		GitCommonDir:   common,
		ObjectFormat:   "sha1",
		SourceHeadRef:  "refs/heads/main",
	}
}

func makeGitCommonDir(t *testing.T, common string) {
	t.Helper()
	for _, dir := range []string{
		common,
		filepath.Join(common, "objects", "info"),
		filepath.Join(common, "refs", "heads"),
		filepath.Join(common, "info"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func writePolicyFile(t *testing.T, path, root string, mode os.FileMode) {
	t.Helper()
	p := Policy{Version: Version, PolicyID: "load-test", AllowedRoots: []string{root}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(b), mode)
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func containsCode(d Decision, want string) bool {
	for _, code := range d.ReasonCodes {
		if code == want {
			return true
		}
	}
	return false
}
