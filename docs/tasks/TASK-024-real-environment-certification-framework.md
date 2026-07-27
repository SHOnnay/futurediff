# Task 024 — Real-Environment Certification Framework

## Delivered

- `futurediff-cert-suite` Go command
- versioned certification report format `0.1`
- report SHA-256 digest
- target-level and check-level certification states
- local MCP/integration contract certification
- rootless OCI certification orchestration
- read-only GitHub repository and push-permission readiness check
- read-only Slack identity and channel readiness check
- OpenCode and Hermes binary/profile readiness checks
- GitHub signed-attestation verification through `gh attestation verify`
- strict `pass`, `fail`, `blocked`, and `skip` semantics
- JSON Schema and operator examples
- release and CI integration

## Security properties

- Token values are never accepted as command-line flags.
- The user passes the name of the environment variable containing a token.
- Provider checks use the controlled egress transport.
- Child agent/version processes receive a minimal environment without provider
  token variables.
- Provider checks are read-only in the general suite.
- Missing live resources are reported as `blocked`, not `pass`.
- Approval and commit authority remain outside agent certification.

## What is not certified in this environment

Docker, Podman, OpenCode, Hermes, GitHub test credentials, Slack test credentials,
and GitHub signed release attestations were unavailable. The framework and its
injected conformance tests passed, but those external targets remain blocked
until run on their actual hosts.
