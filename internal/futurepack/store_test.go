package futurepack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreAndExport(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("future-evidence\n"), 4096)
	ref, err := store.PutBytes("verification.log", payload)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PutBytes("same.log", payload)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != ref.ID {
		t.Fatalf("dedupe identity mismatch: %s != %s", second.ID, ref.ID)
	}

	output := filepath.Join(t.TempDir(), "transaction.futurepack")
	manifest := Manifest{FormatVersion: "0.1", TransactionID: "tx_test", Verdict: "pass", Artifacts: []Ref{ref}}
	if err := store.Export(output, manifest); err != nil {
		t.Fatal(err)
	}

	archive, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := map[string][]byte{}
	for _, file := range archive.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = content
	}
	var decoded Manifest
	if err := json.Unmarshal(entries["manifest.json"], &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TransactionID != "tx_test" {
		t.Fatalf("unexpected transaction id: %s", decoded.TransactionID)
	}
	if !bytes.Equal(entries[ref.RelativePath], payload) {
		t.Fatal("artifact bytes differ")
	}
}

func TestReadRejectsTamperedArtifact(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.PutBytes("evidence", []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(ref.RelativePath)), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(ref); err == nil {
		t.Fatal("expected tamper detection")
	}
}
