# ADR-028 — Credential grants require exact operation and destination scopes

**Status:** Accepted

## Decision

A credential binding declares allowed adapter IDs, exact operation IDs, and HTTPS destination rules. Destination matching requires an exact normalized hostname and a path-prefix boundary. Userinfo, query strings, fragments, non-default ports, IP literals, subdomain confusion, and look-alike path prefixes are rejected.

## Consequences

- A GitHub credential scoped to `https://api.github.com/repos` cannot be used for another host, `/repos-evil`, or an arbitrary provider operation.
- Adapters must authorize the canonical provider endpoint before adding request-specific query data.
- Controlled network egress is still required as a second independent layer.
