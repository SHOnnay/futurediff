package domain

import "time"

// ExternalEffect is the durable, non-secret representation of a provider
// mutation prepared by a trusted EffectSpec adapter.
type ExternalEffect struct {
	EffectID            string            `json:"effect_id"`
	TransactionID       string            `json:"transaction_id"`
	ToolIdentity        string            `json:"tool_identity"`
	AdapterIdentity     string            `json:"adapter_identity"`
	EffectClass         string            `json:"effect_class"`
	RiskLevel           string            `json:"risk_level,omitempty"`
	CredentialID        string            `json:"credential_id"`
	Operation           string            `json:"operation"`
	Destination         string            `json:"destination"`
	InputJSON           string            `json:"input_json"`
	InputDigest         string            `json:"input_digest"`
	PreparedJSON        string            `json:"prepared_json"`
	PreparedDigest      string            `json:"prepared_digest"`
	PreviewJSON         string            `json:"preview_json"`
	PreviewDigest       string            `json:"preview_digest"`
	ResourceVersions    map[string]string `json:"resource_versions,omitempty"`
	IdempotencyKey      string            `json:"idempotency_key"`
	CommitRequestDigest string            `json:"commit_request_digest,omitempty"`
	CommitFencingToken  int64             `json:"commit_fencing_token,omitempty"`
	Status              EffectState       `json:"status"`
	Reversibility       string            `json:"reversibility"`
	CommitRank          int               `json:"commit_rank"`
	SupportLevel        string            `json:"support_level"`
	DependsOn           []string          `json:"depends_on,omitempty"`
	Revision            int64             `json:"revision"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

// EffectReceipt is provider evidence that an external mutation was observed as
// committed. The receipt never contains credential material.
type EffectReceipt struct {
	ReceiptID           string    `json:"receipt_id"`
	EffectID            string    `json:"effect_id"`
	ProviderOperationID string    `json:"provider_operation_id,omitempty"`
	ProviderResourceID  string    `json:"provider_resource_id,omitempty"`
	RequestDigest       string    `json:"request_digest"`
	ResponseDigest      string    `json:"response_digest,omitempty"`
	StatusQueryRef      string    `json:"status_query_ref,omitempty"`
	FencingToken        int64     `json:"fencing_token"`
	CommittedAt         time.Time `json:"committed_at"`
	CreatedAt           time.Time `json:"created_at"`
}

// EffectAttempt records write-ahead intent and the outcome classification for
// commit/status operations. A transport error after dispatch is ambiguous and
// must be represented as unknown rather than failed.
type EffectAttempt struct {
	AttemptID      string    `json:"attempt_id"`
	EffectID       string    `json:"effect_id"`
	TransactionID  string    `json:"transaction_id"`
	Phase          string    `json:"phase"`
	RequestDigest  string    `json:"request_digest"`
	FencingToken   int64     `json:"fencing_token"`
	Outcome        string    `json:"outcome"`
	HTTPStatus     int       `json:"http_status,omitempty"`
	ResponseDigest string    `json:"response_digest,omitempty"`
	ErrorClass     string    `json:"error_class,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
}
