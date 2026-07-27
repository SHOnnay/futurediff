# Task 063 — Effect dependency graph

## Goal
Make a composed FutureDiff transaction understandable without relying on the future UI.

## Implemented
- Read-only graph projection
- Transaction, repository, verification, approval, and effect nodes
- Explicit effect dependency edges
- Stable graph digest
- JSON, Mermaid, and Graphviz DOT output
- `futurediff-effect-graph` command

## Privacy
Provider payload bodies, patches, credentials, and secret values are not rendered.
