// Package effectspec defines the framework-neutral lifecycle contract used by
// FutureDiff effect adapters. The coordinator owns ordering and durability;
// adapters own provider-specific preparation, commitment, status, and compensation.
package effectspec

import (
	"context"
	"errors"
	"time"
)

const Version = "0.1"

type Reversibility string

const (
	Reversible           Reversibility = "reversible"
	Compensatable        Reversibility = "compensatable"
	Irreversible         Reversibility = "irreversible"
	UnknownReversibility Reversibility = "unknown"
)

type PreviewFidelity string

const (
	Exact           PreviewFidelity = "exact"
	ExactPayload    PreviewFidelity = "exact_payload"
	BoundedEstimate PreviewFidelity = "bounded_estimate"
	Unavailable     PreviewFidelity = "unavailable"
)

type Capabilities struct {
	Prepare    bool `json:"prepare"`
	Preview    bool `json:"preview"`
	Verify     bool `json:"verify"`
	Commit     bool `json:"commit"`
	Abort      bool `json:"abort"`
	Compensate bool `json:"compensate"`
	Status     bool `json:"status"`
}

type Descriptor struct {
	EffectSpec      string          `json:"effectspec"`
	Adapter         string          `json:"adapter"`
	Tool            string          `json:"tool"`
	Capabilities    Capabilities    `json:"capabilities"`
	MutatesState    bool            `json:"mutates_state"`
	OpenWorld       bool            `json:"open_world"`
	Reversibility   Reversibility   `json:"reversibility"`
	PreviewFidelity PreviewFidelity `json:"preview_fidelity"`
}

func (d Descriptor) Validate() error {
	if d.EffectSpec != Version {
		return errors.New("unsupported effectspec version")
	}
	if d.Adapter == "" || d.Tool == "" {
		return errors.New("adapter and tool are required")
	}
	if d.MutatesState && !d.Capabilities.Commit {
		return errors.New("mutating adapters must implement commit")
	}
	if d.Capabilities.Compensate && d.Reversibility != Compensatable && d.Reversibility != Reversible {
		return errors.New("compensation capability conflicts with reversibility")
	}
	return nil
}

type Context struct {
	TransactionID  string `json:"transaction_id"`
	EffectID       string `json:"effect_id"`
	IdempotencyKey string `json:"idempotency_key"`
	FencingToken   uint64 `json:"fencing_token"`
	ApprovalDigest string `json:"approval_digest,omitempty"`
}

type PreparedEffect struct {
	Handle           string            `json:"handle"`
	InputDigest      string            `json:"input_digest"`
	ResourceVersions map[string]string `json:"resource_versions,omitempty"`
	ExpiresAt        *time.Time        `json:"expires_at,omitempty"`
}

type Preview struct {
	Digest   string          `json:"digest"`
	Summary  map[string]any  `json:"summary"`
	Fidelity PreviewFidelity `json:"fidelity"`
}

type Verification struct {
	Passed         bool   `json:"passed"`
	EvidenceDigest string `json:"evidence_digest"`
	Message        string `json:"message,omitempty"`
}

type Receipt struct {
	ProviderOperationID string    `json:"provider_operation_id,omitempty"`
	ProviderResourceID  string    `json:"provider_resource_id,omitempty"`
	RequestDigest       string    `json:"request_digest"`
	ResponseDigest      string    `json:"response_digest,omitempty"`
	CommittedAt         time.Time `json:"committed_at"`
}

type EffectStatus string

const (
	StatusPrepared    EffectStatus = "prepared"
	StatusCommitted   EffectStatus = "committed"
	StatusAborted     EffectStatus = "aborted"
	StatusCompensated EffectStatus = "compensated"
	StatusUnknown     EffectStatus = "unknown"
)

type StatusResult struct {
	Status  EffectStatus `json:"status"`
	Receipt *Receipt     `json:"receipt,omitempty"`
	Message string       `json:"message,omitempty"`
}

type Adapter interface {
	Describe(ctx context.Context, tool string) (Descriptor, error)
	Prepare(ctx context.Context, effectContext Context, input []byte) (PreparedEffect, error)
	Preview(ctx context.Context, effectContext Context, prepared PreparedEffect) (Preview, error)
	Verify(ctx context.Context, effectContext Context, prepared PreparedEffect) (Verification, error)
	Commit(ctx context.Context, effectContext Context, prepared PreparedEffect) (Receipt, error)
	Abort(ctx context.Context, effectContext Context, prepared PreparedEffect) error
	Compensate(ctx context.Context, effectContext Context, receipt Receipt) (Receipt, error)
	Status(ctx context.Context, effectContext Context, prepared PreparedEffect) (StatusResult, error)
}
