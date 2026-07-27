package repoadmission

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SHOnnay/futurediff/internal/staging"
)

const Version = "0.1"

func canonicalPath(path string) string {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return filepath.Clean(path)
	}
	for probe := abs; ; probe = filepath.Dir(probe) {
		if resolved, err := filepath.EvalSymlinks(probe); err == nil {
			if probe == abs {
				return resolved
			}
			rel, relErr := filepath.Rel(probe, abs)
			if relErr != nil || rel == "." {
				return resolved
			}
			return filepath.Join(resolved, rel)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return abs
		}
	}
}

type Policy struct {
	Version              string   `json:"version"`
	PolicyID             string   `json:"policy_id"`
	AllowedRoots         []string `json:"allowed_roots"`
	AllowedObjectFormats []string `json:"allowed_object_formats,omitempty"`
	AllowDetachedHead    bool     `json:"allow_detached_head"`
	AllowStageFromHead   bool     `json:"allow_stage_from_head"`
}

type Decision struct {
	Allowed        bool     `json:"allowed"`
	PolicyID       string   `json:"policy_id"`
	RepositoryRoot string   `json:"repository_root"`
	Reasons        []string `json:"reasons,omitempty"`
}

func Load(path string) (Policy, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Policy{}, err
	}
	if !st.Mode().IsRegular() {
		return Policy{}, errors.New("repository policy must be a regular file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var p Policy
	if err := d.Decode(&p); err != nil {
		return Policy{}, err
	}
	var extra any
	if err := d.Decode(&extra); err == nil {
		return Policy{}, errors.New("trailing JSON value rejected")
	}
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func (p *Policy) Validate() error {
	if p.Version != Version {
		return fmt.Errorf("unsupported repository policy version %q", p.Version)
	}
	if strings.TrimSpace(p.PolicyID) == "" {
		return errors.New("policy_id is required")
	}
	if len(p.AllowedRoots) == 0 {
		return errors.New("at least one allowed root is required")
	}
	roots := make([]string, 0, len(p.AllowedRoots))
	seen := map[string]bool{}
	for _, raw := range p.AllowedRoots {
		if !filepath.IsAbs(raw) {
			return fmt.Errorf("allowed root must be absolute: %s", raw)
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			return err
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		abs = filepath.Clean(abs)
		if !seen[abs] {
			seen[abs] = true
			roots = append(roots, abs)
		}
	}
	sort.Strings(roots)
	p.AllowedRoots = roots
	if len(p.AllowedObjectFormats) == 0 {
		p.AllowedObjectFormats = []string{"sha1", "sha256"}
	}
	formats := map[string]bool{}
	for _, f := range p.AllowedObjectFormats {
		if f != "sha1" && f != "sha256" {
			return fmt.Errorf("unsupported object format %q", f)
		}
		formats[f] = true
	}
	p.AllowedObjectFormats = p.AllowedObjectFormats[:0]
	for _, f := range []string{"sha1", "sha256"} {
		if formats[f] {
			p.AllowedObjectFormats = append(p.AllowedObjectFormats, f)
		}
	}
	return nil
}

func (p Policy) Evaluate(in staging.InspectResult, dirty staging.DirtyPolicy) Decision {
	repoRoot := canonicalPath(in.RepositoryRoot)
	d := Decision{PolicyID: p.PolicyID, RepositoryRoot: in.RepositoryRoot, Allowed: true}
	contained := false
	for _, root := range p.AllowedRoots {
		rel, err := filepath.Rel(root, repoRoot)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			contained = true
			break
		}
	}
	if !contained {
		d.Allowed = false
		d.Reasons = append(d.Reasons, "repository is outside every allowed root")
	}
	formatOK := false
	for _, f := range p.AllowedObjectFormats {
		if in.ObjectFormat == f {
			formatOK = true
		}
	}
	if !formatOK {
		d.Allowed = false
		d.Reasons = append(d.Reasons, "Git object format is not allowed")
	}
	if in.SourceHeadRef == "" && !p.AllowDetachedHead {
		d.Allowed = false
		d.Reasons = append(d.Reasons, "detached HEAD is not allowed")
	}
	if dirty == staging.StageFromHead && !p.AllowStageFromHead {
		d.Allowed = false
		d.Reasons = append(d.Reasons, "stage_from_head dirty policy is not allowed")
	}
	return d
}
