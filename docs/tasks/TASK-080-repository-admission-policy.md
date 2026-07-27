# Task 080 — Repository admission policy

## Goal
Prevent the daemon from staging arbitrary repositories merely because the authenticated local process can name their paths.

## Implemented

A versioned repository policy can constrain:

- allowed canonical repository roots;
- allowed Git object formats (`sha1` and/or `sha256`);
- detached-HEAD admission;
- `stage_from_head` dirty-worktree admission.

The policy evaluates the canonical repository path returned by hardened Git inspection, not the untrusted request string. Path containment uses `filepath.Rel` boundary checks after symlink resolution.

Daemon configuration:

```bash
futurediffd --repository-policy /absolute/repository-policy.json
```

The policy participates in detached signed-configuration verification when `--require-signed-configs` is enabled. The installer and configuration linter understand it.

## Validation

A repository inside the configured root was accepted and produced HTTP 201. A valid repository outside the root was rejected before workspace creation.
