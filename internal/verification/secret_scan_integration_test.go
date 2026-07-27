package verification

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/secretscan"
)

func TestSecretScanBlocksVerificationBeforeCommands(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patchPath := filepath.Join(root, "change.patch")
	patch := "--- a/config\n+++ b/config\n+token=ghp_abcdefghijklmnopqrstuvwxyz123456\n"
	if err := os.WriteFile(patchPath, []byte(patch), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := domain.Workspace{TransactionID: "tx", WorkspacePath: root, ArtifactsPath: filepath.Join(root, "artifacts")}
	p := domain.Patch{TransactionID: "tx", PatchPath: patchPath, PatchSHA256: domain.SHA256Bytes([]byte(patch)), ApprovalMaterialDigest: "material", GeneratedAt: time.Now()}
	contract := Contract{FormatVersion: "0.1", ContractID: "c", PolicyVersion: "p", Checks: []Check{{CheckID: "exists", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	report, err := (Engine{SecretScanner: secretscan.Default()}).Run("tx", workspace, p, contract)
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "fail" || len(report.Results) != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Results[0].CheckID != "futurediff.secret_scan" || report.Results[0].Status != "fail" {
		t.Fatalf("secret result: %#v", report.Results[0])
	}
	if report.Results[1].Status != "blocked" {
		t.Fatalf("downstream check should be blocked: %#v", report.Results[1])
	}
}
