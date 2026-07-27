package release

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSPDX(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := GenerateSPDX(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if doc.SPDXVersion != "SPDX-2.3" || len(doc.Files) != 1 || len(doc.Relationships) != 2 {
		t.Fatalf("unexpected SPDX document: %+v", doc)
	}
}

func TestWriteChecksums(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	if err := os.WriteFile(a, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "SHA256SUMS")
	if err := WriteChecksums(out, []string{a}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	if len(data) == 0 {
		t.Fatal("empty checksum file")
	}
}
