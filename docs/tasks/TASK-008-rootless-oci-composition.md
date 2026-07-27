# Task 008 — Rootless OCI Composition in Go

**Status:** Source implementation and automated orchestration validation complete  
**Architecture version:** 0.8  
**Real rootless host certification:** Pending

## Objective

Connect the Go daemon to a rootless Docker or Podman execution boundary so agent-authored mutation and verification commands do not run directly on the host.

## Delivered

### Runtime readiness

The daemon accepts:

```text
--runtime docker|podman
--runtime-binary <optional path>
--runtime-image <name@sha256:digest>
```

When an image is configured, startup fails unless:

- the runtime binary is available;
- the runtime reports a version;
- rootless operation is detected;
- the image is pinned by SHA-256 digest;
- the policy passes fail-closed validation.

Without a certified runtime configuration, cooperative mode remains available and enforced transaction creation is rejected.

### Runtime security command

FutureDiff constructs the command and enforces:

```text
--pull=never
--read-only
--network none
--cap-drop ALL
--security-opt no-new-privileges
--pids-limit
--memory
--cpus
--tmpfs /tmp
--init
```

The container does not inherit the host environment. Explicit environment variables are checked against sensitive-key rules.

### Sanitized execution view

The runtime receives a copied temporary workspace. Copying rejects:

- `.git` and `.futurediff` entries;
- symbolic links;
- sockets, devices, FIFOs, and unsupported special files;
- paths escaping the workspace.

The live repository and Git common directory are never mounted.

### Mutation behavior

For a successful mutation command:

1. copy transaction workspace into OCI scratch;
2. execute in the rootless container;
3. validate the resulting filesystem;
4. synchronize it into the transaction worktree;
5. preserve Git metadata outside the execution view;
6. record durable evidence.

The workspace is not synchronized after:

- nonzero mutation exit;
- timeout;
- cancellation;
- runtime failure;
- evidence mismatch;
- unsafe result tree.

### Verification behavior

`oci_command` checks use the same runtime boundary but never synchronize their files back, even when the command passes.

### Durable evidence

Migration 0004 adds runtime-execution records containing:

- execution ID and transaction ID;
- runtime kind/version/rootless identity;
- image digest;
- command, environment, and policy digests;
- exit code and termination reason;
- timing and output sizes;
- truncation status;
- workspace synchronization result;
- evidence path.

Full bounded stdout, stderr, and JSON evidence are stored beneath the transaction artifact directory.

### API and CLI

Added:

```text
POST /v1/transactions/{id}/execute
futurediff execute <transaction-id> <command...>
```

Health output now includes runtime readiness and rootless backend information.

### Host certification

`scripts/certify-rootless-oci.sh` performs a real process-level certification on a Docker/Podman host. It proves:

- the daemon reports rootless readiness;
- enforced transaction creation succeeds;
- `.git` is absent from the container view;
- a mutation changes only the staged worktree;
- OCI verification passes;
- approval and exact Git-ref publication work;
- the live checkout remains unchanged.

## Automated validation completed

```text
gofmt -w .
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
go test -cover ./...
go build ./cmd/futurediff
go build ./cmd/futurediffd
bash -n scripts/certify-rootless-oci.sh
```

All available checks passed.

## Important limitation

Docker and Podman were unavailable in the execution environment. Tests use a fake OCI executable that validates command construction, synchronization rules, evidence persistence, API composition, and failure behavior. It does not prove Linux namespace, cgroup, seccomp, or real container-engine isolation.

FutureDiff must not describe enforced mode as host-certified until the certification script passes on the intended runtime and operating system.

## Files changed

Key additions and changes:

```text
internal/runtimeoci/runtime.go
internal/runtimeoci/runtime_test.go
internal/domain/model.go
internal/ledger/migrations/0004_oci_execution_runtime.sql
internal/ledger/repository.go
internal/app/service.go
internal/app/enforced_test.go
internal/api/server.go
internal/api/e2e_test.go
internal/verification/verification.go
cmd/futurediff/main.go
cmd/futurediffd/main.go
scripts/certify-rootless-oci.sh
```

## Exit criteria status

| Criterion | Result |
|---|---|
| Go daemon composes OCI runtime | Passed in source and fake-runtime tests |
| Enforced creation requires rootless readiness | Passed |
| Digest-pinned image required | Passed |
| Live checkout/Git metadata not exposed | Passed in filesystem/orchestration tests |
| Network disabled | Command-plan validation passed |
| Credentials not inherited | Environment policy tests passed |
| Mutation sync only after success | Passed |
| Verification never syncs | Passed |
| Evidence persisted durably | Passed |
| Race tests | Passed |
| Real Docker rootless certification | Pending |
| Real Podman rootless certification | Pending |

## Next task

Task 009 should implement the credential broker and adapter process boundary before porting GitHub, Slack, and PostgreSQL spikes into the canonical transaction path.
