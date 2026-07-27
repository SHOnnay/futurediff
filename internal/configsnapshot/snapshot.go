package configsnapshot

import (
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

const Version = "0.1"

type Input struct {
	Name     string
	Path     string
	Required bool
}

type Entry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Required bool   `json:"required"`
	Exists   bool   `json:"exists"`
	Mode     uint32 `json:"mode,omitempty"`
	Size     int64  `json:"size,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

type Manifest struct {
	Version     string    `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	Entries     []Entry   `json:"entries"`
	Digest      string    `json:"digest"`
}

type Check struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type Report struct {
	ManifestDigest string    `json:"manifest_digest"`
	VerifiedAt     time.Time `json:"verified_at"`
	Checks         []Check   `json:"checks"`
	Verified       bool      `json:"verified"`
}

func Build(inputs []Input, now time.Time) (Manifest, error) {
	if len(inputs) == 0 {
		return Manifest{}, errors.New("at least one configuration file is required")
	}
	seen := map[string]bool{}
	entries := make([]Entry, 0, len(inputs))
	for _, in := range inputs {
		name := strings.TrimSpace(in.Name)
		if name == "" || seen[name] {
			return Manifest{}, errors.New("configuration names must be non-empty and unique")
		}
		seen[name] = true
		path, err := canonicalPath(in.Path)
		if err != nil {
			return Manifest{}, fmt.Errorf("%s: %w", name, err)
		}
		entry, err := inspect(name, path, in.Required)
		if err != nil {
			return Manifest{}, err
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	m := Manifest{Version: Version, GeneratedAt: now.UTC().Truncate(time.Second), Entries: entries}
	m.Digest = digest(m)
	return m, nil
}

func Verify(m Manifest, now time.Time) (Report, error) {
	if err := validate(m); err != nil {
		return Report{}, err
	}
	r := Report{ManifestDigest: m.Digest, VerifiedAt: now.UTC().Truncate(time.Second), Verified: true}
	for _, expected := range m.Entries {
		actual, err := inspect(expected.Name, expected.Path, expected.Required)
		check := Check{Name: expected.Name, Path: expected.Path, Status: "pass"}
		if err != nil {
			check.Status, check.Reason, r.Verified = "fail", err.Error(), false
		} else if actual != expected {
			check.Status, check.Reason, r.Verified = "fail", describeMismatch(expected, actual), false
		}
		r.Checks = append(r.Checks, check)
	}
	return r, nil
}

func Write(path string, m Manifest) error {
	if err := validate(m); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func Load(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := strictJSON(b, &m); err != nil {
		return m, err
	}
	return m, validate(m)
}

func validate(m Manifest) error {
	if m.Version != Version || len(m.Entries) == 0 {
		return errors.New("invalid configuration snapshot")
	}
	seen := map[string]bool{}
	for _, e := range m.Entries {
		if e.Name == "" || !filepath.IsAbs(e.Path) || seen[e.Name] {
			return errors.New("invalid or duplicate configuration entry")
		}
		seen[e.Name] = true
	}
	if m.Digest == "" || m.Digest != digest(m) {
		return errors.New("configuration snapshot digest mismatch")
	}
	return nil
}

func inspect(name, path string, required bool) (Entry, error) {
	e := Entry{Name: name, Path: path, Required: required}
	st, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if required {
			return e, fmt.Errorf("required configuration file %q is missing", path)
		}
		return e, nil
	}
	if err != nil {
		return e, err
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		return e, fmt.Errorf("configuration path must be a regular non-symlink file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return e, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return e, err
	}
	e.Exists = true
	e.Mode = uint32(st.Mode().Perm())
	e.Size = st.Size()
	e.SHA256 = hex.EncodeToString(h.Sum(nil))
	return e, nil
}

func canonicalPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("configuration path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func digest(m Manifest) string {
	m.Digest = ""
	b, _ := json.Marshal(m)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func describeMismatch(expected, actual Entry) string {
	switch {
	case expected.Exists != actual.Exists:
		return fmt.Sprintf("existence changed: expected=%t actual=%t", expected.Exists, actual.Exists)
	case expected.Mode != actual.Mode:
		return fmt.Sprintf("mode changed: expected=%04o actual=%04o", expected.Mode, actual.Mode)
	case expected.Size != actual.Size:
		return fmt.Sprintf("size changed: expected=%d actual=%d", expected.Size, actual.Size)
	case expected.SHA256 != actual.SHA256:
		return "content digest changed"
	default:
		return "configuration metadata changed"
	}
}

func strictJSON(b []byte, v any) error {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err == nil {
		return errors.New("trailing JSON data")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
