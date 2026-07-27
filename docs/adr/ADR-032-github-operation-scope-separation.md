# ADR-032: Separate GitHub read, status, and mutation scopes

**Status:** Accepted

## Decision

The GitHub built-in adapter uses separate credential broker operations for ref reads, PR status queries, and draft-PR creation.

```text
github.read_refs
github.query_pull_requests
github.create_draft_pull_request
```

A mutation grant cannot be reused as a generic GitHub API capability.

## Consequences

- each provider request has a matching durable audit decision;
- credential configuration is more verbose;
- future GitHub operations require explicit new scopes.
