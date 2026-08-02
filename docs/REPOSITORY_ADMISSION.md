# Repository admission

FutureDiff treats repository metadata as untrusted input. A repository can look ordinary in a working tree while Git metadata changes which history or objects commands resolve. Admission therefore runs before transaction creation, and the Git subprocess boundary independently disables replacement-object interpretation.

## Stable default

When no custom `--repository-policy` is configured, the transaction service constructs policy version `0.2` with fail-closed defaults before repository admission. The automatically generated policy admits ordinary local branches on the same filesystem volume as the daemon data root, while rejecting unsupported or history-shaping repository forms.

The stable default rejects:

- repositories outside the configured roots;
- detached `HEAD` and non-local source refs;
- `stage_from_head` transaction mode;
- linked worktree source repositories;
- shallow repositories;
- loose or packed `refs/replace/*` references;
- legacy graft files;
- alternate object databases;
- symlinked or missing Git object directories;
- unsupported object or reference formats;
- malformed or unreadable metadata used by these checks.

The default permits SHA-1 and SHA-256 object formats, the traditional `files` reference format, and local refs under `refs/heads/`.

## Custom policy

Use a custom policy only when the operator can define narrower repository roots or has reviewed a necessary exception:

```json
{
  "version": "0.2",
  "policy_id": "engineering-repositories",
  "allowed_roots": ["/home/alice/src"],
  "allowed_object_formats": ["sha1", "sha256"],
  "allowed_ref_formats": ["files"],
  "allowed_head_prefixes": ["refs/heads/"],
  "allow_detached_head": false,
  "allow_stage_from_head": false
}
```

Then start the daemon with:

```bash
futurediffd --repository-policy /absolute/path/repository-policy.json
```

Every `allow_*` field is an explicit risk acceptance. Keep omitted fields false unless the repository form is understood, tested, and required. Policy version `0.1` remains loadable for compatibility, but it does not enable the version `0.2` metadata checks and should not be used as the stable default.

## Policy-file safety

A policy file must:

- be a real regular file, not a symlink;
- be no larger than 1 MiB;
- contain exactly one JSON value with no unknown fields;
- not be group- or world-writable on POSIX systems;
- name existing absolute allowed-root directories for version `0.2`.

FutureDiff rechecks the opened file identity and mode to reduce file-substitution races. Signed configuration sidecars remain available through the daemon configuration-attestation controls.

## Decisions and operator guidance

Admission decisions expose:

- a stable `policy_id`;
- `allowed` status;
- machine-readable `reason_codes`;
- human-readable reasons;
- inspected facts such as common Git directory, reference format, shallow state, replacement refs, grafts, alternates, linked-worktree state, and object-directory symlink state.

An override should be made by changing and reviewing a custom policy, not by bypassing the admission call. The guided workflow still never directly mutates or automatically merges the current branch.

## Scope

These checks reduce repository-metadata ambiguity; they do not turn cooperative workspaces into operating-system sandboxes. Repository files and agent input remain untrusted, and same-user host compromise remains outside the stable local trust claim. Windows runtime and installer support are not claimed by the public alpha.
