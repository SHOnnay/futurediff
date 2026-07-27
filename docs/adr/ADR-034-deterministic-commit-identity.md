# ADR-034 — Deterministic Commit Identity Before Publication

**Status:** Accepted

## Decision

FutureDiff creates an unreachable deterministic Git commit object from the approved staged tree before external-effect preparation. The same identity is later used for local ref materialization and remote branch publication.

## Rationale

A pull request and remote branch must bind to the exact commit produced by the approved patch. Predicting the commit avoids publishing any ref before approval while still allowing provider effects to include the exact SHA.

## Consequences

- commit metadata must be deterministic and versioned;
- patch timestamp becomes approval material;
- changing commit-generation rules is a protocol change;
- object creation is allowed before approval, but ref publication is not.
