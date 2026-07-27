package transactionsnapshot

import (
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/futurepack"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportAndVerify(t *testing.T) {
	root := t.TempDir()
	repo, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	tx := domain.Transaction{ID: "tx_export", Mode: "cooperative", PolicyVersion: "p1", CreatedAt: time.Now().UTC()}
	ws := domain.Workspace{TransactionID: tx.ID, RepositoryRoot: root, GitCommonDir: root, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(root, "workspace"), ArtifactsPath: filepath.Join(root, "artifacts"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}
	if _, err = repo.Create(ledger.CreateInput{Transaction: tx, Workspace: ws}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "tx.futurepack")
	report, err := Export(repo, tx.ID, out)
	if err != nil {
		t.Fatal(err)
	}
	if report.ArtifactCount != 1 {
		t.Fatalf("artifacts=%d", report.ArtifactCount)
	}
	manifest, err := futurepack.VerifyArchive(out)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.TransactionID != tx.ID {
		t.Fatalf("transaction=%s", manifest.TransactionID)
	}
}

func TestSanitizedJSONRedactsNestedCredentials(t *testing.T) {
	payload := map[string]any{"credential_id": "github-main", "input_json": "{\"access_token\":\"ghp_abcdefghijklmnopqrstuvwxyz123456\",\"title\":\"ok\"}", "message": "Authorization: Bearer abc.def.ghi"}
	b, err := sanitizedJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "ghp_") || strings.Contains(text, "abc.def.ghi") {
		t.Fatalf("secret leaked: %s", text)
	}
	if !strings.Contains(text, "github-main") {
		t.Fatalf("credential metadata unexpectedly removed: %s", text)
	}
}
