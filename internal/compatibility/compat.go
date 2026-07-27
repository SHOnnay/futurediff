package compatibility

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SHOnnay/futurediff/effectspec"
	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/configlint"
	"github.com/SHOnnay/futurediff/internal/policybundle"
	"github.com/SHOnnay/futurediff/internal/verification"
)

const Version = "0.1"

type ConfigEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}
type Manifest struct {
	FormatVersion         string        `json:"format_version"`
	APIContracts          []string      `json:"api_contracts,omitempty"`
	VerificationContracts []string      `json:"verification_contracts,omitempty"`
	EffectSpecDescriptors []string      `json:"effectspec_descriptors,omitempty"`
	PolicyBundles         []string      `json:"policy_bundles,omitempty"`
	Configs               []ConfigEntry `json:"configs,omitempty"`
}
type Check struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}
type Report struct {
	FormatVersion string  `json:"format_version"`
	Manifest      string  `json:"manifest"`
	Compatible    bool    `json:"compatible"`
	Passed        int     `json:"passed"`
	Failed        int     `json:"failed"`
	Checks        []Check `json:"checks"`
}

func Run(manifestPath string) (Report, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Report{}, err
	}
	var m Manifest
	if err := strict(data, &m); err != nil {
		return Report{}, err
	}
	if m.FormatVersion != Version {
		return Report{}, errors.New("unsupported compatibility manifest version")
	}
	total := len(m.APIContracts) + len(m.VerificationContracts) + len(m.EffectSpecDescriptors) + len(m.PolicyBundles) + len(m.Configs)
	if total == 0 || total > 100 {
		return Report{}, errors.New("manifest must contain between 1 and 100 checks")
	}
	base, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return Report{}, err
	}
	r := Report{FormatVersion: Version, Manifest: manifestPath, Compatible: true}
	add := func(kind, path string, fn func(string) error) {
		resolved, e := resolve(base, path)
		if e == nil {
			e = fn(resolved)
		}
		c := Check{Kind: kind, Path: path, Passed: e == nil}
		if e != nil {
			c.Message = e.Error()
			r.Failed++
			r.Compatible = false
		} else {
			c.Message = "compatible"
			r.Passed++
		}
		r.Checks = append(r.Checks, c)
	}
	for _, p := range m.APIContracts {
		add("api_contract", p, func(path string) error {
			var c apicontract.Contract
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			if e = strict(b, &c); e != nil {
				return e
			}
			if e = apicontract.Validate(c); e != nil {
				return e
			}
			d := apicontract.Diff(c, apicontract.Current())
			if !d.Compatible {
				return fmt.Errorf("current API is incompatible with baseline: %+v", d.Changes)
			}
			return nil
		})
	}
	for _, p := range m.VerificationContracts {
		add("verification_contract", p, func(path string) error {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			_, e = verification.Parse(b)
			return e
		})
	}
	for _, p := range m.EffectSpecDescriptors {
		add("effectspec_descriptor", p, func(path string) error {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			var d effectspec.Descriptor
			if e = strict(b, &d); e != nil {
				return e
			}
			return d.Validate()
		})
	}
	for _, p := range m.PolicyBundles {
		add("policy_bundle", p, func(path string) error { _, e := policybundle.Verify(path); return e })
	}
	for _, c := range m.Configs {
		entry := c
		add("config:"+entry.Kind, entry.Path, func(path string) error {
			report := configlint.Lint(path, entry.Kind)
			if !report.Valid {
				return fmt.Errorf("configuration invalid: %+v", report.Findings)
			}
			return nil
		})
	}
	sort.Slice(r.Checks, func(i, j int) bool {
		if r.Checks[i].Kind != r.Checks[j].Kind {
			return r.Checks[i].Kind < r.Checks[j].Kind
		}
		return r.Checks[i].Path < r.Checks[j].Path
	})
	if !r.Compatible {
		return r, errors.New("compatibility checks failed")
	}
	return r, nil
}
func resolve(base, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", errors.New("manifest paths must be non-empty and relative")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("manifest path traversal rejected")
	}
	p := filepath.Join(base, clean)
	candidate, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	prefix := base + string(filepath.Separator)
	if candidate != base && !strings.HasPrefix(candidate, prefix) {
		return "", errors.New("manifest path escaped base directory")
	}
	return candidate, nil
}
func strict(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e == nil {
		return errors.New("multiple JSON values are not allowed")
	} else if !errors.Is(e, io.EOF) {
		return e
	}
	return nil
}
