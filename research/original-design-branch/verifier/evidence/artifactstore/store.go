package artifactstore

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Ref struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"size_bytes"`
	RelativePath string `json:"relative_path"`
}

type Manifest struct {
	FormatVersion string         `json:"format_version"`
	RunID         string         `json:"run_id"`
	Scenario      string         `json:"scenario"`
	Verdict       string         `json:"verdict"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Metrics       any            `json:"metrics,omitempty"`
	Artifacts     []Ref          `json:"artifacts"`
}

type Store struct {
	Root        string
	artifactDir string
}

func Open(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("artifact store root is required")
	}
	artifactDir := filepath.Join(root, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact store: %w", err)
	}
	return &Store{Root: root, artifactDir: artifactDir}, nil
}

func (s *Store) PutBytes(name string, payload []byte) (Ref, error) {
	hash := sha256.Sum256(payload)
	sum := hex.EncodeToString(hash[:])
	slug := sanitizeName(name)
	relativePath := filepath.Join("artifacts", sum[:2], sum+"-"+slug)
	absolutePath := filepath.Join(s.Root, relativePath)
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return Ref{}, fmt.Errorf("create artifact directory: %w", err)
	}
	if err := os.WriteFile(absolutePath, payload, 0o644); err != nil {
		return Ref{}, fmt.Errorf("write artifact: %w", err)
	}
	return Ref{
		ID:           sum,
		Name:         name,
		SHA256:       sum,
		SizeBytes:    int64(len(payload)),
		RelativePath: relativePath,
	}, nil
}

func (s *Store) PutFile(name, sourcePath string) (Ref, error) {
	payload, err := os.ReadFile(sourcePath)
	if err != nil {
		return Ref{}, fmt.Errorf("read source artifact: %w", err)
	}
	return s.PutBytes(name, payload)
}

func (s *Store) Read(ref Ref) ([]byte, error) {
	payload, err := os.ReadFile(filepath.Join(s.Root, ref.RelativePath))
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	return payload, nil
}

func (s *Store) ExportFuturepack(outputPath string, manifest Manifest) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create futurepack directory: %w", err)
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create futurepack: %w", err)
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)
	defer zipWriter.Close()

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := writeZipEntry(zipWriter, "manifest.json", manifestBytes); err != nil {
		return err
	}

	for _, ref := range manifest.Artifacts {
		payload, err := s.Read(ref)
		if err != nil {
			return err
		}
		if err := writeZipEntry(zipWriter, ref.RelativePath, payload); err != nil {
			return err
		}
	}

	if err := zipWriter.Close(); err != nil {
		return fmt.Errorf("close futurepack: %w", err)
	}
	return nil
}

func writeZipEntry(zipWriter *zip.Writer, path string, payload []byte) error {
	writer, err := zipWriter.Create(path)
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", path, err)
	}
	if _, err := io.Copy(writer, bytes.NewReader(payload)); err != nil {
		return fmt.Errorf("write zip entry %s: %w", path, err)
	}
	return nil
}

func sanitizeName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "artifact.bin"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-", "\n", "-")
	cleaned := replacer.Replace(trimmed)
	if cleaned == "" {
		return "artifact.bin"
	}
	return cleaned
}
