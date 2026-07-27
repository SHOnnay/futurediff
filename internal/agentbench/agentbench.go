package agentbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Run struct {
	FormatVersion     string    `json:"format_version"`
	RunID             string    `json:"run_id"`
	Agent             string    `json:"agent"`
	AgentVersion      string    `json:"agent_version,omitempty"`
	TaskID            string    `json:"task_id"`
	Mode              string    `json:"mode"`
	StartedAt         time.Time `json:"started_at"`
	FinishedAt        time.Time `json:"finished_at"`
	InputTokens       int64     `json:"input_tokens"`
	OutputTokens      int64     `json:"output_tokens"`
	CachedInputTokens int64     `json:"cached_input_tokens,omitempty"`
	ModelCalls        int       `json:"model_calls"`
	ToolCalls         int       `json:"tool_calls"`
	RepairTurns       int       `json:"repair_turns"`
	VerificationMS    int64     `json:"verification_ms"`
	ComputeMS         int64     `json:"compute_ms"`
	ReleasedEffects   int       `json:"released_effects"`
	UnsafeEffects     int       `json:"unsafe_effects"`
	DuplicateEffects  int       `json:"duplicate_effects"`
	Success           bool      `json:"success"`
	Source            string    `json:"source,omitempty"`
}

func (r Run) Validate() error {
	if r.FormatVersion != "0.1" {
		return fmt.Errorf("unsupported format_version %q", r.FormatVersion)
	}
	if strings.TrimSpace(r.RunID) == "" || strings.TrimSpace(r.Agent) == "" || strings.TrimSpace(r.TaskID) == "" || strings.TrimSpace(r.Mode) == "" {
		return errors.New("run_id, agent, task_id, and mode are required")
	}
	if r.FinishedAt.Before(r.StartedAt) {
		return errors.New("finished_at precedes started_at")
	}
	values := []int64{r.InputTokens, r.OutputTokens, r.CachedInputTokens, int64(r.ModelCalls), int64(r.ToolCalls), int64(r.RepairTurns), r.VerificationMS, r.ComputeMS, int64(r.ReleasedEffects), int64(r.UnsafeEffects), int64(r.DuplicateEffects)}
	for _, v := range values {
		if v < 0 {
			return errors.New("metrics cannot be negative")
		}
	}
	return nil
}
func (r Run) WallMS() int64      { return r.FinishedAt.Sub(r.StartedAt).Milliseconds() }
func (r Run) TotalTokens() int64 { return r.InputTokens + r.OutputTokens }

type Aggregate struct {
	Mode                       string   `json:"mode"`
	Runs                       int      `json:"runs"`
	SuccessRate                float64  `json:"success_rate"`
	MeanTotalTokens            float64  `json:"mean_total_tokens"`
	MeanWallMS                 float64  `json:"mean_wall_ms"`
	MeanRepairTurns            float64  `json:"mean_repair_turns"`
	UnsafeEffects              int      `json:"unsafe_effects"`
	DuplicateEffects           int      `json:"duplicate_effects"`
	ReleasedEffects            int      `json:"released_effects"`
	TokenOverheadVsBaselinePct *float64 `json:"token_overhead_vs_baseline_pct,omitempty"`
	WallOverheadVsBaselinePct  *float64 `json:"wall_overhead_vs_baseline_pct,omitempty"`
}
type Report struct {
	FormatVersion string      `json:"format_version"`
	GeneratedAt   time.Time   `json:"generated_at"`
	BaselineMode  string      `json:"baseline_mode"`
	Runs          []Run       `json:"runs"`
	Aggregates    []Aggregate `json:"aggregates"`
}

func Load(paths []string) ([]Run, error) {
	var runs []Run
	seen := map[string]bool{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var r Run
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		r.Source = filepath.Base(p)
		if err := r.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if seen[r.RunID] {
			return nil, fmt.Errorf("duplicate run_id %s", r.RunID)
		}
		seen[r.RunID] = true
		runs = append(runs, r)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].RunID < runs[j].RunID })
	return runs, nil
}
func Build(runs []Run, baseline string) Report {
	groups := map[string][]Run{}
	for _, r := range runs {
		groups[r.Mode] = append(groups[r.Mode], r)
	}
	modes := make([]string, 0, len(groups))
	for m := range groups {
		modes = append(modes, m)
	}
	sort.Strings(modes)
	aggs := make([]Aggregate, 0, len(modes))
	for _, m := range modes {
		rs := groups[m]
		a := Aggregate{Mode: m, Runs: len(rs)}
		var successes int
		var tok, wall, rep int64
		for _, r := range rs {
			if r.Success {
				successes++
			}
			tok += r.TotalTokens()
			wall += r.WallMS()
			rep += int64(r.RepairTurns)
			a.UnsafeEffects += r.UnsafeEffects
			a.DuplicateEffects += r.DuplicateEffects
			a.ReleasedEffects += r.ReleasedEffects
		}
		n := float64(len(rs))
		a.SuccessRate = float64(successes) / n
		a.MeanTotalTokens = float64(tok) / n
		a.MeanWallMS = float64(wall) / n
		a.MeanRepairTurns = float64(rep) / n
		aggs = append(aggs, a)
	}
	var base *Aggregate
	for i := range aggs {
		if aggs[i].Mode == baseline {
			base = &aggs[i]
			break
		}
	}
	if base != nil {
		for i := range aggs {
			if aggs[i].Mode == baseline {
				continue
			}
			if base.MeanTotalTokens > 0 {
				v := (aggs[i].MeanTotalTokens - base.MeanTotalTokens) / base.MeanTotalTokens * 100
				aggs[i].TokenOverheadVsBaselinePct = &v
			}
			if base.MeanWallMS > 0 {
				v := (aggs[i].MeanWallMS - base.MeanWallMS) / base.MeanWallMS * 100
				aggs[i].WallOverheadVsBaselinePct = &v
			}
		}
	}
	return Report{FormatVersion: "0.1", GeneratedAt: time.Now().UTC(), BaselineMode: baseline, Runs: runs, Aggregates: aggs}
}
func WriteJSON(path string, report Report) error {
	b, err := json.MarshalIndent(report, "", "  ")
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
func WriteMarkdown(path string, report Report) error {
	var b strings.Builder
	b.WriteString("# FutureDiff Agent Benchmark\n\n")
	b.WriteString("Baseline mode: `" + report.BaselineMode + "`\n\n")
	b.WriteString("| Mode | Runs | Success | Mean tokens | Mean wall ms | Repairs | Unsafe effects | Duplicate effects | Token overhead | Wall overhead |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, a := range report.Aggregates {
		tok := "—"
		wall := "—"
		if a.TokenOverheadVsBaselinePct != nil {
			tok = fmt.Sprintf("%.1f%%", *a.TokenOverheadVsBaselinePct)
		}
		if a.WallOverheadVsBaselinePct != nil {
			wall = fmt.Sprintf("%.1f%%", *a.WallOverheadVsBaselinePct)
		}
		fmt.Fprintf(&b, "| %s | %d | %.1f%% | %.0f | %.0f | %.2f | %d | %d | %s | %s |\n", a.Mode, a.Runs, a.SuccessRate*100, a.MeanTotalTokens, a.MeanWallMS, a.MeanRepairTurns, a.UnsafeEffects, a.DuplicateEffects, tok, wall)
	}
	b.WriteString("\nThis report is based only on supplied measured run records. It does not infer missing token or latency values.\n")
	if path == "" || path == "-" {
		_, err := fmt.Print(b.String())
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
