package verification

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SHOnnay/futurediff/internal/domain"
)

func TestDeterministicAssertions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Contract{
		FormatVersion: "0.1",
		ContractID:    "c",
		PolicyVersion: "p",
		Checks: []Check{{
			CheckID:  "exists",
			Required: true,
			Executor: "workspace_assertion",
			Type:     "file_exists",
			Path:     "ok.txt",
		}},
	}
	report, err := (Engine{}).Run("tx", domain.Workspace{WorkspacePath: root}, domain.Patch{PatchSHA256: "p", ApprovalMaterialDigest: "m"}, c)
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "pass" {
		t.Fatalf("outcome %s", report.Outcome)
	}
}

func TestCycleRejected(t *testing.T) {
	c := Contract{
		FormatVersion: "0.1",
		ContractID:    "c",
		PolicyVersion: "p",
		Checks: []Check{
			{CheckID: "a", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "a", DependsOn: []string{"b"}},
			{CheckID: "b", Executor: "workspace_assertion", Type: "file_exists", Path: "b", DependsOn: []string{"a"}},
		},
	}
	if err := Validate(c); err == nil {
		t.Fatal("cycle accepted")
	}
}
