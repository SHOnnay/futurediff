package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

type Mode string

const (
	Direct         Mode = "direct"
	PermissionOnly Mode = "permission_only"
	SandboxOnly    Mode = "sandbox_only"
	FutureDiff     Mode = "futurediff"
)

type Step struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	EffectKey      string `json:"effect_key,omitempty"`
	Fails          bool   `json:"fails,omitempty"`
	RetryAfterLoss bool   `json:"retry_after_loss,omitempty"`
}

type Scenario struct {
	FormatVersion string `json:"format_version"`
	ID            string `json:"id"`
	Description   string `json:"description"`
	Steps         []Step `json:"steps"`
}

type Metrics struct {
	Mode                    Mode `json:"mode"`
	TaskCompleted           bool `json:"task_completed"`
	VerificationPassed      bool `json:"verification_passed"`
	ReleasedEffects         int  `json:"released_effects"`
	UnsafeReleasedEffects   int  `json:"unsafe_released_effects"`
	DuplicateEffects        int  `json:"duplicate_effects"`
	HumanApprovals          int  `json:"human_approvals"`
	CompensationsRequired   int  `json:"compensations_required"`
	RecoverySucceeded       bool `json:"recovery_succeeded"`
	RepositoryChangedOnFail bool `json:"repository_changed_on_failure"`
}

type Result struct {
	ScenarioID string    `json:"scenario_id"`
	Metrics    []Metrics `json:"metrics"`
}

type Report struct {
	FormatVersion string    `json:"format_version"`
	Kind          string    `json:"kind"`
	GeneratedAt   time.Time `json:"generated_at"`
	Results       []Result  `json:"results"`
	ReportDigest  string    `json:"report_digest"`
	Disclaimer    string    `json:"disclaimer"`
}

func ValidateScenario(s Scenario) error {
	if s.FormatVersion != "0.1" || s.ID == "" || len(s.Steps) == 0 {
		return errors.New("scenario requires format_version=0.1, id, and steps")
	}
	ids := map[string]bool{}
	for _, step := range s.Steps {
		if step.ID == "" || ids[step.ID] {
			return fmt.Errorf("invalid or duplicate step id %q", step.ID)
		}
		ids[step.ID] = true
		switch step.Kind {
		case "repository_mutation", "external_effect", "verification":
		default:
			return fmt.Errorf("unsupported step kind %q", step.Kind)
		}
		if step.Kind == "external_effect" && step.EffectKey == "" {
			return fmt.Errorf("external effect %s requires effect_key", step.ID)
		}
	}
	return nil
}

func RunScenario(s Scenario) (Result, error) {
	if err := ValidateScenario(s); err != nil {
		return Result{}, err
	}
	modes := []Mode{Direct, PermissionOnly, SandboxOnly, FutureDiff}
	result := Result{ScenarioID: s.ID}
	for _, mode := range modes {
		result.Metrics = append(result.Metrics, simulate(s, mode))
	}
	return result, nil
}

func simulate(s Scenario, mode Mode) Metrics {
	m := Metrics{Mode: mode, RecoverySucceeded: true}
	stagedRepo := false
	released := map[string]int{}
	verificationSeen := false
	failureSeen := false

	for _, step := range s.Steps {
		switch step.Kind {
		case "repository_mutation":
			if mode == Direct || mode == PermissionOnly {
				m.RepositoryChangedOnFail = true
			} else {
				stagedRepo = true
			}
			if mode == PermissionOnly {
				m.HumanApprovals++
			}
		case "external_effect":
			if mode == PermissionOnly {
				m.HumanApprovals++
			}
			if mode != FutureDiff {
				released[step.EffectKey]++
				m.ReleasedEffects++
				if step.RetryAfterLoss {
					released[step.EffectKey]++
					m.ReleasedEffects++
					m.DuplicateEffects++
				}
			}
		case "verification":
			verificationSeen = true
			if step.Fails {
				failureSeen = true
				m.VerificationPassed = false
			} else {
				m.VerificationPassed = true
			}
		}
	}

	if mode == FutureDiff {
		m.HumanApprovals = 1
		if verificationSeen && !failureSeen {
			m.VerificationPassed = true
			for _, step := range s.Steps {
				if step.Kind == "external_effect" {
					if released[step.EffectKey] == 0 {
						released[step.EffectKey] = 1
						m.ReleasedEffects++
					}
				}
			}
			m.TaskCompleted = true
		} else {
			m.ReleasedEffects = 0
			m.UnsafeReleasedEffects = 0
			m.RepositoryChangedOnFail = false
			m.TaskCompleted = false
		}
	} else if failureSeen {
		m.UnsafeReleasedEffects = m.ReleasedEffects
		if mode == SandboxOnly {
			m.RepositoryChangedOnFail = false
		}
		if m.ReleasedEffects > 0 {
			m.CompensationsRequired = m.ReleasedEffects - m.DuplicateEffects
		}
		m.TaskCompleted = false
	} else {
		m.TaskCompleted = true
	}
	if stagedRepo && failureSeen {
		m.RepositoryChangedOnFail = false
	}
	return m
}

func Run(scenarios []Scenario) (Report, error) {
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].ID < scenarios[j].ID })
	report := Report{FormatVersion: "0.1", Kind: "deterministic_effect_safety_model", GeneratedAt: time.Now().UTC(), Disclaimer: "Synthetic benchmark: this models effect-release semantics. It does not measure model quality, real provider latency, or token use."}
	for _, scenario := range scenarios {
		result, err := RunScenario(scenario)
		if err != nil {
			return Report{}, err
		}
		report.Results = append(report.Results, result)
	}
	clone := report
	clone.ReportDigest = ""
	digest, _ := domain.Digest(clone)
	report.ReportDigest = digest
	return report, nil
}

func LoadDir(dir string) ([]Scenario, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var scenarios []Scenario
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var scenario Scenario
		if err := json.Unmarshal(data, &scenario); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		scenarios = append(scenarios, scenario)
	}
	if len(scenarios) == 0 {
		return nil, errors.New("no benchmark scenarios found")
	}
	return scenarios, nil
}

func WriteJSON(path string, report Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func Markdown(report Report) string {
	out := "# FutureDiff deterministic safety benchmark\n\n"
	out += "> " + report.Disclaimer + "\n\n"
	out += "| Scenario | Mode | Completed | Released | Unsafe | Duplicates | Approvals | Repo changed on failure |\n|---|---|---:|---:|---:|---:|---:|---:|\n"
	for _, result := range report.Results {
		for _, m := range result.Metrics {
			out += fmt.Sprintf("| %s | %s | %t | %d | %d | %d | %d | %t |\n", result.ScenarioID, m.Mode, m.TaskCompleted, m.ReleasedEffects, m.UnsafeReleasedEffects, m.DuplicateEffects, m.HumanApprovals, m.RepositoryChangedOnFail)
		}
	}
	out += "\nReport digest: `" + report.ReportDigest + "`\n"
	return out
}
