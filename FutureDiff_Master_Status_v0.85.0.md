# FutureDiff master status — v0.85.0

## Current characterization

**Feature-complete local MVP with external certification still pending.**

FutureDiff is a Go transactional effect layer that stages autonomous-agent changes as reviewable futures, verifies exact material, binds operator approval, and releases persistent effects through durable and recoverable workflows.

## Completed through Task 085

In addition to the capabilities completed through Task 080, v0.85.0 adds:

- safe policy-driven expiry for abandoned pre-commit transactions;
- explicit retention and cleanup of durable API idempotency records;
- a low-storage mutation circuit breaker with read availability;
- deterministic OpenAPI 3.1 generation, daemon serving, and conformance validation;
- verified backup catalog reconciliation and bounded backup retention.

The release contains 64 Go commands.

## Current completion

| Target | Complete | Remaining |
|---|---:|---:|
| Architecture and research | 99% | 1% |
| Public open-source MVP | 99.85% | 0.15% |
| Production-grade platform | 87% | 13% |

## Remaining public-MVP work

- real Docker-rootless and Podman-rootless certification;
- disposable GitHub branch/PR certification;
- disposable Slack exactly-once certification;
- complete live OpenCode and Hermes transactions;
- actual token, latency and compute measurements;
- native macOS CI execution and artifacts;
- hosted GitHub-signed attestation verification;
- final long-term SQLite driver decision.

## Remaining production work

- multi-user authentication and RBAC;
- short-lived workload identity and external secret managers;
- isolated, signed third-party adapter execution;
- distributed coordination and high availability;
- PostgreSQL and cloud persistence adapters;
- encrypted evidence-retention governance;
- monitoring, disaster recovery, penetration testing and independent security assessment;
- Windows named-pipe/service support;
- production user interface.
