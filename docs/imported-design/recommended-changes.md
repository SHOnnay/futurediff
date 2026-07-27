# Recommended Changes to the Original Brief

These are the changes I would make before anyone starts building.

## 1. Make persistence explicit in the top-level architecture

The brief mentions evidence and recovery, but the main architecture should explicitly show:

- append-only transaction ledger;
- metadata store;
- artifact/blob store; and
- reconciliation worker.

Without those, crash recovery stays hand-wavy.

## 2. Add resource locking as a first-class subsystem

The current brief does not give concurrency control enough weight. Two autonomous agents will collide on shared resources.

Add:
- canonical resource URIs;
- transaction leases;
- lock arbitration rules.

## 3. Split control plane from staging plane

The current repository structure mixes several concerns. Separate:

- control plane: coordination, policy, approval, recovery;
- staging plane: worktrees, containers, disposable databases;
- experience plane: CLI and UI.

This keeps the core safe even if the UI changes completely.

## 4. Treat approval as a snapshot artifact

Approval must not mean “approve whatever the system recomputes later.”

Add an explicit approval snapshot containing:
- effect hashes;
- policy version hash;
- verification evidence hashes;
- resource versions;
- commit order.

## 5. Strengthen the trust-boundary language

The brief states the gateway should hold credentials, but that needs to be elevated from guidance to requirement.

Change the wording to: if the agent can bypass the gateway, FutureDiff does not provide its intended safety guarantees.

## 6. Split `shell-docker` into safer pieces

A single `shell-docker` adapter is too broad.

Replace it with:
- runtime/container sandbox;
- constrained command execution adapter;
- egress policy enforcement.

Generic shell is where safety claims go to die.

## 7. Isolate irreversible effects more aggressively

The current brief handles Class I honestly, but the design should go further:

- do not allow Class I effects inside default multi-effect auto-commit flows;
- require explicit transaction splitting or a separate approval phase.

## 8. Add explicit support levels for adapters

Not every provider can support the same guarantee.

Each adapter should declare one of:
- exact prepare/commit;
- preview + freshness check;
- idempotent best effort;
- unsupported.

This avoids pretending all integrations are equally strong.

## 9. Add policy around `UNKNOWN` state

The brief talks about recovery, but not strongly enough about ambiguity after timeouts.

Add a formal rule:
- timeout or crash with unknown provider outcome becomes `UNKNOWN`;
- `UNKNOWN` blocks further automatic commit until status or policy resolution.

## 10. Make benchmarking a release gate

The benchmark section is good. It should be mandatory, not aspirational.

Do not ship a public claim set until the repository can demonstrate:
- duplicate prevention;
- crash recovery;
- no second LLM run for commit;
- at least one failed transaction with zero external side effects.

## 11. Move the cinematic UI behind the engine

The brief already warns against leading with 3D. Good. I would harden that into a repo priority rule:

- dashboard after gateway;
- spatial/cinematic view after benchmarks and conformance tests.

## 12. Add a conformance suite to the repository plan

Adapters will drift unless there is a strict shared test kit.

Add:
- adapter lifecycle contract tests;
- idempotency tests;
- recovery tests;
- compensation tests;
- unsupported-effect tests.

## 13. Tighten export safety for `.futurepack`

The brief mentions encryption or redaction by default. Keep that, but add artifact-level classification and export profiles:

- redacted shareable;
- internal audit;
- privileged forensic.

## 14. Refine the MVP statement

The MVP should be narrower and sharper:

Required:
- Git/filesystem;
- containerized runtime;
- Postgres;
- GitHub;
- Slack;
- recovery;
- conformance tests;
- benchmark demo.

Not required:
- broad provider coverage;
- hosted multi-tenant SaaS;
- cinematic UI.

## 15. Repository structure adjustment

I would replace the original tree with a structure that makes responsibility boundaries obvious:

```text
specs/
control-plane/
staging/
adapters/
verifier/
sdk/
integrations/
ui/
benchmarks/
examples/
```

That is a cleaner map than centering everything under `gateway/`.

## Final recommendation

Do not change the product thesis. Change the execution discipline.

FutureDiff is strongest when it is built as:
- a transaction gateway first;
- a recovery system second;
- a benchmarked adapter platform third;
- a UI experience last.
