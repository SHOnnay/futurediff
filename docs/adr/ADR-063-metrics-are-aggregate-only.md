# ADR-063 — Metrics are aggregate only

The local metrics surface exports counts and status labels only. High-cardinality identifiers, paths, destinations, user content, and credential metadata are excluded to reduce privacy and secret-leak risk.
