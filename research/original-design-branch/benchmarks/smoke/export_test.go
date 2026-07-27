package smoke

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
)

func TestExportFileChangeFailureBundle(t *testing.T) {
	outputDir := t.TempDir()
	result, err := Runner{}.ExportFileChangeFailureBundle(context.Background(), outputDir)
	if err != nil {
		t.Fatalf("export file change failure bundle: %v", err)
	}
	if _, err := os.Stat(result.FuturepackPath); err != nil {
		t.Fatalf("expected futurepack output: %v", err)
	}
	if result.Metrics.TaskCompletionRate != 1 {
		t.Fatalf("expected task completion rate 1, got %f", result.Metrics.TaskCompletionRate)
	}
	if result.Metrics.SuccessfulAbortRate != 1 {
		t.Fatalf("expected successful abort rate 1, got %f", result.Metrics.SuccessfulAbortRate)
	}

	archive, err := zip.OpenReader(result.FuturepackPath)
	if err != nil {
		t.Fatalf("open futurepack: %v", err)
	}
	defer archive.Close()

	foundManifest := false
	foundArtifacts := 0
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
		if file.Name == "manifest.json" {
			foundManifest = true
			var manifest map[string]any
			if err := json.Unmarshal(content, &manifest); err != nil {
				t.Fatalf("decode manifest: %v", err)
			}
			if manifest["scenario"] != "file-change-failure" {
				t.Fatalf("unexpected scenario: %#v", manifest["scenario"])
			}
		}
		if len(file.Name) > len("artifacts/") && file.Name[:len("artifacts/")] == "artifacts/" {
			foundArtifacts++
		}
	}
	if !foundManifest {
		t.Fatal("expected manifest.json in futurepack")
	}
	if foundArtifacts < 2 {
		t.Fatalf("expected at least two artifact entries, got %d", foundArtifacts)
	}
}
