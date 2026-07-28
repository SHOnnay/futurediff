package apicontract

import (
	"sort"

	"github.com/SHOnnay/futurediff/internal/domain"
)

const Version = "1.1"

type Endpoint struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	OperationID string `json:"operation_id"`
	AgentSafe   bool   `json:"agent_safe"`
}

type Contract struct {
	Version   string     `json:"version"`
	Transport string     `json:"transport"`
	Endpoints []Endpoint `json:"endpoints"`
	Digest    string     `json:"digest"`
}

func Current() Contract {
	endpoints := []Endpoint{
		{"GET", "/v1/health", "health", true},
		{"GET", "/v1/contract", "contract", true},
		{"GET", "/v1/openapi", "openapi", true},
		{"POST", "/v1/transactions", "transaction_create", true},
		{"GET", "/v1/transactions", "transaction_list", true},
		{"GET", "/v1/transactions/{id}", "transaction_get", true},
		{"POST", "/v1/transactions/{id}/execute", "transaction_execute", true},
		{"POST", "/v1/transactions/{id}/effects/github/branch", "github_branch_prepare", true},
		{"POST", "/v1/transactions/{id}/effects/github/draft-pull-request", "github_pr_prepare", true},
		{"POST", "/v1/transactions/{id}/effects/slack/message", "slack_message_prepare", true},
		{"GET", "/v1/transactions/{id}/effects", "effects_list", true},
		{"POST", "/v1/transactions/{id}/effects/{effectID}/refresh", "effect_refresh", true},
		{"POST", "/v1/transactions/{id}/seal", "transaction_seal", true},
		{"POST", "/v1/transactions/{id}/verify", "transaction_verify", true},
		{"GET", "/v1/transactions/{id}/approval-material", "approval_material", false},
		{"POST", "/v1/transactions/{id}/approve", "transaction_approve", false},
		{"POST", "/v1/transactions/{id}/commit", "transaction_commit", false},
		{"POST", "/v1/transactions/{id}/recover", "transaction_recover", false},
		{"POST", "/v1/transactions/{id}/abort", "transaction_abort", false},
		{"GET", "/v1/transactions/{id}/events", "events_list", true},
		{"GET", "/v1/transactions/{id}/access", "transaction_access_list", false},
		{"PUT", "/v1/transactions/{id}/access/{principalID}", "transaction_access_grant", false},
		{"DELETE", "/v1/transactions/{id}/access/{principalID}", "transaction_access_revoke", false},
	}
	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].Path != endpoints[j].Path {
			return endpoints[i].Path < endpoints[j].Path
		}
		return endpoints[i].Method < endpoints[j].Method
	})
	digest, _ := domain.Digest(map[string]any{"version": Version, "transport": "unix-http", "endpoints": endpoints})
	return Contract{Version: Version, Transport: "unix-http", Endpoints: endpoints, Digest: digest}
}
