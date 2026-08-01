# ADR 0003: Unified `fdif` home and safe path canonicalization

- Status: accepted for `v0.1.0-alpha.3`
- Date: 2026-08-01

## Context

The first public alpha exposed three user-facing inconsistencies:

1. documentation implied that bare `fdif` always opened a menu, while
   non-interactive execution failed;
2. a normal macOS path beneath `/tmp` was rejected because `/tmp` is an
   operating-system symlink to `/private/tmp`;
3. `--state` moved the current-selection file but left daemon workspaces under
   the default root, which made configuration appear inconsistent.

The existing symlink rejection protected against path redirection, so simply
allowing all symlinks was not acceptable.

## Decision

### One effective home

Resolve a single FutureDiff home with this precedence:

```text
--home / --root
FDIF_HOME
FUTUREDIFF_ROOT (legacy)
~/.futurediff
```

Derive the current-selection file, daemon socket, runtime directory, workspace
root, and daemon log from that home unless a narrowly scoped advanced override
is supplied.

### Keep `--state`, but name its scope truthfully

`--state` remains for compatibility and changes only the local
current-selection file. Documentation and `fdif config --explain` must say so.

### Canonicalize trusted platform aliases only

Before use, resolve the longest existing path prefix. Permit only known
operating-system aliases required for normal platform behavior, including
macOS `/tmp`, `/var`, and `/etc`. Reject arbitrary symlinked parent components
and a configured home that is itself a symlink.

Use and display the canonical path after resolution.

### Separate starting screen from menu

Bare `fdif` always prints a deterministic starting/continue screen. The
interactive numbered menu moves to explicit `fdif menu`.

### Progressive disclosure

Normal output emphasizes the safe workspace, unchanged current branch, and
next commands. `--verbose` and `--json` expose transaction and configuration
details.

## Security consequences

Positive:

- normal macOS temporary paths work without enabling general symlink traversal;
- daemon and guided CLI share one canonical root and socket;
- effective paths are observable;
- final state-file symlinks remain rejected;
- private daemon-root permissions remain required.

Residual risks:

- cooperative mode still grants an editor or agent the user's ordinary OS
  permissions;
- path validation cannot eliminate every same-user time-of-check/time-of-use
  race;
- explicit socket and selection-file overrides can intentionally leave the
  unified home and are therefore advanced features.

## Rejected alternatives

- Allow every symlink after `EvalSymlinks`: too broad.
- Remove `--state`: unnecessary compatibility break.
- Make bare `fdif` depend on TTY detection: caused the original documentation
  mismatch.
- Add separate public state/runtime/workspace root flags immediately: too much
  surface area for the alpha; one home is easier to reason about.
