package release

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateProvenanceIsSortedAndDigestBound(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "z.bin")
	b := filepath.Join(root, "a.bin")
	_ = os.WriteFile(a, []byte("z"), 0o600)
	_ = os.WriteFile(b, []byte("a"), 0o600)
	now := time.Unix(100, 0).UTC()
	stmt, err := GenerateProvenance(ProvenanceOptions{Artifacts: []string{a, b}, BuilderID: "builder", InvocationID: "run", StartedOn: now, FinishedOn: now})
	if err != nil {
		t.Fatal(err)
	}
	if stmt.PredicateType != "https://slsa.dev/provenance/v1" || stmt.Type != "https://in-toto.io/Statement/v1" {
		t.Fatalf("unexpected statement: %#v", stmt)
	}
	if stmt.Subject[0].Name != "a.bin" || stmt.Subject[1].Name != "z.bin" {
		t.Fatalf("subjects not sorted: %#v", stmt.Subject)
	}
	if len(stmt.Subject[0].Digest["sha256"]) != 64 {
		t.Fatal("missing artifact digest")
	}
}
