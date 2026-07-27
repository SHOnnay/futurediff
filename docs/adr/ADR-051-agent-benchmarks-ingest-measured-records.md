# ADR-051: Agent benchmarks ingest measured records

FutureDiff does not infer token use, latency, repair turns, or effect counts. The agent benchmark consumes versioned measured-run records and computes aggregate overhead relative to a named baseline. Synthetic safety benchmarks remain separate from real-agent performance evidence.
