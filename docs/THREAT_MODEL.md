# FutureDiff Threat Model

## Protected assets

FutureDiff protects source repositories, transaction intent, approval material,
provider credentials, effect receipts, audit evidence, release artifacts,
policy configuration, local path configuration, and the local ledger.

## Trust boundaries

1. Agent or model input is untrusted.
2. The FutureDiff transaction kernel and ledger are trusted only after local
   integrity checks.
3. Cooperative Git workspaces isolate repository changes but are not operating-
   system sandboxes.
4. Enforced OCI workspaces are experimental isolated execution zones and must
   not receive repository metadata or provider credentials.
5. Provider adapters are separate effect boundaries.
6. CI runners and external providers are outside the local trust boundary.
7. Release artifacts are trusted only after checksum, provenance, content, and
   version verification.

## Local path boundary

The guided CLI resolves one canonical FutureDiff home and passes it to the
local daemon. The current-selection file, default socket, runtime directory,
and safe-workspace root are derived from that home.

Normal operating-system aliases are not automatically attacker-controlled. In
particular, macOS exposes `/tmp` through `/private/tmp`. FutureDiff therefore
allows a narrowly enumerated set of platform aliases, resolves them before
use, and displays the canonical result.

It does **not** follow arbitrary symlinked path components. A configured home
that is itself a symlink, a user-created symlinked parent, and a symlink
current-selection file remain rejected. The daemon root must remain a private
real directory.

Residual risks include same-user time-of-check/time-of-use races and files an
editor or agent can access through the user's ordinary OS permissions.

## Primary threats and controls

| Threat | Required control |
|---|---|
| Agent bypasses approval | No direct commit or credential capability; effect release remains kernel-controlled |
| Stale or substituted approval | Approval binds to canonical transaction and artifact digests |
| Repository path escape | Reject arbitrary symlinks, special files, traversal paths, unsafe archives, and hidden Git metadata |
| Platform path alias rejected as hostile | Enumerate trusted OS aliases, canonicalize before use, and test native macOS behavior |
| Custom selection path misrepresented as workspace root | One home model, scoped `--state` compatibility option, and `config --explain` source reporting |
| Unsafe daemon root | Canonical real directory, private permissions, local socket, and daemon secure-root audit |
| Credential disclosure | Brokered credentials, minimized child environments, secret scanning, fingerprint-only findings |
| Duplicate external effect | Idempotency keys, durable attempts, receipts, and reconciliation |
| Unknown provider outcome | Persist unknown state; block dependent effects until reconciliation |
| Evidence tampering | SHA-256 manifests, deterministic bundles, provenance, and optional signatures |
| CI dependency compromise | Pin workflow actions to reviewed major or immutable commit references |
| Backup corruption | Embedded manifest, safe extraction, and full restore verification |
| Policy weakening | Versioned policies, reviewable diffs, readiness gate, and release-candidate evidence |

## Explicit non-claims

This repository does not prove provider behavior, rootless-runtime behavior,
native platform behavior, or hosted attestation unless corresponding real
external evidence is present and certified. Local simulation is not external
certification. Cooperative workspace isolation is not a complete filesystem or
process sandbox.
