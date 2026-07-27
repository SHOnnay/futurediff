package effectspec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// ConformanceStep is one observable assertion in the adapter lifecycle suite.
type ConformanceStep struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

// ConformanceReport records a deterministic lifecycle check for an adapter.
type ConformanceReport struct {
	EffectSpec string            `json:"effectspec"`
	Adapter    string            `json:"adapter"`
	Tool       string            `json:"tool"`
	Passed     bool              `json:"passed"`
	Steps      []ConformanceStep `json:"steps"`
	Generated  time.Time         `json:"generated_at"`
}

// ConformanceOptions supplies stable identities and input for the lifecycle suite.
type ConformanceOptions struct {
	Tool  string
	Input []byte
}

// RunConformance exercises the supported lifecycle in a conservative order.
// It never assumes an unsupported capability and does not blindly retry commit.
func RunConformance(ctx context.Context, adapter Adapter, opts ConformanceOptions) (ConformanceReport, error) {
	if adapter == nil {
		return ConformanceReport{}, errors.New("adapter is required")
	}
	if opts.Tool == "" {
		return ConformanceReport{}, errors.New("tool is required")
	}
	report := ConformanceReport{EffectSpec: Version, Tool: opts.Tool, Passed: true, Generated: time.Now().UTC()}
	add := func(name string, err error) {
		step := ConformanceStep{Name: name, Passed: err == nil}
		if err != nil {
			step.Message = err.Error()
			report.Passed = false
		}
		report.Steps = append(report.Steps, step)
	}

	descriptor, err := adapter.Describe(ctx, opts.Tool)
	add("describe", err)
	if err != nil {
		return report, nil
	}
	report.Adapter = descriptor.Adapter
	add("descriptor_validate", descriptor.Validate())
	if !report.Passed {
		return report, nil
	}

	effectCtx := Context{TransactionID: "tx_conformance", EffectID: "effect_conformance", IdempotencyKey: "conformance:commit", FencingToken: 1, ApprovalDigest: "sha256:conformance"}
	prepared, err := adapter.Prepare(ctx, effectCtx, opts.Input)
	add("prepare", err)
	if err != nil {
		return report, nil
	}
	if prepared.Handle == "" || prepared.InputDigest == "" {
		add("prepared_identity", errors.New("prepared effect must contain handle and input_digest"))
		return report, nil
	}
	add("prepared_identity", nil)

	if descriptor.Capabilities.Preview {
		preview, previewErr := adapter.Preview(ctx, effectCtx, prepared)
		if previewErr == nil && preview.Digest == "" {
			previewErr = errors.New("preview digest is required")
		}
		if previewErr == nil && preview.Fidelity == "" {
			previewErr = errors.New("preview fidelity is required")
		}
		add("preview", previewErr)
	}
	if descriptor.Capabilities.Verify {
		verification, verifyErr := adapter.Verify(ctx, effectCtx, prepared)
		if verifyErr == nil && verification.EvidenceDigest == "" {
			verifyErr = errors.New("verification evidence digest is required")
		}
		add("verify", verifyErr)
		if verifyErr != nil || !verification.Passed {
			return report, nil
		}
	}
	if descriptor.Capabilities.Status {
		status, statusErr := adapter.Status(ctx, effectCtx, prepared)
		if statusErr == nil && status.Status != StatusPrepared && status.Status != StatusUnknown {
			statusErr = fmt.Errorf("status before commit must be prepared or unknown, got %s", status.Status)
		}
		add("status_before_commit", statusErr)
	}

	receipt, err := adapter.Commit(ctx, effectCtx, prepared)
	if err == nil && receipt.RequestDigest == "" {
		err = errors.New("commit receipt request_digest is required")
	}
	if err == nil && receipt.CommittedAt.IsZero() {
		err = errors.New("commit receipt committed_at is required")
	}
	add("commit", err)
	if err != nil {
		return report, nil
	}

	if descriptor.Capabilities.Status {
		status, statusErr := adapter.Status(ctx, effectCtx, prepared)
		if statusErr == nil && status.Status != StatusCommitted {
			statusErr = fmt.Errorf("status after commit must be committed, got %s", status.Status)
		}
		if statusErr == nil && status.Receipt == nil {
			statusErr = errors.New("committed status must include a receipt")
		}
		add("status_after_commit", statusErr)
	}

	if descriptor.Capabilities.Compensate {
		compensation, compensateErr := adapter.Compensate(ctx, effectCtx, receipt)
		if compensateErr == nil && compensation.RequestDigest == "" {
			compensateErr = errors.New("compensation receipt request_digest is required")
		}
		add("compensate", compensateErr)
	}

	if descriptor.Capabilities.Abort {
		abortCtx := Context{TransactionID: "tx_conformance_abort", EffectID: "effect_conformance_abort", IdempotencyKey: "conformance:abort", FencingToken: 1}
		abortPrepared, prepareErr := adapter.Prepare(ctx, abortCtx, opts.Input)
		add("abort_prepare", prepareErr)
		if prepareErr == nil {
			abortErr := adapter.Abort(ctx, abortCtx, abortPrepared)
			add("abort", abortErr)
			if abortErr == nil && descriptor.Capabilities.Status {
				status, statusErr := adapter.Status(ctx, abortCtx, abortPrepared)
				if statusErr == nil && status.Status != StatusAborted {
					statusErr = fmt.Errorf("status after abort must be aborted, got %s", status.Status)
				}
				add("status_after_abort", statusErr)
			}
		}
	}
	return report, nil
}

// ValidateDescriptorJSON performs strict descriptor decoding and semantic validation.
func ValidateDescriptorJSON(data []byte) (Descriptor, error) {
	var descriptor Descriptor
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&descriptor); err != nil {
		return Descriptor{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Descriptor{}, errors.New("trailing descriptor data")
		}
		return Descriptor{}, err
	}
	return descriptor, descriptor.Validate()
}
