package repoadmission

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"github.com/SHOnnay/futurediff/internal/staging"
)

const (
	// Version is the current repository-admission policy version. Version 0.2
	// enables stable-default checks for repository metadata that can alter or
	// truncate the history FutureDiff reviews.
	Version = "0.2"

	legacyVersion       = "0.1"
	maxPolicySize       = 1 << 20  // 1 MiB
	maxMetadataFileSize = 64 << 20 // 64 MiB
	maxMetadataLineSize = 1 << 20  // 1 MiB
)

// StableDefault returns the stable repository-admission policy. The caller
// supplies the filesystem roots from which source repositories may be admitted.
// Every opt-out remains false so history-shaping metadata and unsupported
// repository shapes fail closed.
func StableDefault(allowedRoots ...string) (Policy, error) {
	p := Policy{
		Version:      Version,
		PolicyID:     "stable-default-v0.2",
		AllowedRoots: append([]string(nil), allowedRoots...),
	}
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

// StableDefaultForPath creates the stable policy for the filesystem volume
// containing path. It is used by the daemon when no narrower custom policy is
// supplied. A custom policy should be preferred when operators can enumerate
// dedicated source roots.
func StableDefaultForPath(path string) (Policy, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Policy{}, fmt.Errorf("resolve stable repository policy root: %w", err)
	}
	volume := filepath.VolumeName(abs)
	allowedRoot := string(filepath.Separator)
	if volume != "" {
		allowedRoot = volume + string(filepath.Separator)
	}
	return StableDefault(allowedRoot)
}

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
	AllowedRefFormats    []string `json:"allowed_ref_formats,omitempty"`
	AllowedHeadPrefixes  []string `json:"allowed_head_prefixes,omitempty"`

	AllowDetachedHead             bool `json:"allow_detached_head"`
	AllowStageFromHead            bool `json:"allow_stage_from_head"`
	AllowGitCommonDirOutsideRoots bool `json:"allow_git_common_dir_outside_roots,omitempty"`
	AllowLinkedWorktree           bool `json:"allow_linked_worktree,omitempty"`
	AllowNonLocalHeadRef          bool `json:"allow_non_local_head_ref,omitempty"`
	AllowShallowRepository        bool `json:"allow_shallow_repository,omitempty"`
	AllowReplaceRefs              bool `json:"allow_replace_refs,omitempty"`
	AllowGrafts                   bool `json:"allow_grafts,omitempty"`
	AllowAlternateObjectDatabase  bool `json:"allow_alternate_object_database,omitempty"`
	AllowSymlinkedObjectDirectory bool `json:"allow_symlinked_object_directory,omitempty"`
}

type RepositoryFacts struct {
	GitCommonDir             string `json:"git_common_dir,omitempty"`
	RefFormat                string `json:"ref_format,omitempty"`
	LinkedWorktree           bool   `json:"linked_worktree"`
	Shallow                  bool   `json:"shallow"`
	ReplaceRefs              bool   `json:"replace_refs"`
	Grafts                   bool   `json:"grafts"`
	AlternateObjectDatabase  bool   `json:"alternate_object_database"`
	SymlinkedObjectDirectory bool   `json:"symlinked_object_directory"`
}

type Decision struct {
	Allowed        bool            `json:"allowed"`
	PolicyID       string          `json:"policy_id"`
	RepositoryRoot string          `json:"repository_root"`
	ReasonCodes    []string        `json:"reason_codes,omitempty"`
	Reasons        []string        `json:"reasons,omitempty"`
	Facts          RepositoryFacts `json:"facts"`
}

