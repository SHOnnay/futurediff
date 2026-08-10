# Compatibility and Deprecation Policy

FutureDiff is a local-first product under `v0.x` alpha versioning. This policy
defines what the public product promises about compatibility, how deprecations
are announced and executed, and the uninstall contract. It is consistent with
[`LIMITATIONS.md`](LIMITATIONS.md) (supported scope) and
[`STABLE_READINESS.md`](STABLE_READINESS.md) (what must be true before a stable
release).

## Versioning semantics

- All versions are `v0.x` prereleases until the stable-readiness milestone
  passes. Pre-release versions carry a qualifier such as `-alpha.N`; the
  leading `v` is part of the public version string (`v0.1.0-alpha.3`).
- Within `v0.x`, minor releases may add functionality. No API or behavioral
  compatibility guarantee is implied until `v1.0.0`; this policy exists so
  that even alpha users get a written, deterministic removal process instead
  of silent breakage.
- The embedded build identity (`internal/buildinfo` Version/Commit/Date) is
  part of every released binary; `fdif version` must report a released
  version string.

## Supported surfaces

The following surfaces are in the supported scope and receive the
compatibility treatment below:

- the `fdif` guided CLI and the `futurediff` / `futurediffd` binaries
  distributed by the packaged release archive
  (`futurediff-<version>-<os>-<arch>.tar.gz` with its `.sha256` sidecar);
- the documented installer (`scripts/install-release.sh`) and its
  checksum-verified download flow;
- the FutureDiff home (`$FDIF_HOME`, default `~/.futurediff`) layout:
  daemon root, socket, current-selection file, runtime directory, and safe
  workspaces move together when `--home` / `FDIF_HOME` is set;
- the local operator audit trail (append-only JSONL with hash chaining);
- local repository admission and the guided publication flow (local branch
  publish and GitHub draft pull request via an explicitly configured
  credential);
- the packaged runtime targets: Linux AMD64, Linux ARM64, macOS AMD64, and
  macOS ARM64.

Explicitly excluded from supported-surface compatibility guarantees:

- the Slack message outbox is **experimental**, not a supported provider
  surface; its deterministic coverage is recorded, real-mutation
  certification remains blocked on dedicated test credentials, and Slack
  delivery is not guaranteed (see `LIMITATIONS.md`);
- Windows runtime and installer support: no Windows claim is made and no
  Windows compatibility guarantee exists (see `STABLE_READINESS.md`);
- hosted, team, or multi-tenant operation; exposing `futurediffd` over a
  network;
- `--state` compatibility relocation: accepted for existing scripts but not a
  supported surface for new integrations.

## Per-surface compatibility guarantees

| Surface | Guarantee within `v0.x` |
|---|---|
| Packaged binaries and installer | The release archive layout (`bin/{fdif,futurediff,futurediffd}`, `LICENSE`, `README`, `VERSION`) and the checksum-verified install flow are stable for the supported targets. |
| `fdif` guided CLI | Core flows (`start`, workspace edit, `finish`, recovery, seal/verify/approve) keep their documented shape; changes are announced per the deprecation process below. |
| FutureDiff home layout | The `$FDIF_HOME` relocation contract is stable; `--state` remains a compatibility alias, not a new integration surface. |
| Audit trail | Append-only, hash-chained record format is stable; verification tooling is provided separately from diagnostics. |
| Credential configuration | The GitHub credential config contract (explicitly configured credential + repository allowlist) is stable. |

## Deprecation process

A supported surface is removed only through a written process:

1. **Announcement**: the removal is announced in the release notes and in
   this policy document at least one `v0.x` minor release before removal.
2. **Removal window**: the deprecated surface keeps working for at least the
   next two `v0.x` releases after announcement, and emits an explicit warning
   naming the replacement.
3. **Removal**: the surface is removed in a release whose notes link this
   policy and name the migration path. Removal never happens in a patch or
   hotfix release.

The first stable release (`v1.0.0`) may additionally reclassify `v0.x`
experimental surfaces; anything listed above as supported is carried forward
or moved through this same process.

## Uninstall contract

FutureDiff installs per-user via `scripts/install-release.sh` into a
`--prefix` (default `$HOME/.local`). Uninstall is defined as exactly:

1. remove `$prefix/bin/fdif`, `$prefix/bin/futurediff`, and
   `$prefix/bin/futurediffd`;
2. remove the FutureDiff home (`$FDIF_HOME`, default `~/.futurediff`) —
   daemon root, socket, current-selection file, runtime directory, safe
   workspaces, and the operator audit trail it contains.

There are **no package-manager hooks**: no service registration, no launch
agents, no shell-profile edits, and no other system-wide side effects are
created by install or removed by uninstall. After uninstall, `fdif`,
`futurediff`, and `futurediffd` are no longer resolvable from the prefixed
`bin` directory and the FutureDiff home no longer exists.

The certification drill
(`scripts/certify-release-supply-chain.sh`, evidence under
`docs/certification/release-supply-chain-<ts>/`) executes exactly this
contract against the published `v0.1.0-alpha.1` → `v0.1.0-alpha.3` assets.

## Windows

Windows runtime and installer support are deferred and unsupported. No
Windows compatibility or uninstall contract exists; a Windows claim requires
native runtime, installer, peer-auth, and hosted release validation first
(see `STABLE_READINESS.md`).
