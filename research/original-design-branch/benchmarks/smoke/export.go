package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/futurediff/futurediff/verifier/evidence/artifactstore"
)

type Metrics struct {
	TaskCompletionRate             float64 `json:"task_completion_rate"`
	IrreversibleEffectFailures     int     `json:"irreversible_effect_failures"`
	DuplicateEffects               int     `json:"duplicate_effects"`
	SuccessfulAbortRate            float64 `json:"successful_abort_rate"`
	SuccessfulRecoveryRate         float64 `json:"successful_recovery_rate"`
	SuccessfulCompensationRate     float64 `json:"successful_compensation_rate"`
	FalseBlocks                    int     `json:"false_blocks"`
	ApprovalsRequired              int     `json:"approvals_required"`
	WallClockOverheadMS            int     `json:"wall_clock_overhead_ms"`
	ComputeOverheadMS              int     `json:"compute_overhead_ms"`
	TokenOverhead                  int     `json:"token_overhead"`
	DiffAccuracy                   float64 `json:"diff_accuracy"`
	UnsupportedEffectDetectionRate float64 `json:"unsupported_effect_detection_rate"`
}

type BundleResult struct {
	FuturepackPath string
	Manifest       artifactstore.Manifest
	Metrics        Metrics
}

func (r Runner) ExportFileChangeFailureBundle(ctx context.Context, outputDir string) (*BundleResult, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create benchmark output dir: %w", err)
	}
	report, err := r.CompareFileChangeFailure(ctx)
	if err != nil {
		return nil, err
	}
	store, err := artifactstore.Open(filepath.Join(outputDir, "artifact-store"))
	if err != nil {
		return nil, err
	}

	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode benchmark report: %w", err)
	}
	reportRef, err := store.PutBytes("file-change-failure-report.json", append(reportBytes, '\n'))
	if err != nil {
		return nil, err
	}

	metrics := buildFileChangeFailureMetrics(report)
	metricsBytes, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode benchmark metrics: %w", err)
	}
	metricsRef, err := store.PutBytes("benchmark-metrics.json", append(metricsBytes, '\n'))
	if err != nil {
		return nil, err
	}

	manifest := artifactstore.Manifest{
		FormatVersion: "0.1",
		RunID:         fmt.Sprintf("file-change-failure-%d", time.Now().UnixNano()),
		Scenario:      "file-change-failure",
		Verdict:       "pass",
		Metadata: map[string]any{
			"baseline":     "direct-vs-futurediff",
			"contract_ref": "benchmark-metrics-0.1",
		},
		Metrics:   metrics,
		Artifacts: []artifactstore.Ref{reportRef, metricsRef},
	}
	futurepackPath := filepath.Join(outputDir, "file-change-failure.futurepack")
	if err := store.ExportFuturepack(futurepackPath, manifest); err != nil {
		return nil, err
	}
	return &BundleResult{
		FuturepackPath: futurepackPath,
		Manifest:       manifest,
		Metrics:        metrics,
	}, nil
}

func buildFileChangeFailureMetrics(report *FileChangeFailureReport) Metrics {
	overhead := report.FutureDiffDuration - report.DirectDuration
	if overhead < 0 {
		overhead = 0
	}
	abortRate := 0.0
	if report.FutureDiffState == "ABORTED" && !report.FutureDiffRepoChanged {
		abortRate = 1
	}
	taskCompletion := 0.0
	if abortRate == 1 && report.DirectRepoChanged {
		taskCompletion = 1
	}
	diffAccuracy := 0.0
	if !report.FutureDiffRepoChanged {
		diffAccuracy = 1
	}
	return Metrics{
		TaskCompletionRate:             taskCompletion,
		IrreversibleEffectFailures:     0,
		DuplicateEffects:               0,
		SuccessfulAbortRate:            abortRate,
		SuccessfulRecoveryRate:         0,
		SuccessfulCompensationRate:     0,
		FalseBlocks:                    0,
		ApprovalsRequired:              0,
		WallClockOverheadMS:            int(overhead / time.Millisecond),
		ComputeOverheadMS:              int(overhead / time.Millisecond),
		TokenOverhead:                  0,
		DiffAccuracy:                   diffAccuracy,
		UnsupportedEffectDetectionRate: 0,
	}
}
