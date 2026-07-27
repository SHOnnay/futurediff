package releaseverify

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	rel "github.com/SHOnnay/futurediff/internal/release"
)

type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
	Skip Status = "skip"
)

type Check struct {
	ID     string `json:"id"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
}

type Report struct {
	Source   string  `json:"source"`
	Root     string  `json:"root"`
	Checks   []Check `json:"checks"`
	Verified bool    `json:"verified"`
}

type Options struct {
	Source                   string
	GHBinary                 string
	AttestationRepo          string
	RequireSignedAttestation bool
}

func Verify(o Options) (Report, error) {
	if o.Source == "" {
		return Report{}, errors.New("source required")
	}
	root := o.Source
	cleanup := func() {}
	st, err := os.Stat(o.Source)
	if err != nil {
		return Report{}, err
	}
	if !st.IsDir() {
		root, cleanup, err = extractArchive(o.Source)
		if err != nil {
			return Report{}, err
		}
	}
	defer cleanup()
	root, err = singleRoot(root)
	if err != nil {
		return Report{}, err
	}
	r := Report{Source: o.Source, Root: root}
	r.Checks = append(r.Checks, verifyChecksums(root)...)
	r.Checks = append(r.Checks, verifySBOM(root))
	r.Checks = append(r.Checks, verifyProvenance(root))
	if o.RequireSignedAttestation {
		r.Checks = append(r.Checks, verifySigned(o, root))
	} else {
		r.Checks = append(r.Checks, Check{ID: "signed_attestation", Status: Skip, Detail: "signed attestation verification was not requested"})
	}
	r.Verified = true
	for _, c := range r.Checks {
		if c.Status == Fail {
			r.Verified = false
		}
	}
	return r, nil
}

func verifyChecksums(root string) []Check {
	path := filepath.Join(root, "SHA256SUMS")
	f, err := os.Open(path)
	if err != nil {
		return []Check{{ID: "checksums_file", Status: Fail, Detail: err.Error()}}
	}
	defer f.Close()
	var checks []Check
	s := bufio.NewScanner(f)
	entries := 0
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			checks = append(checks, Check{ID: "checksum_format", Status: Fail, Detail: "invalid line: " + line})
			continue
		}
		name := strings.TrimPrefix(parts[1], "*")
		if !safeName(name) {
			checks = append(checks, Check{ID: "checksum_path", Status: Fail, Detail: "unsafe checksum path: " + name})
			continue
		}
		got, err := fileSHA(filepath.Join(root, name))
		if err != nil {
			checks = append(checks, Check{ID: "checksum_" + name, Status: Fail, Detail: err.Error()})
			continue
		}
		status := Pass
		if !strings.EqualFold(got, parts[0]) {
			status = Fail
		}
		checks = append(checks, Check{ID: "checksum_" + name, Status: status, Detail: "sha256=" + got})
		entries++
	}
	if err := s.Err(); err != nil {
		checks = append(checks, Check{ID: "checksum_read", Status: Fail, Detail: err.Error()})
	}
	if entries == 0 {
		checks = append(checks, Check{ID: "checksums_nonempty", Status: Fail, Detail: "no checksum entries"})
	} else {
		checks = append(checks, Check{ID: "checksums_nonempty", Status: Pass, Detail: fmt.Sprintf("entries=%d", entries)})
	}
	return checks
}

func verifySBOM(root string) Check {
	path := filepath.Join(root, "futurediff.spdx.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return Check{ID: "spdx_sbom", Status: Fail, Detail: err.Error()}
	}
	var doc rel.SPDXDocument
	if err := json.Unmarshal(b, &doc); err != nil {
		return Check{ID: "spdx_sbom", Status: Fail, Detail: err.Error()}
	}
	if doc.SPDXVersion != "SPDX-2.3" || len(doc.Packages) == 0 {
		return Check{ID: "spdx_sbom", Status: Fail, Detail: "invalid SPDX document"}
	}
	return Check{ID: "spdx_sbom", Status: Pass, Detail: fmt.Sprintf("packages=%d files=%d", len(doc.Packages), len(doc.Files))}
}

func verifyProvenance(root string) Check {
	path := filepath.Join(root, "futurediff.intoto.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		return Check{ID: "intoto_provenance", Status: Fail, Detail: err.Error()}
	}
	var stmt rel.InTotoStatement
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(b))), &stmt); err != nil {
		return Check{ID: "intoto_provenance", Status: Fail, Detail: err.Error()}
	}
	if stmt.Type != "https://in-toto.io/Statement/v1" || stmt.PredicateType != "https://slsa.dev/provenance/v1" {
		return Check{ID: "intoto_provenance", Status: Fail, Detail: "unexpected statement or predicate type"}
	}
	for _, sub := range stmt.Subject {
		if !safeName(sub.Name) {
			return Check{ID: "intoto_provenance", Status: Fail, Detail: "unsafe subject name"}
		}
		got, err := fileSHA(filepath.Join(root, sub.Name))
		if err != nil {
			return Check{ID: "intoto_provenance", Status: Fail, Detail: "subject " + sub.Name + ": " + err.Error()}
		}
		if !strings.EqualFold(got, sub.Digest["sha256"]) {
			return Check{ID: "intoto_provenance", Status: Fail, Detail: "subject digest mismatch: " + sub.Name}
		}
	}
	return Check{ID: "intoto_provenance", Status: Pass, Detail: fmt.Sprintf("subjects=%d", len(stmt.Subject))}
}

func verifySigned(o Options, root string) Check {
	if o.AttestationRepo == "" {
		return Check{ID: "signed_attestation", Status: Fail, Detail: "attestation repository required"}
	}
	bin := o.GHBinary
	if bin == "" {
		bin = "gh"
	}
	artifact := filepath.Join(filepath.Dir(root), filepath.Base(root)+".tar.gz")
	if _, err := os.Stat(artifact); err != nil {
		artifact = o.Source
	}
	cmd := exec.Command(bin, "attestation", "verify", artifact, "--repo", o.AttestationRepo)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Check{ID: "signed_attestation", Status: Fail, Detail: safeOutput(out, err)}
	}
	return Check{ID: "signed_attestation", Status: Pass, Detail: "GitHub attestation verified"}
}

func extractArchive(path string) (string, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return "", func() {}, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", func() {}, err
	}
	defer gz.Close()
	dir, err := os.MkdirTemp("", "futurediff-release-verify-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			cleanup()
			return "", func() {}, fmt.Errorf("unsupported archive entry type for %s", h.Name)
		}
		clean := filepath.Clean(h.Name)
		if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			cleanup()
			return "", func() {}, fmt.Errorf("unsafe archive path %q", h.Name)
		}
		target := filepath.Join(dir, clean)
		relp, err := filepath.Rel(dir, target)
		if err != nil || strings.HasPrefix(relp, "..") {
			cleanup()
			return "", func() {}, fmt.Errorf("archive path escapes root")
		}
		if h.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				cleanup()
				return "", func() {}, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			cleanup()
			return "", func() {}, err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
		_, copyErr := io.CopyN(out, tr, h.Size)
		closeErr := out.Close()
		if copyErr != nil {
			cleanup()
			return "", func() {}, copyErr
		}
		if closeErr != nil {
			cleanup()
			return "", func() {}, closeErr
		}
	}
	return dir, cleanup, nil
}

func singleRoot(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(root, entries[0].Name()), nil
	}
	return root, nil
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func safeName(n string) bool {
	return n != "" && !filepath.IsAbs(n) && filepath.Base(n) == n && n != "." && n != ".."
}

func safeOutput(out []byte, err error) string {
	s := strings.TrimSpace(string(out))
	if len(s) > 512 {
		s = s[:512]
	}
	if s == "" {
		s = err.Error()
	}
	return s
}

func WriteJSON(path string, r Report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(b)
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func Summary(r Report) string {
	states := map[Status]int{}
	for _, c := range r.Checks {
		states[c.Status]++
	}
	keys := []string{string(Pass), string(Fail), string(Skip)}
	sort.Strings(keys)
	return fmt.Sprintf("verified=%t pass=%d fail=%d skip=%d", r.Verified, states[Pass], states[Fail], states[Skip])
}
