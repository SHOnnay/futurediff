# ADR-056: Artifact retention is plan-bound and confirmation-gated

Retention applies only to terminal transactions older than a chosen cutoff. FutureDiff first emits a deterministic plan digest. Application requires the exact phrase `PRUNE_TERMINAL_FUTUREDIFF_ARTIFACTS`, removes only managed runtime paths below the FutureDiff data root, preserves transaction metadata and published Git refs, and records a durable retention action.
