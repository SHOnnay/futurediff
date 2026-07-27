# Task 016 — Deterministic effect-safety benchmark

## Goal

Create an honest benchmark that demonstrates FutureDiff's release semantics without pretending to measure LLM intelligence or real provider performance.

## Implemented

- New `futurediff-bench` command.
- Versioned JSON scenario format.
- Four comparison modes: direct execution, permission-only, sandbox-only, and FutureDiff.
- Metrics for released effects, unsafe effects, duplicate effects, human approvals, compensation requirements, recovery, and live-repository modification after failure.
- Scenarios for failed verification, lost-response retry, and a successful repository/PR/notification workflow.
- JSON and Markdown reports with a content digest.

## Result

In the synthetic failed-verification scenario, FutureDiff released zero external effects and left the live repository unchanged. Direct and permission-only modes released an external effect before verification failed. In the lost-response scenario, FutureDiff produced one effect while the other modeled modes produced a duplicate.

## Limitation

This is a deterministic semantic model. It does not measure model accuracy, token overhead, real provider latency, or end-to-end autonomous-agent success. Those remain separate benchmark layers.
