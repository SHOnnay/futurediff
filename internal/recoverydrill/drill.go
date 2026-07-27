package recoverydrill

import (
	"fmt"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

type Input struct {
	Name                string                  `json:"name"`
	TransactionStatus   domain.TransactionState `json:"transaction_status"`
	EffectStatus        domain.EffectState      `json:"effect_status,omitempty"`
	ProviderStatus      string                  `json:"provider_status,omitempty"`
	HasReceipt          bool                    `json:"has_receipt"`
	RepositoryPublished bool                    `json:"repository_published"`
	SourceMoved         bool                    `json:"source_moved"`
}
type Plan struct {
	Action            string `json:"action"`
	BlindRetryAllowed bool   `json:"blind_retry_allowed"`
	RequiresHuman     bool   `json:"requires_human"`
	Reason            string `json:"reason"`
}
type Scenario struct {
	Input          Input  `json:"input"`
	ExpectedAction string `json:"expected_action"`
	Actual         Plan   `json:"actual"`
	Passed         bool   `json:"passed"`
}
type Report struct {
	GeneratedAt time.Time  `json:"generated_at"`
	Passed      bool       `json:"passed"`
	Scenarios   []Scenario `json:"scenarios"`
}

func Decide(in Input) (Plan, error) {
	if in.TransactionStatus != domain.StateCommitting && in.TransactionStatus != domain.StateNeedsReconciliation && in.TransactionStatus != domain.StateCompensating {
		return Plan{}, fmt.Errorf("recovery planner requires committing, compensating, or needs_reconciliation state")
	}
	if in.SourceMoved && !in.RepositoryPublished {
		return Plan{Action: "mark_stale", Reason: "source moved before publication"}, nil
	}
	if in.EffectStatus == domain.EffectUnknown || in.TransactionStatus == domain.StateNeedsReconciliation {
		switch in.ProviderStatus {
		case "committed":
			if in.HasReceipt {
				return Plan{Action: "finalize_effect", Reason: "provider status and receipt prove commitment"}, nil
			}
			return Plan{Action: "query_status", Reason: "provider reports commitment but durable receipt is missing"}, nil
		case "not_committed":
			return Plan{Action: "rearm_effect", Reason: "provider proves no mutation occurred"}, nil
		case "unknown", "":
			return Plan{Action: "query_status", BlindRetryAllowed: false, Reason: "ambiguous provider result must be reconciled before retry"}, nil
		default:
			return Plan{}, fmt.Errorf("unsupported provider status %s", in.ProviderStatus)
		}
	}
	if in.RepositoryPublished && (in.EffectStatus == domain.EffectCommitted || in.EffectStatus == "") {
		return Plan{Action: "finalize_transaction", Reason: "all required durable effects are present"}, nil
	}
	if in.TransactionStatus == domain.StateCompensating {
		return Plan{Action: "continue_compensation", Reason: "compensation is incomplete"}, nil
	}
	return Plan{Action: "manual_intervention", RequiresHuman: true, Reason: "evidence is insufficient for an automated recovery decision"}, nil
}

func SelfTest() Report {
	cases := []struct {
		in   Input
		want string
	}{
		{Input{Name: "ambiguous", TransactionStatus: domain.StateNeedsReconciliation, EffectStatus: domain.EffectUnknown, ProviderStatus: "unknown"}, "query_status"},
		{Input{Name: "not_committed", TransactionStatus: domain.StateNeedsReconciliation, EffectStatus: domain.EffectUnknown, ProviderStatus: "not_committed"}, "rearm_effect"},
		{Input{Name: "committed", TransactionStatus: domain.StateNeedsReconciliation, EffectStatus: domain.EffectUnknown, ProviderStatus: "committed", HasReceipt: true}, "finalize_effect"},
		{Input{Name: "stale_source", TransactionStatus: domain.StateCommitting, SourceMoved: true}, "mark_stale"},
		{Input{Name: "finalize", TransactionStatus: domain.StateCommitting, RepositoryPublished: true, EffectStatus: domain.EffectCommitted}, "finalize_transaction"},
	}
	report := Report{GeneratedAt: time.Now().UTC(), Passed: true}
	for _, c := range cases {
		p, e := Decide(c.in)
		s := Scenario{Input: c.in, ExpectedAction: c.want, Actual: p, Passed: e == nil && p.Action == c.want && !p.BlindRetryAllowed}
		if !s.Passed {
			report.Passed = false
		}
		report.Scenarios = append(report.Scenarios, s)
	}
	return report
}
