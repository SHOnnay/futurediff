# ADR-088 — Delegated capabilities are short-lived and single-use

A capability is bound to one UID, operation, and optional resource, expires within 15 minutes, and is consumed durably before the protected handler runs.
