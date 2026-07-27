package backupcatalog

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

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

const Version = "0.1"
const Confirmation = "DELETE_VERIFIED_FUTUREDIFF_BACKUPS"

type Policy struct {
	Version            string `json:"version"`
	BackupRoot         string `json:"backup_root"`
	ApplyEnabled       bool   `json:"apply_enabled"`
	KeepLatest         int    `json:"keep_latest"`
	MinimumAgeHours    int64  `json:"minimum_age_hours"`
	MaximumDeleteBytes int64  `json:"maximum_delete_bytes,omitempty"`
}
type Entry struct {
	BackupID        string    `json:"backup_id"`
	PathDigest      string    `json:"path_digest"`
	SHA256          string    `json:"sha256"`
	SizeBytes       int64     `json:"size_bytes"`
	CreatedAt       time.Time `json:"created_at"`
	Exists          bool      `json:"exists"`
	Regular         bool      `json:"regular"`
	WithinRoot      bool      `json:"within_root"`
	DigestValid     bool      `json:"digest_valid"`
	SizeValid       bool      `json:"size_valid"`
	SQLiteIntegrity bool      `json:"sqlite_integrity"`
	Error           string    `json:"error,omitempty"`
}
type Candidate struct {
	BackupID   string    `json:"backup_id"`
	PathDigest string    `json:"path_digest"`
	SHA256     string    `json:"sha256"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
	path       string
}
type Report struct {
	Version      string      `json:"version"`
	Policy       Policy      `json:"policy"`
	PolicyDigest string      `json:"policy_digest"`
	CheckedAt    time.Time   `json:"checked_at"`
	Healthy      bool        `json:"healthy"`
	Entries      []Entry     `json:"entries"`
	Candidates   []Candidate `json:"candidates"`
	DeleteBytes  int64       `json:"delete_bytes"`
	WithinLimits bool        `json:"within_limits"`
	PlanDigest   string      `json:"plan_digest"`
}
type Result struct {
	AppliedAt    time.Time `json:"applied_at"`
	Deleted      int       `json:"deleted"`
	BytesRemoved int64     `json:"bytes_removed"`
	BackupIDs    []string  `json:"backup_ids"`
	PlanDigest   string    `json:"plan_digest"`
}

func Validate(p Policy) error {
	if p.Version != Version {
		return fmt.Errorf("unsupported backup-retention policy version %q", p.Version)
	}
	if !filepath.IsAbs(p.BackupRoot) {
		return errors.New("backup_root must be absolute")
	}
	if p.KeepLatest < 0 || p.MinimumAgeHours < 0 || p.MaximumDeleteBytes < 0 {
		return errors.New("backup retention limits must be non-negative")
	}
	return nil
}
func Load(path string) (Policy, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Policy{}, e
	}
	var p Policy
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e = d.Decode(&p); e != nil {
		return p, e
	}
	var extra any
	if e = d.Decode(&extra); e == nil {
		return p, errors.New("trailing JSON data")
	} else if !errors.Is(e, io.EOF) {
		return p, e
	}
	return p, Validate(p)
}
func digestText(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }

func canonicalPath(path string) string {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

func within(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "."
}
func inspect(root string, b ledger.BackupRecord) (Entry, string) {
	e := Entry{BackupID: b.BackupID, PathDigest: digestText(b.Path), SHA256: b.SHA256, SizeBytes: b.SizeBytes, CreatedAt: b.CreatedAt}
	clean := canonicalPath(b.Path)
	e.WithinRoot = within(root, clean)
	if !e.WithinRoot {
		e.Error = "backup path is outside configured root"
		return e, clean
	}
	st, err := os.Lstat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			e.Error = "backup file is missing"
		} else {
			e.Error = err.Error()
		}
		return e, clean
	}
	e.Exists = true
	e.Regular = st.Mode().IsRegular() && st.Mode()&os.ModeSymlink == 0
	if !e.Regular {
		e.Error = "backup is not a regular non-symlink file"
		return e, clean
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		e.Error = err.Error()
		return e, clean
	}
	sum := sha256.Sum256(data)
	e.DigestValid = hex.EncodeToString(sum[:]) == b.SHA256
	e.SizeValid = int64(len(data)) == b.SizeBytes
	if !e.DigestValid || !e.SizeValid {
		e.Error = "backup size or digest mismatch"
		return e, clean
	}
	tmp, err := os.CreateTemp("", "futurediff-backup-verify-*.db")
	if err != nil {
		e.Error = err.Error()
		return e, clean
	}
	tmpPath := tmp.Name()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	_ = tmp.Close()
	defer os.Remove(tmpPath)
	if err != nil {
		e.Error = err.Error()
		return e, clean
	}
	db, err := ledger.Open(tmpPath)
	if err != nil {
		e.Error = err.Error()
		return e, clean
	}
	err = db.IntegrityCheck()
	_ = db.Close()
	if err != nil {
		e.Error = err.Error()
		return e, clean
	}
	e.SQLiteIntegrity = true
	return e, clean
}
func Evaluate(repo *ledger.Repository, p Policy, now time.Time) (Report, error) {
	if e := Validate(p); e != nil {
		return Report{}, e
	}
	root := canonicalPath(p.BackupRoot)
	records, err := repo.Backups()
	if err != nil {
		return Report{}, err
	}
	pd, _ := domain.Digest(p)
	report := Report{Version: Version, Policy: p, PolicyDigest: pd, CheckedAt: now.UTC(), Healthy: true, WithinLimits: true}
	type inspected struct {
		entry Entry
		path  string
	}
	all := make([]inspected, 0, len(records))
	for _, b := range records {
		entry, path := inspect(root, b)
		all = append(all, inspected{entry, path})
		report.Entries = append(report.Entries, entry)
		if !(entry.Exists && entry.Regular && entry.WithinRoot && entry.DigestValid && entry.SizeValid && entry.SQLiteIntegrity) {
			report.Healthy = false
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].entry.CreatedAt.Equal(all[j].entry.CreatedAt) {
			return all[i].entry.BackupID > all[j].entry.BackupID
		}
		return all[i].entry.CreatedAt.After(all[j].entry.CreatedAt)
	})
	cutoff := now.UTC().Add(-time.Duration(p.MinimumAgeHours) * time.Hour)
	for i, item := range all {
		if i < p.KeepLatest || item.entry.CreatedAt.After(cutoff) {
			continue
		}
		if !(item.entry.Exists && item.entry.Regular && item.entry.WithinRoot && item.entry.DigestValid && item.entry.SizeValid && item.entry.SQLiteIntegrity) {
			continue
		}
		report.Candidates = append(report.Candidates, Candidate{BackupID: item.entry.BackupID, PathDigest: item.entry.PathDigest, SHA256: item.entry.SHA256, SizeBytes: item.entry.SizeBytes, CreatedAt: item.entry.CreatedAt, path: item.path})
		report.DeleteBytes += item.entry.SizeBytes
	}
	if p.MaximumDeleteBytes > 0 && report.DeleteBytes > p.MaximumDeleteBytes {
		report.WithinLimits = false
	}
	material := struct {
		Version      string      `json:"version"`
		PolicyDigest string      `json:"policy_digest"`
		CheckedAt    string      `json:"checked_at"`
		Candidates   []Candidate `json:"candidates"`
		DeleteBytes  int64       `json:"delete_bytes"`
	}{Version, pd, report.CheckedAt.Format(time.RFC3339Nano), report.Candidates, report.DeleteBytes}
	data, _ := json.Marshal(material)
	sum := sha256.Sum256(data)
	report.PlanDigest = hex.EncodeToString(sum[:])
	return report, nil
}
func Apply(repo *ledger.Repository, report Report, confirmation string, now time.Time) (Result, error) {
	if !report.Policy.ApplyEnabled {
		return Result{}, errors.New("backup policy does not allow apply")
	}
	if !report.Healthy {
		return Result{}, errors.New("backup catalog is unhealthy")
	}
	if !report.WithinLimits {
		return Result{}, errors.New("backup deletion exceeds maximum_delete_bytes")
	}
	if confirmation != Confirmation {
		return Result{}, errors.New("exact backup deletion confirmation is required")
	}
	result := Result{AppliedAt: now.UTC(), PlanDigest: report.PlanDigest}
	for _, c := range report.Candidates {
		entry, _ := inspect(canonicalPath(report.Policy.BackupRoot), ledger.BackupRecord{BackupID: c.BackupID, Path: c.path, SHA256: c.SHA256, SizeBytes: c.SizeBytes, CreatedAt: c.CreatedAt})
		if !(entry.DigestValid && entry.SizeValid && entry.SQLiteIntegrity && entry.WithinRoot && entry.Regular) {
			return result, fmt.Errorf("backup %s changed after planning", c.BackupID)
		}
		if err := os.Remove(c.path); err != nil {
			return result, err
		}
		if err := repo.DeleteBackupRecord(c.BackupID, c.SHA256); err != nil {
			return result, err
		}
		if err := repo.RecordBackupRetention("", c.BackupID, c.PathDigest, c.SizeBytes, report.PlanDigest, now.UTC()); err != nil {
			return result, err
		}
		result.Deleted++
		result.BytesRemoved += c.SizeBytes
		result.BackupIDs = append(result.BackupIDs, c.BackupID)
	}
	return result, nil
}