func Load(path string) (Policy, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return Policy{}, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return Policy{}, errors.New("repository policy symlink is not allowed")
	}
	if !st.Mode().IsRegular() {
		return Policy{}, errors.New("repository policy must be a regular file")
	}
	if st.Size() > maxPolicySize {
		return Policy{}, fmt.Errorf("repository policy exceeds %d bytes", maxPolicySize)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o022 != 0 {
		return Policy{}, errors.New("repository policy must not be group- or world-writable")
	}

	f, err := os.Open(path)
	if err != nil {
		return Policy{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return Policy{}, err
	}
	if !os.SameFile(st, opened) {
		return Policy{}, errors.New("repository policy changed while it was opened")
	}
	if !opened.Mode().IsRegular() || opened.Size() > maxPolicySize {
		return Policy{}, errors.New("repository policy changed to an unsafe file")
	}
	if runtime.GOOS != "windows" && opened.Mode().Perm()&0o022 != 0 {
		return Policy{}, errors.New("repository policy must not be group- or world-writable")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxPolicySize+1))
	if err != nil {
		return Policy{}, err
	}
	if len(b) > maxPolicySize {
		return Policy{}, fmt.Errorf("repository policy exceeds %d bytes", maxPolicySize)
	}

	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var p Policy
	if err := d.Decode(&p); err != nil {
		return Policy{}, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			return Policy{}, errors.New("trailing JSON value rejected")
		}
		return Policy{}, fmt.Errorf("invalid trailing JSON: %w", err)
	}
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

