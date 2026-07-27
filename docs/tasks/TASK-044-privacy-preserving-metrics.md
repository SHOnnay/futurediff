# Task 044 — Privacy-Preserving Metrics

## Goal

Expose operational health without exporting transaction content, paths, recipients, or credentials.

## Delivered

- Aggregate counts for transactions, effects, verification runs, OCI executions, and credential-access decisions.
- Unknown-effect and unresolved-transaction gauges.
- JSON and Prometheus text formats.
- `futurediff-metrics` command.

## Privacy boundary

Metrics contain statuses and counts only. They do not include transaction IDs, effect IDs, repository paths, provider destinations, prompts, message bodies, or credential identities.
