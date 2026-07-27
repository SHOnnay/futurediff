# ADR-025: Rootless OCI is the enforced execution boundary

**Status:** Accepted

## Decision

FutureDiff may create an `enforced` transaction only when a configured Docker or Podman backend is detected as rootless and the runtime image is pinned by digest.

The daemon—not the agent—constructs the runtime command. The enforced command includes a read-only container root, disabled network, all capabilities dropped, no-new-privileges, bounded CPU/memory/PIDs/tmpfs, a sanitized workspace, and no inherited host environment.

## Safety boundary

The OCI runtime receives a copied execution view. It does not receive the live checkout, Git metadata, FutureDiff ledger, host home directory, production credentials, or arbitrary host mounts.

Mutation commands may synchronize a successful sanitized result into the transaction worktree. Verification commands never synchronize. Timeout, cancellation, runtime error, invalid workspace content, or nonzero mutation exit fail closed.

## Qualification

Fake-runtime integration tests validate command construction, orchestration, evidence, and synchronization rules. They do not certify Linux-kernel, namespace, cgroup, seccomp, Docker, or Podman isolation. A real rootless host must pass `scripts/certify-rootless-oci.sh` before enforced mode is advertised as certified.