func (p *Policy) Validate() error {
	if p.Version != Version && p.Version != legacyVersion {
		return fmt.Errorf("unsupported repository policy version %q", p.Version)
	}
	p.PolicyID = strings.TrimSpace(p.PolicyID)
	if p.PolicyID == "" {
		return errors.New("policy_id is required")
	}
	if len(p.PolicyID) > 128 || strings.IndexFunc(p.PolicyID, unicode.IsControl) >= 0 {
		return errors.New("policy_id must be at most 128 bytes and contain no control characters")
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
		root := canonicalPath(raw)
		if p.Version == Version {
			st, err := os.Stat(root)
			if err != nil {
				return fmt.Errorf("allowed root %q is unavailable: %w", root, err)
			}
			if !st.IsDir() {
				return fmt.Errorf("allowed root %q is not a directory", root)
			}
		}
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	p.AllowedRoots = roots

	formats, err := normalizeAllowlist(p.AllowedObjectFormats, []string{"sha1", "sha256"}, map[string]bool{"sha1": true, "sha256": true}, "object format")
	if err != nil {
		return err
	}
	p.AllowedObjectFormats = formats

	if p.Version == Version {
		refFormats, err := normalizeAllowlist(p.AllowedRefFormats, []string{"files"}, map[string]bool{"files": true, "reftable": true}, "reference format")
		if err != nil {
			return err
		}
		p.AllowedRefFormats = refFormats

		if len(p.AllowedHeadPrefixes) == 0 {
			p.AllowedHeadPrefixes = []string{"refs/heads/"}
		}
		prefixes := make([]string, 0, len(p.AllowedHeadPrefixes))
		seenPrefixes := map[string]bool{}
		for _, prefix := range p.AllowedHeadPrefixes {
			if err := validateHeadPrefix(prefix); err != nil {
				return err
			}
			if !seenPrefixes[prefix] {
				seenPrefixes[prefix] = true
				prefixes = append(prefixes, prefix)
			}
		}
		sort.Strings(prefixes)
		p.AllowedHeadPrefixes = prefixes
	}
	return nil
}

func normalizeAllowlist(values, defaults []string, supported map[string]bool, label string) ([]string, error) {
	if len(values) == 0 {
		values = append([]string(nil), defaults...)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if !supported[value] {
			return nil, fmt.Errorf("unsupported %s %q", label, raw)
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out, nil
}

func validateHeadPrefix(prefix string) error {
	if !strings.HasPrefix(prefix, "refs/heads/") {
		return fmt.Errorf("allowed head prefix must begin with refs/heads/: %q", prefix)
	}
	if strings.IndexFunc(prefix, unicode.IsControl) >= 0 ||
		strings.ContainsAny(prefix, " ~^:?*[\\") ||
		strings.Contains(prefix, "..") ||
		strings.Contains(prefix, "//") ||
		strings.Contains(prefix, "@{") ||
		strings.HasSuffix(prefix, ".") ||
		strings.HasSuffix(prefix, ".lock") {
		return fmt.Errorf("invalid allowed head prefix %q", prefix)
	}
	return nil
}

func (p Policy) Evaluate(in staging.InspectResult, dirty staging.DirtyPolicy) Decision {
	repoRoot := canonicalPath(in.RepositoryRoot)
	d := Decision{
		PolicyID:       p.PolicyID,
		RepositoryRoot: in.RepositoryRoot,
		Allowed:        true,
		Facts: RepositoryFacts{
			GitCommonDir: canonicalPath(in.GitCommonDir),
		},
	}

	if !pathWithinAny(p.AllowedRoots, repoRoot) {
		d.deny("repository_outside_allowed_roots", "repository is outside every allowed root")
	}
	if !stringAllowed(p.AllowedObjectFormats, in.ObjectFormat) {
		d.deny("object_format_not_allowed", "Git object format is not allowed")
	}
	if in.SourceHeadRef == "" && !p.AllowDetachedHead {
		d.deny("detached_head_not_allowed", "detached HEAD is not allowed")
	}
	if dirty == staging.StageFromHead && !p.AllowStageFromHead {
		d.deny("stage_from_head_not_allowed", "stage_from_head dirty policy is not allowed")
	}

	// Version 0.1 remains loadable for compatibility. Stable-default metadata
	// checks are intentionally activated only by version 0.2.
	if p.Version != Version {
		return d
	}

	if in.RepositoryRoot == "" {
		d.deny("repository_root_missing", "repository root is missing")
	} else if st, err := os.Stat(repoRoot); err != nil {
		d.deny("repository_root_unavailable", "repository root is unavailable")
	} else if !st.IsDir() {
		d.deny("repository_root_not_directory", "repository root is not a directory")
	}

	commonDir := d.Facts.GitCommonDir
	if strings.TrimSpace(in.GitCommonDir) == "" {
		d.deny("git_common_dir_missing", "Git common directory is missing")
		return d
	}
	commonInfo, err := os.Lstat(in.GitCommonDir)
	if err != nil {
		d.deny("git_common_dir_unavailable", "Git common directory is unavailable")
		return d
	}
	if commonInfo.Mode()&os.ModeSymlink != 0 {
		d.deny("git_common_dir_symlink", "Git common directory must not be a symlink")
	}
	resolvedInfo, err := os.Stat(commonDir)
	if err != nil || !resolvedInfo.IsDir() {
		d.deny("git_common_dir_not_directory", "Git common directory is not a directory")
		return d
	}
	if !p.AllowGitCommonDirOutsideRoots && !pathWithinAny(p.AllowedRoots, commonDir) {
		d.deny("git_common_dir_outside_allowed_roots", "Git common directory is outside every allowed root")
	}

	expectedCommon := canonicalPath(filepath.Join(repoRoot, ".git"))
	d.Facts.LinkedWorktree = commonDir != expectedCommon
	if d.Facts.LinkedWorktree && !p.AllowLinkedWorktree {
		d.deny("linked_worktree_not_allowed", "linked worktree source repositories are not allowed")
	}

	refFormat, err := detectRefFormat(commonDir)
	if err != nil {
		d.deny("git_metadata_invalid", "Git reference storage metadata is unavailable or malformed")
	} else {
		d.Facts.RefFormat = refFormat
		if !stringAllowed(p.AllowedRefFormats, refFormat) {
			d.deny("ref_format_not_allowed", "Git reference format is not allowed")
		}
	}
	if in.SourceHeadRef != "" && !p.AllowNonLocalHeadRef && !hasAllowedPrefix(p.AllowedHeadPrefixes, in.SourceHeadRef) {
		d.deny("head_ref_not_allowed", "source HEAD must be a permitted local branch reference")
	}

	shallow, err := regularMetadataPresence(filepath.Join(commonDir, "shallow"), false)
	if err != nil {
		d.deny("git_metadata_invalid", "Git shallow metadata is unavailable or malformed")
	} else {
		d.Facts.Shallow = shallow
		if shallow && !p.AllowShallowRepository {
			d.deny("shallow_repository_not_allowed", "shallow repositories are not allowed")
		}
	}

	replaceRefs, err := hasReplaceRefs(commonDir)
	if err != nil {
		d.deny("git_metadata_invalid", "Git replacement-reference metadata is unavailable or malformed")
	} else {
		d.Facts.ReplaceRefs = replaceRefs
		if replaceRefs && !p.AllowReplaceRefs {
			d.deny("replace_refs_not_allowed", "Git replacement references are not allowed")
		}
	}

	grafts, err := regularMetadataPresence(filepath.Join(commonDir, "info", "grafts"), true)
	if err != nil {
		d.deny("git_metadata_invalid", "Git graft metadata is unavailable or malformed")
	} else {
		d.Facts.Grafts = grafts
		if grafts && !p.AllowGrafts {
			d.deny("grafts_not_allowed", "Git grafts are not allowed")
		}
	}

	objectsPath := filepath.Join(commonDir, "objects")
	objectsInfo, objectsErr := os.Lstat(objectsPath)
	if objectsErr != nil {
		d.deny("git_objects_unavailable", "Git object directory is unavailable")
		return d
	}
	if objectsInfo.Mode()&os.ModeSymlink != 0 {
		d.Facts.SymlinkedObjectDirectory = true
		if !p.AllowSymlinkedObjectDirectory {
			d.deny("symlinked_object_directory_not_allowed", "symlinked Git object directories are not allowed")
		}
	}
	resolvedObjects, err := os.Stat(objectsPath)
	if err != nil || !resolvedObjects.IsDir() {
		d.deny("git_objects_not_directory", "Git object directory is not a directory")
		return d
	}
	alternates, err := regularMetadataPresence(filepath.Join(objectsPath, "info", "alternates"), true)
	if err != nil {
		d.deny("git_metadata_invalid", "Git alternate-object metadata is unavailable or malformed")
	} else {
		d.Facts.AlternateObjectDatabase = alternates
		if alternates && !p.AllowAlternateObjectDatabase {
			d.deny("alternate_object_database_not_allowed", "alternate Git object databases are not allowed")
		}
	}
	return d
}

func (d *Decision) deny(code, reason string) {
	d.Allowed = false
	d.ReasonCodes = append(d.ReasonCodes, code)
	d.Reasons = append(d.Reasons, reason)
}

func pathWithinAny(roots []string, candidate string) bool {
	for _, root := range roots {
		rel, err := filepath.Rel(root, candidate)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func stringAllowed(allowed []string, value string) bool {
	for _, item := range allowed {
		if item == value {
			return true
		}
	}
	return false
}

func hasAllowedPrefix(prefixes []string, ref string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(ref, prefix) {
			return true
		}
	}
	return false
}

func detectRefFormat(commonDir string) (string, error) {
	reftablePath := filepath.Join(commonDir, "reftable")
	st, err := os.Lstat(reftablePath)
	if err == nil {
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return "", fmt.Errorf("%s must be a real directory", reftablePath)
		}
		return "reftable", nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	refsPath := filepath.Join(commonDir, "refs")
	st, err = os.Lstat(refsPath)
	if err != nil {
		return "", err
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
		return "", fmt.Errorf("%s must be a real directory", refsPath)
	}
	return "files", nil
}

func regularMetadataPresence(path string, requireNonEmpty bool) (bool, error) {
	st, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !st.Mode().IsRegular() {
		return false, fmt.Errorf("%s must be a regular file", path)
	}
	if st.Size() > maxMetadataFileSize {
		return false, fmt.Errorf("%s exceeds %d bytes", path, maxMetadataFileSize)
	}
	if requireNonEmpty {
		return st.Size() > 0, nil
	}
	return true, nil
}

func hasReplaceRefs(commonDir string) (bool, error) {
	replaceRoot := filepath.Join(commonDir, "refs", "replace")
	found, err := treeHasEntry(replaceRoot)
	if err != nil || found {
		return found, err
	}

	packedPath := filepath.Join(commonDir, "packed-refs")
	present, err := regularMetadataPresence(packedPath, false)
	if err != nil || !present {
		return false, err
	}
	f, err := os.Open(packedPath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	limited := io.LimitReader(f, maxMetadataFileSize+1)
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64*1024), maxMetadataLineSize)
	var consumed int64
	for scanner.Scan() {
		consumed += int64(len(scanner.Bytes())) + 1
		if consumed > maxMetadataFileSize {
			return false, fmt.Errorf("%s exceeds %d bytes", packedPath, maxMetadataFileSize)
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return false, fmt.Errorf("malformed packed reference line")
		}
		if strings.HasPrefix(fields[1], "refs/replace/") {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func treeHasEntry(root string) (bool, error) {
	st, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
		return false, fmt.Errorf("%s must be a real directory", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return true, nil
		}
		if entry.IsDir() {
			found, err := treeHasEntry(filepath.Join(root, entry.Name()))
			if err != nil {
				return false, err
			}
			if found {
				return true, nil
			}
			continue
		}
		return true, nil
	}
	return false, nil
}
