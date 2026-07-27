package artifactstore

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLargeArtifactPersistsAcrossReopenAndExport(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	payload := bytes.Repeat([]byte("0123456789abcdef"), 128*1024)
	ref, err := store.PutBytes("verification.log", payload)
	if err != nil {
		t.Fatalf("put bytes: %v", err)
	}
	if ref.SizeBytes != int64(len(payload)) {
		t.Fatalf("unexpected size: %d", ref.SizeBytes)
	}
	if !strings.HasPrefix(ref.RelativePath, "artifacts/") {
		t.Fatalf("expected artifact path under artifacts/, got %s", ref.RelativePath)
	}
	if _, err := os.Stat(filepath.Join(root, ref.RelativePath)); err != nil {
		t.Fatalf("expected artifact file on disk: %v", err)
	}

	reopened, err := Open(root)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	restored, err := reopened.Read(ref)
	if err != nil {
		t.Fatalf("read restored artifact: %v", err)
	}
	if !bytes.Equal(restored, payload) {
		t.Fatal("restored payload mismatch")
	}

	futurepackPath := filepath.Join(root, "exports", "evidence.futurepack")
	manifest := Manifest{
		FormatVersion: "0.1",
		RunID:         "run_artifact_store",
		Scenario:      "artifact-store-spike",
		Verdict:       "pass",
		Metadata: map[string]any{
			"artifact_store_root": root,
		},
		Artifacts: []Ref{ref},
	}
	if err := reopened.ExportFuturepack(futurepackPath, manifest); err != nil {
		t.Fatalf("export futurepack: %v", err)
	}

	archive, err := zip.OpenReader(futurepackPath)
	if err != nil {
		t.Fatalf("open futurepack: %v", err)
	}
	defer archive.Close()

	entries := map[string][]byte{}
	for _, file := range archive.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", file.Name, err)
		}
		entries[file.Name] = content
	}
	manifestBytes, ok := entries["manifest.json"]
	if !ok {
		t.Fatal("expected manifest.json in futurepack")
	}
	var decoded Manifest
	if err := json.Unmarshal(manifestBytes, &decoded); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if decoded.RunID != manifest.RunID {
		t.Fatalf("unexpected manifest run id: %s", decoded.RunID)
	}
	artifactBytes, ok := entries[ref.RelativePath]
	if !ok {
		t.Fatalf("expected artifact entry %s in futurepack", ref.RelativePath)
	}
	if !bytes.Equal(artifactBytes, payload) {
		t.Fatal("futurepack artifact payload mismatch")
	}
}
