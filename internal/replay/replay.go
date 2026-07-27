package replay

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

type EffectProjection struct {
	EffectID string             `json:"effect_id"`
	Replayed domain.EffectState `json:"replayed"`
	Stored   domain.EffectState `json:"stored"`
	Match    bool               `json:"match"`
}
type Report struct {
	TransactionID          string                  `json:"transaction_id"`
	ReplayedStatus         domain.TransactionState `json:"replayed_status"`
	StoredStatus           domain.TransactionState `json:"stored_status"`
	StatusMatch            bool                    `json:"status_match"`
	ReplayedApprovalDigest string                  `json:"replayed_approval_digest,omitempty"`
	StoredApprovalDigest   string                  `json:"stored_approval_digest,omitempty"`
	ApprovalMatch          bool                    `json:"approval_match"`
	Effects                []EffectProjection      `json:"effects,omitempty"`
	EventCount             int                     `json:"event_count"`
	Valid                  bool                    `json:"valid"`
	Findings               []string                `json:"findings,omitempty"`
}

func Transaction(repo *ledger.Repository, transactionID string) (Report, error) {
	if _, err := repo.VerifyEventChains(); err != nil {
		return Report{}, err
	}
	stored, err := repo.Get(transactionID)
	if err != nil {
		return Report{}, err
	}
	events, err := repo.Events(transactionID)
	if err != nil {
		return Report{}, err
	}
	effects, err := repo.ExternalEffects(transactionID)
	if err != nil {
		return Report{}, err
	}
	report := Report{TransactionID: transactionID, StoredStatus: stored.Status, StoredApprovalDigest: stored.ApprovalDigest, EventCount: len(events), Valid: true}
	replayed := domain.TransactionState("")
	approval := ""
	effectStates := map[string]domain.EffectState{}
	for _, event := range events {
		kind := ledger.String(event, "event_type")
		effectID := ledger.String(event, "effect_id")
		payload := map[string]any{}
		_ = json.Unmarshal([]byte(ledger.String(event, "payload_json")), &payload)
		switch kind {
		case "transaction.created":
			replayed = domain.StateCreated
		case "transaction.activated":
			replayed = domain.StateActive
		case "repository.patch_sealed":
			replayed = domain.StateSealed
			approval = ""
		case "verification.completed":
			approval = ""
			if fmt.Sprint(payload["outcome"]) == "pass" {
				replayed = domain.StateReady
			} else {
				replayed = domain.StateFailedVerification
			}
		case "transaction.approved":
			approval = fmt.Sprint(payload["digest"])
		case "transaction.committing":
			replayed = domain.StateCommitting
		case "transaction.committed":
			replayed = domain.StateCommitted
		default:
			if strings.HasPrefix(kind, "transaction.") {
				suffix := domain.TransactionState(strings.TrimPrefix(kind, "transaction."))
				if isTransactionState(suffix) {
					replayed = suffix
				}
			}
		}
		if kind == "effect.prepared" || kind == "effect.refreshed" {
			approval = ""
		}
		if strings.HasPrefix(kind, "transaction.") && (replayed == domain.StateStale || replayed == domain.StateAborting || replayed == domain.StateAborted) {
			approval = ""
		}
		if effectID != "" {
			switch kind {
			case "effect.prepared", "effect.refreshed", "effect.rearmed", "effect.commit.rejected":
				effectStates[effectID] = domain.EffectVerified
			case "effect.commit.intent":
				effectStates[effectID] = domain.EffectCommitting
			case "effect.unknown":
				effectStates[effectID] = domain.EffectUnknown
			case "effect.committed":
				effectStates[effectID] = domain.EffectCommitted
			case "effect.aborted":
				effectStates[effectID] = domain.EffectAborted
			}
		}
	}
	report.ReplayedStatus = replayed
	report.StatusMatch = replayed == stored.Status
	report.ReplayedApprovalDigest = approval
	report.ApprovalMatch = approval == stored.ApprovalDigest
	if !report.StatusMatch {
		report.Valid = false
		report.Findings = append(report.Findings, fmt.Sprintf("transaction status replayed=%s stored=%s", replayed, stored.Status))
	}
	if !report.ApprovalMatch {
		report.Valid = false
		report.Findings = append(report.Findings, "approval digest projection differs")
	}
	for _, effect := range effects {
		projected := effectStates[effect.EffectID]
		item := EffectProjection{EffectID: effect.EffectID, Replayed: projected, Stored: effect.Status, Match: projected == effect.Status}
		if !item.Match {
			report.Valid = false
			report.Findings = append(report.Findings, fmt.Sprintf("effect %s replayed=%s stored=%s", effect.EffectID, projected, effect.Status))
		}
		report.Effects = append(report.Effects, item)
	}
	return report, nil
}
func isTransactionState(s domain.TransactionState) bool {
	switch s {
	case domain.StateCreated, domain.StateActive, domain.StateSealed, domain.StateVerifying, domain.StateFailedVerification, domain.StateReady, domain.StateStale, domain.StateCommitting, domain.StateAborting, domain.StateAborted, domain.StateCompensating, domain.StateCompensated, domain.StateNeedsReconciliation, domain.StateCommitted, domain.StateManualIntervention:
		return true
	}
	return false
}
