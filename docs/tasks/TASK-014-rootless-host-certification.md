# Task 014 — Rootless OCI host certification harness

## Goal

Convert the OCI security policy from a source-level claim into a host-specific, machine-readable certification procedure.

## Implemented

- New `futurediff-certify` Go command.
- Docker-rootless and Podman-rootless runtime probing.
- Digest-pinned image requirement.
- Runtime, image, and host-specific JSON certification report.
- Required checks for rootless identity, workspace isolation, absence of Git metadata, ambient-secret isolation, controlled workspace synchronization, host-sentinel protection, symlink rejection, and sensitive-environment rejection.
- Active outbound-network denial check when the selected image contains `wget` or `curl`.
- Stable report digest and nonzero exit status when a required check fails.
- Unit tests through a deterministic executor.

## Validation in this environment

The command compiled and its unit tests passed. Docker and Podman are not installed, so the real host report correctly returned `certified=false` with `runtime_ready=fail`. This is expected and is not presented as rootless certification.

## Remaining work

Run the command on supported Linux hosts with real, digest-pinned images and publish the resulting reports for Docker rootless and Podman rootless.
