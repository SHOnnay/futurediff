package releaseverify

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	rel "github.com/SHOnnay/futurediff/internal/release"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyDirectory(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "futurediff")
	os.WriteFile(bin, []byte("binary"), 0o755)
	sbom := rel.SPDXDocument{SPDXVersion: "SPDX-2.3", Packages: []rel.SPDXPackage{{Name: "x"}}}
	b, _ := json.Marshal(sbom)
	os.WriteFile(filepath.Join(root, "futurediff.spdx.json"), b, 0o644)
	stmt, err := rel.GenerateProvenance(rel.ProvenanceOptions{Artifacts: []string{bin, filepath.Join(root, "futurediff.spdx.json")}, StartedOn: time.Now(), FinishedOn: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := rel.WriteProvenance(filepath.Join(root, "futurediff.intoto.jsonl"), stmt); err != nil {
		t.Fatal(err)
	}
	if err := rel.WriteChecksums(filepath.Join(root, "SHA256SUMS"), []string{bin, filepath.Join(root, "futurediff.spdx.json"), filepath.Join(root, "futurediff.intoto.jsonl")}); err != nil {
		t.Fatal(err)
	}
	r, err := Verify(Options{Source: root})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Verified {
		t.Fatalf("not verified: %#v", r.Checks)
	}
}
func TestRejectTraversalArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.tar.gz")
	f, _ := os.Create(path)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "../evil", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("x"))
	tw.Close()
	gz.Close()
	f.Close()
	_, err := Verify(Options{Source: path})
	if err == nil || !strings.Contains(err.Error(), "unsafe archive path") {
		t.Fatalf("unexpected err %v", err)
	}
}
