# Task 009 — Credential Broker and Built-in Adapter Trust Boundary

**Status:** Source implementation and automated validation complete  
**Architecture version:** 0.9  
**Production secret-manager integration:** Pending  
**Third-party adapter process isolation:** Pending

## Objective

Prevent autonomous agents and public daemon clients from receiving raw provider credentials. Provide a fail-closed path in which a trusted built-in adapter may use a narrowly scoped credential only after adapter identity, operation, destination, expiry, and durable audit requirements pass.

## Delivered

### Credential configuration v0.1

A strict 0600 JSON configuration declares:

- adapter identity and version;
- trust level;
- executable identity digest;
- credential provider/account metadata;
- secret-source reference;
- allowed adapter IDs;
- allowed operation IDs;
- exact HTTPS destination rules;
- expiry and enabled state.

Unknown fields, duplicate identifiers, unknown adapter references, unsafe destinations, and permissive file permissions are rejected.

### Secret handling

- Secret values are never stored in SQLite.
- SQLite stores a SHA-256 digest of the source reference, not the environment-variable name.
- Secret values are not returned by an HTTP endpoint.
- Secret values render as `[REDACTED]` through `String`, `GoString`, and JSON encoding.
- Trusted-adapter errors are scrubbed if they contain the secret.
- Secret memory owned by the broker is overwritten after the callback returns.

This memory overwrite is best effort because Go and provider libraries may create additional copies.

### Adapter trust

Protocol 0.1 grants credential use only to `built_in` adapters.

- `built_in`: eligible when enabled and scoped.
- `verified`: denied until signed executable verification and process isolation exist.
- `untrusted`: denied.

Existing adapter trust level or executable digest cannot silently change in the durable registry.

### Scope enforcement

Each request must match:

```text
adapter ID
credential ID
operation ID
HTTPS scheme
exact host
allowed path-prefix boundary
binding expiry
binding enabled state
```

The matcher rejects:

- HTTP;
- IP literals;
- userinfo;
- query strings and fragments;
- non-default ports;
- hostname suffix attacks;
- look-alike path prefixes.

### Durable audit

Before secret release, the broker writes a credential access event recording:

```text
transaction/effect identity when present
adapter ID
credential ID
operation
destination
granted / denied / error
reason
timestamp
```

If the audit write fails, credential release fails closed.

### Ambient-environment hardening

Task 009 removed full environment inheritance from:

- Git subprocesses;
- Docker/Podman readiness probes;
- OCI execution commands;
- local verification commands, which were already minimal.

This prevents environment-backed provider tokens from reaching unrelated child processes.

### Daemon integration

New daemon option:

```bash
futurediffd --credential-config /secure/path/credentials.json
```

Health output reports counts and `secret_values_persisted: false`; it does not expose source references or values.

### Ledger migration 0005

Added:

```text
adapter_identities
credential_bindings
credential_access_events
```

No table contains a secret-value column.

## Validation completed

```text
gofmt -w .
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
go test -cover ./...
go build ./cmd/futurediff
go build ./cmd/futurediffd
process-level daemon/CLI health smoke
SQLite plaintext secret/source-reference scan
JSON syntax validation
```

All available checks passed.

## Important limitations

1. Environment variables are not an ideal production secret source.
2. Built-in adapters execute in the daemon process; a defect could still access daemon memory.
3. Go memory zeroing is best effort and cannot erase copies created by libraries.
4. Destination authorization does not replace an allow-listed egress proxy.
5. No canonical GitHub or Slack adapter invokes the broker yet.
6. No key rotation, lease renewal, OAuth device flow, workload identity, or short-lived token exchange exists yet.
7. Windows ACL validation is not implemented.

## Exit criteria

| Criterion | Result |
|---|---|
| No raw-secret API | Passed |
| No raw secret in SQLite | Passed |
| Source reference not persisted in plaintext | Passed |
| 0600 configuration check | Passed on Unix |
| Built-in identity enforcement | Passed |
| Verified/untrusted denial | Passed |
| Operation scope enforcement | Passed |
| Destination scope enforcement | Passed |
| Expiry enforcement | Passed |
| Durable audit before release | Passed |
| Audit failure blocks release | Passed |
| Trusted-adapter error redaction | Passed |
| Ambient child-process environment removed | Passed |
| OS keyring/secret manager | Pending |
| Isolated third-party adapter host | Pending |
| Real provider adapter using broker | Pending |

## Next task

Task 010 should implement the durable external-effect coordinator and the first canonical GitHub draft-pull-request adapter behind this broker.
