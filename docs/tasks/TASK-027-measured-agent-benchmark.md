# Task 027 — Measured agent benchmark ingestion

## Objective

Create a reproducible format for real OpenCode, Hermes, or other agent runs without fabricating token or latency measurements.

## Delivered

- `futurediff-agent-bench`
- Versioned run schema `0.1`
- Input/output/cached token fields
- Model-call, tool-call, repair-turn, verification, compute, and wall-time fields
- Released, unsafe, and duplicate effect metrics
- Success-rate aggregation
- Token and wall-time overhead relative to a named baseline
- JSON and Markdown reports
- Duplicate-run and negative-value rejection

## Example only

The included example shows 8% token overhead and 30% wall-time overhead for one illustrative run. These numbers are demonstration data and are not presented as a measured FutureDiff performance result.

## Remaining certification

Actual OpenCode and Hermes runs must populate this format using provider/model usage records.
