package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/evidencecrypto"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
)

func TestPersistRuntimeEvidenceEncrypted(t *testing.T) {
	key, err := evidencecrypto.Generate(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key.json")
	if err := evidencecrypto.WriteKey(path, key); err != nil {
		t.Fatal(err)
	}
	c, err := evidencecrypto.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	result := runtimeoci.Result{Stdout: []byte("super-secret-output"), Stderr: []byte("err"), Evidence: runtimeoci.Evidence{ExecutionID: "exec1", TransactionID: "tx1"}}
	rec, err := persistRuntimeEvidence(t.TempDir(), result, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{rec.StdoutPath, rec.StderrPath, rec.EvidencePath} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) == "super-secret-output" || !evidencecrypto.IsEncrypted(p) {
			t.Fatalf("not encrypted: %s", p)
		}
	}
	got, err := c.ReadFile(rec.StdoutPath, []byte("tx1:exec1:stdout"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "super-secret-output" {
		t.Fatalf("stdout=%q", got)
	}
}
