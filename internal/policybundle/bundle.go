package policybundle

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

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/verification"
)

const Version = "0.1"

var epoch = time.Unix(0, 0).UTC()

type Manifest struct {
	FormatVersion  string   `json:"format_version"`
	PolicyID       string   `json:"policy_id"`
	PolicyVersion  string   `json:"policy_version"`
	ContractDigest string   `json:"contract_digest"`
	BundleDigest   string   `json:"bundle_digest"`
	Labels         []string `json:"labels,omitempty"`
}

type Bundle struct {
	Manifest Manifest
	Contract verification.Contract
}

func Build(contract verification.Contract, policyID string, labels []string, output string) (Manifest, error) {
	if err := verification.Validate(contract); err != nil {
		return Manifest{}, err
	}
	policyID = strings.TrimSpace(policyID)
	if policyID == "" || output == "" {
		return Manifest{}, errors.New("policy id and output are required")
	}
	labels = normalizeLabels(labels)
	contractBytes, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	contractBytes = append(contractBytes, '\n')
	cd, _ := domain.Digest(contract)
	provisional := Manifest{FormatVersion: Version, PolicyID: policyID, PolicyVersion: contract.PolicyVersion, ContractDigest: cd, Labels: labels}
	bd, _ := domain.Digest(map[string]any{"format_version": Version, "policy_id": policyID, "policy_version": contract.PolicyVersion, "contract_digest": cd, "labels": labels})
	provisional.BundleDigest = bd
	manifestBytes, _ := json.MarshalIndent(provisional, "", "  ")
	manifestBytes = append(manifestBytes, '\n')
	if err := writeZip(output, map[string][]byte{"manifest.json": manifestBytes, "verification-contract.json": contractBytes}); err != nil {
		return Manifest{}, err
	}
	if _, err := Verify(output); err != nil {
		return Manifest{}, err
	}
	return provisional, nil
}

func Verify(path string) (Bundle, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return Bundle{}, err
	}
	defer r.Close()
	if len(r.File) != 2 {
		return Bundle{}, fmt.Errorf("policy bundle must contain exactly two files")
	}
	files := map[string][]byte{}
	for _, f := range r.File {
		if f.Name != "manifest.json" && f.Name != "verification-contract.json" {
			return Bundle{}, fmt.Errorf("unexpected bundle entry %q", f.Name)
		}
		if f.Mode()&os.ModeSymlink != 0 || f.UncompressedSize64 > 4<<20 {
			return Bundle{}, errors.New("unsafe policy bundle entry")
		}
		rc, err := f.Open()
		if err != nil {
			return Bundle{}, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, 4<<20+1))
		_ = rc.Close()
		if err != nil {
			return Bundle{}, err
		}
		files[f.Name] = b
	}
	var m Manifest
	if err := strict(files["manifest.json"], &m); err != nil {
		return Bundle{}, err
	}
	if m.FormatVersion != Version || m.PolicyID == "" || m.PolicyVersion == "" {
		return Bundle{}, errors.New("invalid policy manifest")
	}
	c, err := verification.Parse(files["verification-contract.json"])
	if err != nil {
		return Bundle{}, err
	}
	if c.PolicyVersion != m.PolicyVersion {
		return Bundle{}, errors.New("policy version mismatch")
	}
	cd, _ := domain.Digest(c)
	if cd != m.ContractDigest {
		return Bundle{}, errors.New("contract digest mismatch")
	}
	labels := normalizeLabels(m.Labels)
	bd, _ := domain.Digest(map[string]any{"format_version": Version, "policy_id": m.PolicyID, "policy_version": m.PolicyVersion, "contract_digest": cd, "labels": labels})
	if bd != m.BundleDigest {
		return Bundle{}, errors.New("bundle digest mismatch")
	}
	m.Labels = labels
	return Bundle{Manifest: m, Contract: c}, nil
}
func normalizeLabels(in []string) []string {
	set := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" {
			set[v] = true
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func writeZip(path string, files map[string][]byte) error {
	if !strings.HasSuffix(path, ".fdpolicy") {
		return errors.New("policy bundle must use .fdpolicy extension")
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	names := []string{"manifest.json", "verification-contract.json"}
	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o600)
		h.SetModTime(epoch)
		w, err := zw.CreateHeader(h)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		if _, err = w.Write(files[name]); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err = zw.Close(); err == nil {
		err = f.Close()
	} else {
		_ = f.Close()
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err = os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
func strict(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
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
func FileSHA256(path string) (string, error) {
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}
