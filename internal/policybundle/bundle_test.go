package policybundle

import (
	"github.com/SHOnnay/futurediff/internal/verification"
	"os"
	"path/filepath"
	"testing"
)

func sample() verification.Contract {
	return verification.Contract{FormatVersion: "0.1", ContractID: "safe", PolicyVersion: "policy-1", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
}
func TestBuildVerifyDeterministic(t *testing.T) {
	d := t.TempDir()
	a := filepath.Join(d, "a.fdpolicy")
	b := filepath.Join(d, "b.fdpolicy")
	if _, e := Build(sample(), "default", []string{"release", "safe", "safe"}, a); e != nil {
		t.Fatal(e)
	}
	if _, e := Build(sample(), "default", []string{"safe", "release"}, b); e != nil {
		t.Fatal(e)
	}
	ab, _ := os.ReadFile(a)
	bb, _ := os.ReadFile(b)
	if string(ab) != string(bb) {
		t.Fatal("bundle is not deterministic")
	}
	bundle, e := Verify(a)
	if e != nil {
		t.Fatal(e)
	}
	if bundle.Manifest.PolicyID != "default" {
		t.Fatal("policy id")
	}
}
func TestTamperRejected(t *testing.T) {
	if _, e := Build(sample(), "x", nil, filepath.Join(t.TempDir(), "x.zip")); e == nil {
		t.Fatal("wrong extension accepted")
	}
}
