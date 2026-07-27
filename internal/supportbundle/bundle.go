package supportbundle

import (
	"archive/zip"
	"bytes"
	"context"
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

	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/doctor"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

type Options struct {
	DataRoot         string
	Socket           string
	Runtime          string
	CredentialConfig string
}
type ManifestEntry struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type Manifest struct {
	FormatVersion string          `json:"format_version"`
	GeneratedAt   time.Time       `json:"generated_at"`
	Entries       []ManifestEntry `json:"entries"`
}

func Create(ctx context.Context, output string, opts Options) (Manifest, error) {
	if output == "" || opts.DataRoot == "" {
		return Manifest{}, errors.New("output and data root are required")
	}
	repo, err := ledger.OpenRepository(filepath.Join(opts.DataRoot, "ledger.db"))
	if err != nil {
		return Manifest{}, err
	}
	audit, err := repo.Audit()
	if err != nil {
		repo.Close()
		return Manifest{}, err
	}
	metrics, err := repo.Metrics()
	_ = repo.Close()
	if err != nil {
		return Manifest{}, err
	}
	d := doctor.Run(ctx, doctor.Options{DataRoot: opts.DataRoot, Socket: opts.Socket, CredentialConfig: opts.CredentialConfig, Runtime: opts.Runtime})
	docs := map[string]any{"build.json": buildinfo.Current(), "doctor.json": d, "audit.json": audit, "metrics.json": metrics, "api-contract.json": apicontract.Current()}
	home, _ := os.UserHomeDir()
	replacements := map[string]string{opts.DataRoot: "<FUTUREDIFF_ROOT>", home: "<HOME>"}
	payloads := map[string][]byte{}
	names := make([]string, 0, len(docs))
	for n := range docs {
		names = append(names, n)
	}
	sort.Strings(names)
	manifest := Manifest{FormatVersion: "0.1", GeneratedAt: time.Now().UTC()}
	for _, name := range names {
		raw, _ := json.MarshalIndent(docs[name], "", "  ")
		text := string(raw)
		for from, to := range replacements {
			if from != "" {
				text = strings.ReplaceAll(text, from, to)
			}
		}
		b := []byte(text + "\n")
		payloads[name] = b
		s := sha256.Sum256(b)
		manifest.Entries = append(manifest.Entries, ManifestEntry{Name: name, SHA256: hex.EncodeToString(s[:]), Size: int64(len(b))})
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	payloads["manifest.json"] = append(mb, '\n')
	if err := writeZipAtomic(output, payloads); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}
func writeZipAtomic(output string, files map[string][]byte) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(output), ".support-*.zip")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	zw := zip.NewWriter(tmp)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		h := &zip.FileHeader{Name: n, Method: zip.Deflate}
		h.SetModTime(time.Unix(0, 0).UTC())
		w, e := zw.CreateHeader(h)
		if e != nil {
			return e
		}
		if _, e = w.Write(files[n]); e != nil {
			return e
		}
	}
	if err = zw.Close(); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, output)
}
func Verify(path string) (Manifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Manifest{}, err
	}
	defer zr.Close()
	files := map[string][]byte{}
	for _, f := range zr.File {
		if strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") {
			return Manifest{}, errors.New("unsafe bundle path")
		}
		if f.UncompressedSize64 > 4<<20 {
			return Manifest{}, errors.New("bundle entry too large")
		}
		r, e := f.Open()
		if e != nil {
			return Manifest{}, e
		}
		b, e := io.ReadAll(io.LimitReader(r, 4<<20+1))
		r.Close()
		if e != nil {
			return Manifest{}, e
		}
		if _, ok := files[f.Name]; ok {
			return Manifest{}, errors.New("duplicate bundle entry")
		}
		files[f.Name] = b
	}
	mb, ok := files["manifest.json"]
	if !ok {
		return Manifest{}, errors.New("manifest missing")
	}
	var m Manifest
	if err = json.Unmarshal(mb, &m); err != nil {
		return Manifest{}, err
	}
	for _, e := range m.Entries {
		b, ok := files[e.Name]
		if !ok {
			return Manifest{}, fmt.Errorf("missing %s", e.Name)
		}
		s := sha256.Sum256(b)
		if hex.EncodeToString(s[:]) != e.SHA256 || int64(len(b)) != e.Size {
			return Manifest{}, fmt.Errorf("digest mismatch for %s", e.Name)
		}
	}
	return m, nil
}
func ContainsForbidden(path string, patterns ...string) (bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer zr.Close()
	for _, file := range zr.File {
		r, err := file.Open()
		if err != nil {
			return false, err
		}
		data, err := io.ReadAll(io.LimitReader(r, 4<<20+1))
		_ = r.Close()
		if err != nil {
			return false, err
		}
		for _, pattern := range patterns {
			if pattern != "" && bytes.Contains(data, []byte(pattern)) {
				return true, nil
			}
		}
	}
	return false, nil
}
