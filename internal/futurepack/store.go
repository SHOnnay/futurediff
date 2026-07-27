package futurepack

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Ref identifies an immutable content-addressed artifact.
type Ref struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"size_bytes"`
	RelativePath string `json:"relative_path"`
}

// Manifest is the portable evidence index stored in a .futurepack archive.
type Manifest struct {
	FormatVersion string         `json:"format_version"`
	TransactionID string         `json:"transaction_id"`
	Scenario      string         `json:"scenario,omitempty"`
	Verdict       string         `json:"verdict,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Metrics       any            `json:"metrics,omitempty"`
	Artifacts     []Ref          `json:"artifacts"`
}

// Store keeps immutable evidence blobs under a content-addressed directory.
type Store struct {
	root string
}

func Open(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("futurepack store root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve futurepack root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(abs, "artifacts", "sha256"), 0o700); err != nil {
		return nil, fmt.Errorf("create futurepack store: %w", err)
	}
	return &Store{root: abs}, nil
}

func (s *Store) PutBytes(name string, payload []byte) (Ref, error) {
	if s == nil || s.root == "" {
		return Ref{}, errors.New("futurepack store is not initialized")
	}
	digest := sha256.Sum256(payload)
	sum := hex.EncodeToString(digest[:])
	rel := filepath.Join("artifacts", "sha256", sum[:2], sum)
	abs := filepath.Join(s.root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return Ref{}, fmt.Errorf("create artifact directory: %w", err)
	}

	file, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	switch {
	case err == nil:
		if _, writeErr := file.Write(payload); writeErr != nil {
			file.Close()
			_ = os.Remove(abs)
			return Ref{}, fmt.Errorf("write artifact: %w", writeErr)
		}
		if syncErr := file.Sync(); syncErr != nil {
			file.Close()
			_ = os.Remove(abs)
			return Ref{}, fmt.Errorf("sync artifact: %w", syncErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			_ = os.Remove(abs)
			return Ref{}, fmt.Errorf("close artifact: %w", closeErr)
		}
	case errors.Is(err, os.ErrExist):
		existing, readErr := os.ReadFile(abs)
		if readErr != nil {
			return Ref{}, fmt.Errorf("read existing artifact: %w", readErr)
		}
		existingDigest := sha256.Sum256(existing)
		if existingDigest != digest {
			return Ref{}, errors.New("content-addressed artifact path contains mismatched bytes")
		}
	default:
		return Ref{}, fmt.Errorf("create artifact: %w", err)
	}

	return Ref{
		ID:           sum,
		Name:         sanitizeName(name),
		SHA256:       sum,
		SizeBytes:    int64(len(payload)),
		RelativePath: filepath.ToSlash(rel),
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
	if err := validateRef(ref); err != nil {
		return nil, err
	}
	abs := filepath.Join(s.root, filepath.FromSlash(ref.RelativePath))
	payload, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return nil, errors.New("artifact digest mismatch")
	}
	return payload, nil
}

func (s *Store) Export(outputPath string, manifest Manifest) error {
	if strings.TrimSpace(manifest.FormatVersion) == "" {
		return errors.New("futurepack format version is required")
	}
	if strings.TrimSpace(manifest.TransactionID) == "" {
		return errors.New("futurepack transaction id is required")
	}

	refs := append([]Ref(nil), manifest.Artifacts...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].RelativePath < refs[j].RelativePath })
	manifest.Artifacts = refs
	for _, ref := range refs {
		if err := validateRef(ref); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return fmt.Errorf("create futurepack output directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(outputPath), ".futurepack-*.tmp")
	if err != nil {
		return fmt.Errorf("create futurepack temporary file: %w", err)
	}
	tempName := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempName)
		}
	}()

	zw := zip.NewWriter(temp)
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = zw.Close()
		return fmt.Errorf("encode futurepack manifest: %w", err)
	}
	if err := writeEntry(zw, "manifest.json", manifestBytes); err != nil {
		_ = zw.Close()
		return err
	}
	for _, ref := range refs {
		payload, readErr := s.Read(ref)
		if readErr != nil {
			_ = zw.Close()
			return readErr
		}
		if err := writeEntry(zw, ref.RelativePath, payload); err != nil {
			_ = zw.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close futurepack archive: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync futurepack archive: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close futurepack file: %w", err)
	}
	if err := os.Rename(tempName, outputPath); err != nil {
		return fmt.Errorf("publish futurepack archive: %w", err)
	}
	committed = true
	return nil
}

