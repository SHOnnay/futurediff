# Containerized Runtime Hardening Results

## Status

Implemented and verified in the bootstrap repo.

## Scope covered

This spike records the first hardened Docker-compatible runtime seam for staged command execution.

## Implemented

- `futurediff/adapters/runtime/dockerrun/runtime.go`
- `futurediff/adapters/runtime/dockerrun/runtime_test.go`

## Proven behavior

- Docker availability can be probed explicitly;
- hardened container run plans are built with:
  - `--network none`
  - `--cap-drop ALL`
  - `--security-opt no-new-privileges`
  - `--read-only`
  - `--tmpfs /tmp`
  - explicit workdir bind mount
- runtime execution can be driven through an injected runner for deterministic tests;
- unavailability is reported honestly instead of being silently ignored.

## Verification

- `go test ./...` passes, including `adapters/runtime/dockerrun`.

## Why this matters

This closes the gap between “we intend to harden runtime isolation later” and “we now have a concrete, testable seam for containerized command isolation.”

## Next useful move

Promote the Docker-backed executor from an optional injected seam into a supported runtime policy with explicit CI and operator guidance.
