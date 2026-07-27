# Task 075 — Secure data-root audit

## Goal
Fail closed when the local trust root is exposed through unsafe ownership, permissions, symlinks, or special filesystem entries.

## Implemented

`futurediff-root-audit` checks:

- absolute data-root path;
- real directory rather than a symlink;
- private root permissions (`0700` or stricter);
- expected Unix UID ownership;
- absence of top-level symlinks;
- absence of group/world-writable entries;
- private permissions for credential and key material;
- absence of devices and FIFOs in the top level.

The daemon runs this audit by default before acquiring its instance lock or opening SQLite. Operators can disable the startup requirement only with the explicit `--require-secure-root=false` flag.

SQLite ledger creation now normalizes `ledger.db` to mode `0600`.

## Validation

A normal data root passed before and during daemon execution. Changing the root to mode `0755` caused the standalone command and daemon startup boundary to fail closed. A top-level symlink is also rejected in unit tests.
