# ADR-048 — Certification is targeted and evidence-bound

## Status

Accepted.

## Decision

FutureDiff certification is represented as a versioned, digest-bound report with
one result per target and one result per check. Status values are `pass`, `fail`,
`blocked`, and `skip`.

A missing runtime, provider token, test repository, test channel, agent binary,
or signed attestation is `blocked`; it is never converted into `pass`.
Certification targets are opt-in so a local build can prove its local contract
without claiming provider or host certification.

Provider readiness checks are read-only. Destructive or externally visible
certification requires a separate explicit disposable-resource run and is not
silently triggered by the general suite.

## Consequences

- CI can validate the local contract deterministically.
- Operators can attach one machine-readable report to a release or host.
- Source support and real-environment certification remain distinct facts.
- Credential values are read just in time from named environment variables and
  are not written to the report.
