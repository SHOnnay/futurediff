# ADR-062 — Ambiguous effects are never blindly retried

Recovery planning treats transport ambiguity as a status-query problem. Re-arm is allowed only when provider evidence proves no mutation occurred. Insufficient evidence escalates rather than guessing.
