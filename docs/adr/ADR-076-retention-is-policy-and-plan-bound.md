# ADR-076: Retention is policy- and plan-bound

Artifact deletion remains a two-stage operation. A versioned retention policy produces a deterministic plan and can cap candidate count and bytes. Apply is disabled unless the policy explicitly permits it and the exact pruning confirmation is supplied.
