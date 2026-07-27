# ADR-079: Release readiness is manifest-driven

The release-readiness gate combines audit health, SLO policy, API-contract identity, maintenance state, and optional signed operator receipts. A failed prerequisite makes the report non-ready; the gate does not auto-remediate.
