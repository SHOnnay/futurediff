# ADR-027 — Only built-in adapters receive credentials in protocol 0.1

**Status:** Accepted

## Decision

The credential broker recognizes `built_in`, `verified`, and `untrusted` identity metadata, but protocol 0.1 releases secret material only to `built_in` adapters whose identity and executable digest were registered at daemon startup.

`verified` does not yet imply sufficient isolation because signed manifests, executable verification, and a capability-scoped adapter process are not implemented. Both verified and untrusted adapters are denied credential access.

## Consequences

- GitHub and Slack adapters must first enter the canonical codebase as reviewed built-ins.
- Third-party adapters may participate in observation or metadata-only workflows.
- A later ADR must define signed adapter packages and process isolation before `verified` access is enabled.
