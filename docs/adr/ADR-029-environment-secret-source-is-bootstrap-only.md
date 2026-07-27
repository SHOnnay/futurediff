# ADR-029 — Environment secret source is bootstrap-only

**Status:** Accepted

## Decision

Task 009 supports an environment-variable secret source because it requires no third-party dependency and is testable offline. The configuration file stores the variable name, while SQLite stores only its SHA-256 identity digest. The daemon does not inherit its full environment into Git, verification, or OCI runtime subprocesses.

## Consequences

- Operators must protect the daemon environment and the 0600 credential configuration file.
- Error messages never reveal the variable name or value.
- OS keychain, Vault, cloud secret manager, and short-lived identity federation are required before production-grade deployment.
