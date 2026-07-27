# Phase 7 Research Outputs

## Purpose

This folder holds concrete research outputs for the MVP implementation phase.

The rule for every file here is simple:
- it must reduce build-critical uncertainty;
- it must end in a decision, a bounded risk, or a required spike.

## Current outputs

- `DR-001-mvp-interception-strategy.md` — chooses the wrapper/proxy path as the primary MVP interception model.
- `DR-002-mvp-control-plane-stack.md` — chooses Go + Postgres + DB-driven workers + S3-compatible artifact storage for MVP.
- `mvp-adapter-reality-matrix.md` — assigns honest support levels and risks to the MVP adapters.
- `recovery-and-idempotency-note.md` — sharpens retry, reconciliation, `UNKNOWN`, and manual-intervention behavior.
- `local-staging-runtime-plan.md` — fixes the practical local runtime model for MVP development and CI.
- `benchmark-feasibility-split.md` — separates early benchmarkable scenarios from later/non-blocking ones.
- `implementation-risk-register.md` — initial implementation risk register tied to the research phase.
- `machine-readable-spec-conversion-plan.md` — conversion order from prose contracts into schemas, transition tables, and normalization fixtures.
- `bootstrap-spike-list.md` — ordered spikes that de-risk the first implementation cycle.
- `first-repository-instantiation-checklist.md` — concrete checklist for the first repo bootstrap pass.
- `priority-1-spike-results.md` — implemented results and verification for S-001, S-002, and S-003.
- `priority-2-spike-results.md` — implemented and verified results for the database preview, GitHub duplicate-recovery, and Slack ambiguous-send spikes.
- `failed-verification-path-results.md` — implemented and verified results for the first aborted-on-verification failure path.
- `first-cross-tool-vertical-slice-results.md` — implemented and verified results for the first multi-effect preparation flow.
- `first-benchmark-smoke-results.md` — implemented and verified results for the first benchmark smoke comparison.
- `destructive-shell-containment-smoke-results.md` — implemented and verified results for destructive shell containment smoke.
- `benchmark-evidence-export-results.md` — implemented and verified results for exportable benchmark evidence bundles.
- `artifact-store-spike-results.md` — implemented and verified results for the durable local artifact store.
- `local-dev-stack-spike-results.md` — implemented and verified results for the local contributor bootstrap stack.
- `migration-failure-smoke-results.md` — implemented and verified results for the migration-failure smoke scenario.
- `cross-tool-evidence-export-results.md` — implemented and verified results for the exportable cross-tool transaction bundle.
- `duplicate-retry-smoke-results.md` — implemented and verified results for duplicate API retry smoke.
- `stale-github-drift-smoke-results.md` — implemented and verified results for stale GitHub base drift smoke.
- `multi-effect-commit-orchestration-results.md` — implemented and verified results for the first real multi-effect commit path.
- `partial-commit-reconciliation-results.md` — implemented and verified results for the first partial-commit reconciliation path.
- `approval-invalidation-results.md` — implemented and verified results for the first stale-approval invalidation path.
- `containerized-runtime-hardening-results.md` — implemented and verified results for the first hardened Docker-compatible runtime seam.
- `compensation-policy-results.md` — implemented and verified results for the first compensation policy path.
- `coordinator-approval-state-results.md` — implemented and verified results for coordinator-owned approval state.
- `container-runtime-wiring-results.md` — implemented and verified results for Docker-backed staged execution wiring.
- `coordinator-transition-wiring-results.md` — implemented and verified results for the first coordinator-owned approval transition engine.
- `coordinator-postgres-state-results.md` — implemented and verified results for the first durable Postgres-backed coordinator state path.

## Next outputs to add

- first repository skeleton commit plan
- broader benchmark matrix result
## Recommended reading order

1. `DR-001-mvp-interception-strategy.md`
2. `DR-002-mvp-control-plane-stack.md`
3. `mvp-adapter-reality-matrix.md`
4. `recovery-and-idempotency-note.md`
5. `local-staging-runtime-plan.md`
6. `benchmark-feasibility-split.md`
7. `implementation-risk-register.md`
8. `priority-1-spike-results.md`
9. `priority-2-spike-results.md`
10. `failed-verification-path-results.md`
11. `first-cross-tool-vertical-slice-results.md`
12. `first-benchmark-smoke-results.md`
13. `destructive-shell-containment-smoke-results.md`
14. `benchmark-evidence-export-results.md`
15. `artifact-store-spike-results.md`
16. `local-dev-stack-spike-results.md`
17. `migration-failure-smoke-results.md`
18. `cross-tool-evidence-export-results.md`
19. `duplicate-retry-smoke-results.md`
20. `stale-github-drift-smoke-results.md`
21. `multi-effect-commit-orchestration-results.md`
22. `partial-commit-reconciliation-results.md`
23. `approval-invalidation-results.md`
24. `containerized-runtime-hardening-results.md`
25. `compensation-policy-results.md`
26. `coordinator-approval-state-results.md`
27. `container-runtime-wiring-results.md`
28. `coordinator-transition-wiring-results.md`
29. `coordinator-postgres-state-results.md`
## Current research verdict

The MVP is no longer blocked on vague architecture questions.

The main research and bootstrap decisions now fixed are:

1. **Interception**: start with a wrapper/proxy path, not multi-path integrations.
2. **Control plane**: start with Go, Postgres, DB-driven workers, and S3-compatible artifact storage.
3. **Adapter honesty**: Git/filesystem is strongest; GitHub/Postgres are freshness-check driven; Slack is weaker and must stay visibly weaker.
4. **Recovery discipline**: no blind retries after ambiguity; `UNKNOWN` and manual intervention are real first-class paths.
5. **Local runtime**: git worktrees + disposable Postgres + filesystem artifacts are proven now, and the Docker-backed runtime path is both hardened and wired as an optional staged-command executor.
6. **Verification gate**: failed staged verification aborts before promotion while preserving inspectable evidence.
7. **Cross-tool preparation**: one flow now spans repo staging, Postgres preview, GitHub preparation, and Slack preparation without outward effects on failure.
8. **Benchmark smoke**: direct-vs-FutureDiff smoke now covers file-change failure, destructive shell containment, migration failure, duplicate retry, and stale GitHub drift.
9. **Evidence durability**: benchmark artifacts and cross-tool transaction evidence now export through a reusable local artifact store.
10. **Commit path**: the bootstrap repo now has a real multi-effect commit seam, partial-commit reconciliation, and a first concrete compensation path.
11. **Approval semantics**: exact prepared-state approval can now be invalidated before commit when material fields drift, approval state has a coordinator-owned store, and approval transitions now have a coordinator-owned engine boundary.
12. **Primary durability**: coordinator transaction state, effect state, and ledger transitions now have a first durable Postgres-backed path.

That is enough to move from bootstrap spikes into broader benchmark coverage, deeper coordinator transition ownership, and fuller production-shape control-plane hardening without hand-wavy drift.
