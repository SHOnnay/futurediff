package repoadmission

import (
	"github.com/SHOnnay/futurediff/internal/staging"
	"path/filepath"
	"testing"
)

func TestPolicyContainment(t *testing.T) {
	root := t.TempDir()
	p := Policy{Version: Version, PolicyID: "p", AllowedRoots: []string{root}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	d := p.Evaluate(staging.InspectResult{RepositoryRoot: filepath.Join(root, "repo"), ObjectFormat: "sha1", SourceHeadRef: "refs/heads/main"}, staging.Reject)
	if !d.Allowed {
		t.Fatalf("%+v", d)
	}
	d = p.Evaluate(staging.InspectResult{RepositoryRoot: "/outside", ObjectFormat: "sha1", SourceHeadRef: "refs/heads/main"}, staging.Reject)
	if d.Allowed {
		t.Fatal("outside repository allowed")
	}
}