func validateRef(ref Ref) error {
	if len(ref.SHA256) != 64 || ref.ID != ref.SHA256 {
		return errors.New("invalid artifact digest identity")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref.RelativePath)))
	if clean != ref.RelativePath || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return errors.New("unsafe artifact relative path")
	}
	expected := "artifacts/sha256/" + ref.SHA256[:2] + "/" + ref.SHA256
	if clean != expected {
		return errors.New("artifact path does not match digest")
	}
	return nil
}

func writeEntry(zw *zip.Writer, name string, payload []byte) error {
	header := &zip.FileHeader{Name: filepath.ToSlash(name), Method: zip.Deflate}
	header.SetMode(0o600)
	header.Modified = time.Unix(0, 0).UTC()
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create futurepack entry %s: %w", name, err)
	}
	if _, err := io.Copy(writer, bytes.NewReader(payload)); err != nil {
		return fmt.Errorf("write futurepack entry %s: %w", name, err)
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

// VerifyArchive validates a .futurepack archive without extracting it.
func VerifyArchive(path string) (Manifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open futurepack: %w", err)
	}
	defer zr.Close()
	var manifest Manifest
	seen := map[string]bool{}
	blobs := map[string][]byte{}
	for _, file := range zr.File {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.Name)))
		if clean != file.Name || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || file.FileInfo().Mode()&os.ModeType != 0 {
			return Manifest{}, fmt.Errorf("unsafe futurepack entry %q", file.Name)
		}
		if seen[clean] {
			return Manifest{}, fmt.Errorf("duplicate futurepack entry %q", clean)
		}
		seen[clean] = true
		if file.UncompressedSize64 > 64<<20 {
			return Manifest{}, fmt.Errorf("futurepack entry too large: %s", clean)
		}
		rc, openErr := file.Open()
		if openErr != nil {
			return Manifest{}, openErr
		}
		payload, readErr := io.ReadAll(io.LimitReader(rc, (64<<20)+1))
		closeErr := rc.Close()
		if readErr != nil {
			return Manifest{}, readErr
		}
		if closeErr != nil {
			return Manifest{}, closeErr
		}
		if len(payload) > 64<<20 {
			return Manifest{}, fmt.Errorf("futurepack entry too large: %s", clean)
		}
		if clean == "manifest.json" {
			if err := json.Unmarshal(payload, &manifest); err != nil {
				return Manifest{}, fmt.Errorf("decode manifest: %w", err)
			}
		} else {
			blobs[clean] = payload
		}
	}
	if manifest.FormatVersion == "" || manifest.TransactionID == "" {
		return Manifest{}, errors.New("futurepack manifest is incomplete")
	}
	expected := map[string]bool{"manifest.json": true}
	referenceDigests := map[string]bool{}
	for _, ref := range manifest.Artifacts {
		if referenceDigests[ref.SHA256] {
			return Manifest{}, fmt.Errorf("duplicate artifact reference %s", ref.SHA256)
		}
		referenceDigests[ref.SHA256] = true
		if err := validateRef(ref); err != nil {
			return Manifest{}, err
		}
		expected[ref.RelativePath] = true
		payload, ok := blobs[ref.RelativePath]
		if !ok {
			return Manifest{}, fmt.Errorf("missing artifact %s", ref.RelativePath)
		}
		sum := sha256.Sum256(payload)
		if hex.EncodeToString(sum[:]) != ref.SHA256 || int64(len(payload)) != ref.SizeBytes {
			return Manifest{}, fmt.Errorf("artifact verification failed: %s", ref.Name)
		}
	}
	for name := range seen {
		if !expected[name] {
			return Manifest{}, fmt.Errorf("unreferenced futurepack entry %s", name)
		}
	}
	return manifest, nil
}
