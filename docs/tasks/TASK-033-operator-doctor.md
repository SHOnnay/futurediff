# Task 033 — Operator doctor

`futurediff-doctor` provides a machine-readable readiness report covering:

- private data-root permissions;
- Git executable and version;
- linked SQLite version;
- ledger integrity and invariants;
- daemon socket permission and health;
- credential metadata file permission;
- optional rootless Docker or Podman probe.

Warnings do not masquerade as successful certification. Provider secrets are never resolved or displayed.
