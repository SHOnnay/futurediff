# Benchmark Evidence Export Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This result records the first exportable benchmark evidence path for the file-change failure smoke scenario.

## Implemented

- `futurediff/benchmarks/smoke/export.go`
- `futurediff/benchmarks/smoke/export_test.go`

## Proven behavior

- the file-change failure benchmark can emit normalized metrics;
- the benchmark report and metrics are stored as durable artifacts;
- a `.futurepack` bundle is exported with:
  - `manifest.json`
  - stored artifact entries
  - embedded metrics
- the exported bundle is readable and verifiable in test.

## Verification

- `go test ./...` passes, including `benchmarks/smoke`.

## Why this matters

This is the first working path from benchmark execution into a portable evidence artifact. That closes the loop between safety claims and something you can actually archive, inspect, and publish.

## Next useful move

Expand the bundle exporter to include more benchmark scenarios and shared evidence manifests for cross-tool runs.
