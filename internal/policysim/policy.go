package policysim

import (
	"fmt"
	"sort"

	"github.com/SHOnnay/futurediff/internal/verification"
)

type CheckExplanation struct {
	CheckID     string   `json:"check_id"`
	Required    bool     `json:"required"`
	Executor    string   `json:"executor"`
	Type        string   `json:"type"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Simulated   string   `json:"simulated_status,omitempty"`
	Explanation string   `json:"explanation"`
}

type Report struct {
	ContractID    string             `json:"contract_id"`
	PolicyVersion string             `json:"policy_version"`
	Order         []string           `json:"order"`
	Checks        []CheckExplanation `json:"checks"`
	Warnings      []string           `json:"warnings,omitempty"`
	Simulated     bool               `json:"simulated"`
	Outcome       string             `json:"outcome,omitempty"`
}

func Explain(contract verification.Contract, statuses map[string]string, assumePass bool) (Report, error) {
	if err := verification.Validate(contract); err != nil {
		return Report{}, err
	}
	order, err := verification.Topological(contract.Checks)
	if err != nil {
		return Report{}, err
	}
	allowed := map[string]bool{"pass": true, "fail": true, "error": true, "timeout": true, "blocked": true, "cancelled": true}
	for id, status := range statuses {
		if !allowed[status] {
			return Report{}, fmt.Errorf("unsupported simulated status %s for %s", status, id)
		}
	}
	report := Report{ContractID: contract.ContractID, PolicyVersion: contract.PolicyVersion, Simulated: statuses != nil || assumePass}
	result := map[string]string{}
	for _, ch := range order {
		report.Order = append(report.Order, ch.CheckID)
		blocked := false
		for _, dep := range ch.DependsOn {
			if result[dep] != "pass" {
				blocked = true
			}
		}
		status := ""
		if report.Simulated {
			if blocked {
				status = "blocked"
			} else if v, ok := statuses[ch.CheckID]; ok {
				status = v
			} else if assumePass {
				status = "pass"
			} else {
				status = "error"
			}
			result[ch.CheckID] = status
		}
		explanation := fmt.Sprintf("%s check using %s", ch.Type, ch.Executor)
		if ch.Required {
			explanation += "; required for transaction readiness"
		} else {
			explanation += "; advisory only"
		}
		report.Checks = append(report.Checks, CheckExplanation{CheckID: ch.CheckID, Required: ch.Required, Executor: ch.Executor, Type: ch.Type, DependsOn: append([]string(nil), ch.DependsOn...), Simulated: status, Explanation: explanation})
		if ch.Executor == "local_command" {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s uses local_command and must remain disabled in enforced mode", ch.CheckID))
		}
		if (ch.Executor == "local_command" || ch.Executor == "oci_command") && ch.TimeoutSeconds <= 0 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s has no explicit timeout", ch.CheckID))
		}
		if !ch.Required {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s cannot block readiness because it is optional", ch.CheckID))
		}
	}
	sort.Strings(report.Warnings)
	if report.Simulated {
		required := 0
		outcome := "pass"
		for _, ch := range order {
			if !ch.Required {
				continue
			}
			required++
			s := result[ch.CheckID]
			if s == "error" || s == "cancelled" {
				outcome = "error"
				break
			}
			if s != "pass" && outcome == "pass" {
				outcome = "fail"
			}
		}
		if required == 0 {
			outcome = "error"
		}
		report.Outcome = outcome
	}
	return report, nil
}
