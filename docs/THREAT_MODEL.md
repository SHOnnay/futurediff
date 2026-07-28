# FutureDiff Threat Model

## Protected assets

FutureDiff protects source repositories, transaction intent, approval material, provider credentials, effect receipts, audit evidence, release artifacts, policy configuration, and the local ledger.

## Trust boundaries

1. Agent or model input is untrusted.
2. The FutureDiff transaction kernel and ledger are trusted only after local integrity checks.
3. OCI workspaces are isolated execution zones and must not receive repository metadata or provider credentials.
4. Provider adapters are separate effect boundaries.
5. CI runners and external providers are outside the local trust boundary.
6. Release artifacts are trusted only after manifest, SBOM, provenance, policy, and signature verification.

## Primary threats and controls

| Threat | Required control |
|---|---|
| Agent bypasses approval | No direct commit or credential capability; effect release remains kernel-controlled |
| Stale or substituted approval | Approval binds to canonical transaction and artifact digests |
| Repository path escape | Reject symlinks, special files, traversal paths, unsafe archives, and hidden Git metadata |
| Credential disclosure | Brokered credentials, minimized child environments, secret scanning, fingerprint-only findings |
| Duplicate external effect | Idempotency keys, durable attempts, receipts, and reconciliation |
| Unknown provider outcome | Persist unknown state; block dependent effects until reconciliation |
| Evidence tampering | SHA-256 manifests, deterministic bundles, provenance, optional signatures |
| CI dependency compromise | Pin workflow actions to reviewed major or immutable commit references |
| Backup corruption | Embedded manifest, safe extraction, full restore verification |
| Policy weakening | Versioned policies, reviewable diffs, readiness gate, release candidate evidence |

## Explicit non-claims

This overlay does not prove provider behavior, rootless-runtime behavior, native macOS behavior, or hosted attestation unless corresponding real external evidence is present and certified. Local simulation is not external certification.
