package storageguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const Version = "0.1"

type Policy struct {
	Version             string  `json:"version"`
	MinimumFreeBytes    int64   `json:"minimum_free_bytes,omitempty"`
	MinimumFreePercent  float64 `json:"minimum_free_percent,omitempty"`
	MaximumLedgerBytes  int64   `json:"maximum_ledger_bytes,omitempty"`
	MaximumRuntimeBytes int64   `json:"maximum_runtime_bytes,omitempty"`
}
type Filesystem struct {
	TotalBytes  int64   `json:"total_bytes"`
	FreeBytes   int64   `json:"free_bytes"`
	FreePercent float64 `json:"free_percent"`
}
type Status struct {
	Version      string     `json:"version"`
	CheckedAt    time.Time  `json:"checked_at"`
	Healthy      bool       `json:"healthy"`
	Filesystem   Filesystem `json:"filesystem"`
	LedgerBytes  int64      `json:"ledger_bytes"`
	RuntimeBytes int64      `json:"runtime_bytes"`
	Findings     []string   `json:"findings,omitempty"`
	Policy       Policy     `json:"policy"`
}
type Probe interface {
	Inspect(root, ledgerPath, runtimePath string) (Filesystem, int64, int64, error)
}
type OSProbe struct{}

func (OSProbe) Inspect(root, ledgerPath, runtimePath string) (Filesystem, int64, int64, error) {
	fs, e := filesystem(root)
	if e != nil {
		return Filesystem{}, 0, 0, e
	}
	ledgerBytes := int64(0)
	if st, e := os.Stat(ledgerPath); e == nil {
		ledgerBytes = st.Size()
	} else if !os.IsNotExist(e) {
		return Filesystem{}, 0, 0, e
	}
	runtimeBytes, e := directorySize(runtimePath)
	if e != nil {
		return Filesystem{}, 0, 0, e
	}
	return fs, ledgerBytes, runtimeBytes, nil
}
func directorySize(root string) (int64, error) {
	var total int64
	e := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			if os.IsNotExist(e) {
				return nil
			}
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("storage path contains symlink: %s", path)
		}
		if d.Type().IsRegular() {
			st, e := d.Info()
			if e != nil {
				return e
			}
			total += st.Size()
		}
		return nil
	})
	if os.IsNotExist(e) {
		return 0, nil
	}
	return total, e
}
func Validate(p Policy) error {
	if p.Version != Version {
		return fmt.Errorf("unsupported storage policy version %q", p.Version)
	}
	if p.MinimumFreeBytes < 0 || p.MaximumLedgerBytes < 0 || p.MaximumRuntimeBytes < 0 {
		return errors.New("byte thresholds must be non-negative")
	}
	if p.MinimumFreePercent < 0 || p.MinimumFreePercent > 100 {
		return errors.New("minimum_free_percent must be between 0 and 100")
	}
	if p.MinimumFreeBytes == 0 && p.MinimumFreePercent == 0 && p.MaximumLedgerBytes == 0 && p.MaximumRuntimeBytes == 0 {
		return errors.New("at least one storage threshold is required")
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
func Evaluate(root string, p Policy, probe Probe, now time.Time) (Status, error) {
	if e := Validate(p); e != nil {
		return Status{}, e
	}
	if probe == nil {
		probe = OSProbe{}
	}
	fs, ledgerBytes, runtimeBytes, e := probe.Inspect(root, filepath.Join(root, "ledger.db"), filepath.Join(root, "runtime"))
	if e != nil {
		return Status{}, e
	}
	s := Status{Version: Version, CheckedAt: now.UTC(), Healthy: true, Filesystem: fs, LedgerBytes: ledgerBytes, RuntimeBytes: runtimeBytes, Policy: p}
	if p.MinimumFreeBytes > 0 && fs.FreeBytes < p.MinimumFreeBytes {
		s.Healthy = false
		s.Findings = append(s.Findings, fmt.Sprintf("free bytes %d below minimum %d", fs.FreeBytes, p.MinimumFreeBytes))
	}
	if p.MinimumFreePercent > 0 && fs.FreePercent < p.MinimumFreePercent {
		s.Healthy = false
		s.Findings = append(s.Findings, fmt.Sprintf("free percent %.2f below minimum %.2f", fs.FreePercent, p.MinimumFreePercent))
	}
	if p.MaximumLedgerBytes > 0 && ledgerBytes > p.MaximumLedgerBytes {
		s.Healthy = false
		s.Findings = append(s.Findings, fmt.Sprintf("ledger bytes %d exceed maximum %d", ledgerBytes, p.MaximumLedgerBytes))
	}
	if p.MaximumRuntimeBytes > 0 && runtimeBytes > p.MaximumRuntimeBytes {
		s.Healthy = false
		s.Findings = append(s.Findings, fmt.Sprintf("runtime bytes %d exceed maximum %d", runtimeBytes, p.MaximumRuntimeBytes))
	}
	return s, nil
}

type Guard struct {
	Root     string
	Policy   Policy
	Probe    Probe
	CacheTTL time.Duration
	mu       sync.Mutex
	cached   Status
	cachedAt time.Time
}

func (g *Guard) Status(now time.Time) (Status, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ttl := g.CacheTTL
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	if !g.cachedAt.IsZero() && now.Sub(g.cachedAt) < ttl {
		return g.cached, nil
	}
	s, e := Evaluate(g.Root, g.Policy, g.Probe, now)
	if e == nil {
		g.cached = s
		g.cachedAt = now
	}
	return s, e
}
